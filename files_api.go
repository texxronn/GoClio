package main

import (
	"database/sql"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fileUploadLimit is the maximum individual upload (section 64.12).
const fileUploadLimit int64 = 16 * 1024 * 1024

// reservedFilesSegment is reserved directly under /{project}/files so a content
// path cannot shadow the stable ID URL /{project}/files/id/{id} (section 66.5).
// The API shape /api/v1/{project}/files/{id} addresses an opaque entry ID.
const reservedFilesSegment = "id"

// filesCollection serves GET /api/v1/{project}/files: the paged flat catalog,
// or the node at ?path= (section 64.4).
func (a *app) filesCollection(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !q.Has("path") {
		a.listFiles(w, r)
		return
	}
	clean, ae := canonicalContentPath(q.Get("path"))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	node, ae := a.contentNode(clean, limit, offset)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	writeJSON(w, 200, node)
}

// listFiles is the paged flat catalog with optional prefix, content_type and
// kind filters (section 64.4).
func (a *app) listFiles(w http.ResponseWriter, r *http.Request) {
	q := queryValues(r)
	limit, ae := intParam(q, "limit", 100, 1, 1000)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	offset, ae := intParam(q, "offset", 0, 0, int(^uint(0)>>1))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	where := "project=?"
	args := []any{a.project}
	if kind := first(q, "kind"); kind != "" {
		if kind != "file" && kind != "page" {
			writeAPIError(w, invalid("kind must be file or page"))
			return
		}
		where += " AND kind=?"
		args = append(args, kind)
	}
	if contentType := first(q, "content_type"); contentType != "" {
		where += " AND content_type=?"
		args = append(args, contentType)
	}
	if prefix := first(q, "prefix"); prefix != "" {
		if _, ae := canonicalContentPath(prefix); ae != nil {
			writeErr(w, ae)
			return
		}
		where += " AND substr(path,1,length(?))=?"
		args = append(args, prefix, prefix)
	}
	var total int
	if err := a.db.QueryRow(`SELECT count(*) FROM content_entries WHERE `+where, args...).Scan(&total); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	rows, err := a.db.Query(
		`SELECT id,project,path,kind,content_type,size,sha256,created_at,updated_at FROM content_entries WHERE `+where+` ORDER BY path LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), limit, offset)...,
	)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	defer rows.Close()
	data := []map[string]any{}
	for rows.Next() {
		entry, scanErr := scanContentEntry(rows)
		if scanErr != nil {
			writeErr(w, errAPI(scanErr))
			return
		}
		data = append(data, a.contentEntryRepresentation(entry))
	}
	if err := rows.Err(); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeJSON(w, 200, map[string]any{"data": data, "page": pageInfo(limit, offset, len(data), total)})
}

func scanContentEntry(rows *sql.Rows) (contentEntry, error) {
	var entry contentEntry
	err := rows.Scan(&entry.ID, &entry.Project, &entry.Path, &entry.Kind, &entry.ContentType, &entry.Size, &entry.SHA256, &entry.CreatedAt, &entry.UpdatedAt)
	return entry, err
}

// contentNode returns the directory or content-entry representation at a
// canonical path. The project root is created lazily so it lists as empty.
func (a *app) contentNode(clean string, limit, offset int) (map[string]any, *apiError) {
	target, ae := a.contentPath(clean)
	if ae != nil {
		return nil, ae
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) && clean == "/" {
		if mkErr := os.MkdirAll(target, 0755); mkErr != nil {
			return nil, errAPI(mkErr)
		}
		info, err = os.Lstat(target)
	}
	if os.IsNotExist(err) {
		return nil, missing("File")
	}
	if err != nil {
		return nil, errAPI(err)
	}
	if info.IsDir() {
		return a.directoryRepresentation(clean, target, limit, offset)
	}
	if !info.Mode().IsRegular() {
		return nil, missing("File")
	}
	entry, ae := a.entryForExistingFile(clean, info)
	if ae != nil {
		return nil, ae
	}
	return a.contentEntryRepresentation(entry), nil
}

// entryForExistingFile returns the entry for a regular file, adopting a file
// written directly on disk so it still has a stable ID (section 64.2).
func (a *app) entryForExistingFile(clean string, info os.FileInfo) (contentEntry, *apiError) {
	entry, found, err := a.contentEntryByPath(clean)
	if err != nil {
		return contentEntry{}, errAPI(err)
	}
	if found {
		return entry, nil
	}
	if err = a.adoptContentPath(clean, info); err != nil {
		return contentEntry{}, errAPI(err)
	}
	entry, found, err = a.contentEntryByPath(clean)
	if err != nil {
		return contentEntry{}, errAPI(err)
	}
	if !found {
		return contentEntry{}, errAPI(fmt.Errorf("content entry missing after adoption: %s", clean))
	}
	return entry, nil
}

// adoptContentPath records a file discovered on disk without an entry. It keeps
// the sha256 empty, matching reconciliation (section 64.2).
func (a *app) adoptContentPath(clean string, info os.FileInfo) error {
	now := formatUTC(time.Now())
	_, err := a.db.Exec(
		`INSERT OR IGNORE INTO content_entries(id,project,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		newID(), a.project, clean, contentKind(clean), contentMediaType(clean), info.Size(), "", now, now,
	)
	return err
}

func (a *app) directoryRepresentation(clean, target string, limit, offset int) (map[string]any, *apiError) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, errAPI(err)
	}
	children := make([]map[string]any, 0, len(entries))
	for _, de := range entries {
		if de.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := joinContentPath(clean, de.Name())
		info, err := de.Info()
		if err != nil {
			return nil, errAPI(err)
		}
		if info.IsDir() {
			children = append(children, map[string]any{"path": child, "kind": "directory", "url": a.contentURL(child)})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		entry, ae := a.entryForExistingFile(child, info)
		if ae != nil {
			return nil, ae
		}
		children = append(children, a.contentEntryRepresentation(entry))
	}
	sort.Slice(children, func(i, j int) bool {
		return children[i]["path"].(string) < children[j]["path"].(string)
	})
	total := len(children)
	page := pageSlice(children, limit, offset)
	return map[string]any{
		"path":     clean,
		"kind":     "directory",
		"url":      a.contentURL(clean),
		"children": page,
		"page":     pageInfo(limit, offset, len(page), total),
	}, nil
}

// contentEntryRepresentation is the wire form of a page or file (section 64.4).
func (a *app) contentEntryRepresentation(entry contentEntry) map[string]any {
	return map[string]any{
		"id":           entry.ID,
		"path":         entry.Path,
		"kind":         entry.Kind,
		"content_type": entry.ContentType,
		"size":         entry.Size,
		"sha256":       entry.SHA256,
		"created_at":   entry.CreatedAt,
		"updated_at":   entry.UpdatedAt,
		"url":          a.contentURL(entry.Path),
		"stable_url":   a.baseURL + a.projectPath("files", "id", entry.ID),
		"indexed":      false,
	}
}

// getFile serves GET /api/v1/{project}/files/{id} (section 64.4).
func (a *app) getFile(w http.ResponseWriter, r *http.Request, id string) {
	entry, found, err := a.contentEntryByID(id)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if !found {
		writeAPIError(w, missing("File"))
		return
	}
	writeJSON(w, 200, a.contentEntryRepresentation(entry))
}

// serveFileContent serves GET/HEAD /api/v1/{project}/files/{id}/content and,
// through the human stable URL, /{project}/files/id/{id}: the raw stored bytes
// of an entry with a download disposition (sections 64.4, 64.5 and 66.9). Pages
// download here too; they render only at their path URL.
func (a *app) serveFileContent(w http.ResponseWriter, r *http.Request, id string) {
	entry, found, err := a.contentEntryByID(id)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if !found {
		writeAPIError(w, missing("File"))
		return
	}
	target, ae := a.contentPath(entry.Path)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if ae = serveDownload(w, r, target, entry.ContentType); ae != nil {
		writeErr(w, ae)
	}
}

// serveDownload streams a regular file as an attachment (section 64.5). The
// download disposition and nosniff are set before http.ServeContent, which
// supplies HEAD support, byte ranges and conditional requests.
func serveDownload(w http.ResponseWriter, r *http.Request, target, contentType string) *apiError {
	f, err := os.Open(target)
	if os.IsNotExist(err) {
		return missing("File")
	}
	if err != nil {
		return errAPI(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return errAPI(err)
	}
	if !info.Mode().IsRegular() {
		return missing("File")
	}
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return nil
}

// putFile serves PUT /api/v1/{project}/files?path=...: create or replace raw
// bytes atomically, preserving the entry ID on replace (section 64.4).
func (a *app) putFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !q.Has("path") {
		writeAPIError(w, invalid("path is required"))
		return
	}
	clean, ae := canonicalContentPath(q.Get("path"))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	body, ae := readRawBody(r, fileUploadLimit)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	existed := false
	createdAt := ""
	if info, e := os.Lstat(target); e == nil {
		if info.IsDir() || !info.Mode().IsRegular() {
			writeAPIError(w, conflict("An incompatible resource already exists at this path"))
			return
		}
		existed = true
		entry, found, lookupErr := a.contentEntryByPath(clean)
		if lookupErr != nil {
			writeErr(w, errAPI(lookupErr))
			return
		}
		if found {
			createdAt = entry.CreatedAt
		} else {
			createdAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
	} else if !os.IsNotExist(e) {
		writeErr(w, errAPI(e))
		return
	}
	if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
		writeErr(w, ae)
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if err := atomicWriteFile(target, body); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	contentType := ""
	if contentKind(clean) == "file" {
		contentType = requestMediaType(r)
	}
	if err := a.saveContentEntryTyped(clean, body, createdAt, contentType); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	entry, found, err := a.contentEntryByPath(clean)
	if err != nil || !found {
		writeErr(w, errAPI(firstError(err, fmt.Errorf("content entry missing after write: %s", clean))))
		return
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, a.contentEntryRepresentation(entry))
}

func readRawBody(r *http.Request, limit int64) ([]byte, *apiError) {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, invalid("Malformed request body")
	}
	if int64(len(body)) > limit {
		return nil, &apiError{http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"}
	}
	return body, nil
}

func atomicWriteFile(target string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".clio-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, target)
}

// requestMediaType returns the declared request media type, or "" when the
// header is absent or malformed.
func requestMediaType(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Content-Type"))
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return mediaType
}

// deleteFileByPath serves DELETE /api/v1/{project}/files?path=... (section 64.4).
func (a *app) deleteFileByPath(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !q.Has("path") {
		writeAPIError(w, invalid("path is required"))
		return
	}
	clean, ae := canonicalContentPath(q.Get("path"))
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if clean == "/" {
		writeAPIError(w, conflict("The content root cannot be deleted"))
		return
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		writeAPIError(w, missing("File"))
		return
	}
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if info.IsDir() {
		if err = os.RemoveAll(target); err != nil {
			writeErr(w, errAPI(err))
			return
		}
		if err = a.deleteContentEntriesUnder(clean); err != nil {
			writeErr(w, errAPI(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !info.Mode().IsRegular() {
		writeAPIError(w, missing("File"))
		return
	}
	if err = os.Remove(target); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if err = a.deleteContentEntry(clean); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteFileByID serves DELETE /api/v1/{project}/files/{id} (section 64.4).
func (a *app) deleteFileByID(w http.ResponseWriter, r *http.Request, id string) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	entry, found, err := a.contentEntryByID(id)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if !found {
		writeAPIError(w, missing("File"))
		return
	}
	target, ae := a.contentPath(entry.Path)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.IsDir() {
			writeAPIError(w, conflict("The path names a directory"))
			return
		}
		if err = os.Remove(target); err != nil {
			writeErr(w, errAPI(err))
			return
		}
	} else if !os.IsNotExist(statErr) {
		writeErr(w, errAPI(statErr))
		return
	}
	if err = a.deleteContentEntry(entry.Path); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// transferFile serves POST .../files/move and .../files/copy (section 64.4).
func (a *app) transferFile(w http.ResponseWriter, r *http.Request, copying bool) {
	input, ae := readJSON(r, bodyLimit)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	fromRaw, ok := input["from"].(string)
	if !ok || fromRaw == "" {
		writeAPIError(w, invalid("from is required"))
		return
	}
	toRaw, ok := input["to"].(string)
	if !ok || toRaw == "" {
		writeAPIError(w, invalid("to is required"))
		return
	}
	from, ae := canonicalContentPath(fromRaw)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	to, ae := canonicalContentPath(toRaw)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if from == "/" || to == "/" {
		writeAPIError(w, invalid("The content root cannot be moved or copied"))
		return
	}
	if from == to {
		writeAPIError(w, conflict("Source and destination are the same"))
		return
	}
	if strings.HasPrefix(to+"/", from+"/") {
		writeAPIError(w, conflict("A directory cannot be moved or copied into itself"))
		return
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	src, ae := a.contentPath(from)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	dst, ae := a.contentPath(to)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		writeAPIError(w, missing("File"))
		return
	}
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		writeAPIError(w, missing("File"))
		return
	}
	if _, err = os.Lstat(dst); err == nil {
		writeAPIError(w, conflict("A resource already exists at the destination"))
		return
	} else if !os.IsNotExist(err) {
		writeErr(w, errAPI(err))
		return
	}
	if ae = a.checkNoFileParent(filepath.Dir(dst)); ae != nil {
		writeErr(w, ae)
		return
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		writeErr(w, errAPI(err))
		return
	}
	switch {
	case info.IsDir() && copying:
		ae = a.copyDirectory(from, to, src, dst)
	case info.IsDir():
		ae = a.moveDirectory(from, to, src, dst)
	case info.Mode().IsRegular() && copying:
		ae = a.copyRegularFile(from, to, src, dst)
	case info.Mode().IsRegular():
		ae = a.moveRegularFile(from, to, src, dst)
	default:
		writeAPIError(w, missing("File"))
		return
	}
	if ae != nil {
		writeErr(w, ae)
		return
	}
	node, ae := a.contentNode(to, 100, 0)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	status := http.StatusOK
	if copying {
		status = http.StatusCreated
	}
	writeJSON(w, status, node)
}

// moveRegularFile renames bytes on disk and updates the entry path, preserving
// the ID (section 64.2).
func (a *app) moveRegularFile(from, to, src, dst string) *apiError {
	entry, found, err := a.contentEntryByPath(from)
	if err != nil {
		return errAPI(err)
	}
	if err = os.Rename(src, dst); err != nil {
		return errAPI(err)
	}
	if !found {
		info, statErr := os.Lstat(dst)
		if statErr != nil {
			return errAPI(statErr)
		}
		if err = a.adoptContentPath(to, info); err != nil {
			return errAPI(err)
		}
		return nil
	}
	contentType := entry.ContentType
	if filepath.Ext(from) != filepath.Ext(to) {
		contentType = contentMediaType(to)
	}
	if err = a.applyContentRename(entry.ID, from, to, contentKind(to), contentType, false); err != nil {
		_ = os.Rename(dst, src)
		return errAPI(err)
	}
	return nil
}

// moveDirectory renames a subtree and re-homes every descendant entry path,
// preserving each ID (section 64.2).
func (a *app) moveDirectory(from, to, src, dst string) *apiError {
	if err := os.Rename(src, dst); err != nil {
		return errAPI(err)
	}
	if err := a.applyContentRename("", from, to, "", "", true); err != nil {
		_ = os.Rename(dst, src)
		return errAPI(err)
	}
	return nil
}

// copyRegularFile writes a new file at the destination and assigns a new entry
// ID (section 64.4).
func (a *app) copyRegularFile(from, to, src, dst string) *apiError {
	data, err := os.ReadFile(src)
	if err != nil {
		return errAPI(err)
	}
	contentType := ""
	if entry, found, lookupErr := a.contentEntryByPath(from); lookupErr == nil && found {
		contentType = entry.ContentType
	}
	if err = atomicWriteFile(dst, data); err != nil {
		return errAPI(err)
	}
	if err = a.saveContentEntryTyped(to, data, "", contentType); err != nil {
		_ = os.Remove(dst)
		return errAPI(err)
	}
	return nil
}

// copyDirectory recursively copies a subtree, assigning new IDs to every file
// (section 64.4). Symbolic links are not followed.
func (a *app) copyDirectory(from, to, src, dst string) *apiError {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return errAPI(err)
	}
	createdEntries := []string{}
	rollback := func() {
		for _, p := range createdEntries {
			_ = a.deleteContentEntry(p)
		}
		_ = os.RemoveAll(dst)
	}
	walkErr := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		destPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err = os.WriteFile(destPath, data, 0644); err != nil {
			return err
		}
		contentType := ""
		if entry, found, lookupErr := a.contentEntryByPath(joinContentPath(from, relSlash)); lookupErr == nil && found {
			contentType = entry.ContentType
		}
		cleanDest := joinContentPath(to, relSlash)
		if err = a.saveContentEntryTyped(cleanDest, data, "", contentType); err != nil {
			return err
		}
		createdEntries = append(createdEntries, cleanDest)
		return nil
	})
	if walkErr != nil {
		rollback()
		return errAPI(walkErr)
	}
	return nil
}

// rescanFiles serves POST /api/v1/{project}/files/rescan (section 64.4).
func (a *app) rescanFiles(w http.ResponseWriter, r *http.Request) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	summary, err := a.rescanContent()
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeJSON(w, 200, map[string]any{"added": summary.Added, "removed": summary.Removed, "refreshed": summary.Refreshed})
}

func joinContentPath(base, name string) string {
	if base == "/" {
		return "/" + name
	}
	return base + "/" + name
}
