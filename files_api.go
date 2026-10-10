package main

import (
	"database/sql"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
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

// createDirectoryAPI serves POST /api/v1/{project}/files/directories: a JSON
// directory create or, when the body is a ZIP archive, a directory-tree upload
// (sections 38 and 44 folded into the files partition, sections 66.2 and 66.7).
func (a *app) createDirectoryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/zip") {
		a.uploadZip(w, r, r.URL.Query().Get("path"))
		return
	}
	input, ae := readJSON(r, bodyLimit)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	raw, ok := input["path"].(string)
	if !ok || raw == "" {
		writeAPIError(w, invalid("path is required"))
		return
	}
	clean, ae := canonicalContentPath(raw)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if clean == "/" {
		writeAPIError(w, conflict("Root directory already exists"))
		return
	}
	created, ae := a.createDirectory(clean)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	v, ae := a.contentNode(created, 100, 0)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	writeJSON(w, http.StatusCreated, v)
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
	indexed, err := a.indexedEntryIDs()
	if err != nil {
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
		data = append(data, a.contentEntryRepresentationIndexed(entry, indexed[entry.ID]))
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
	// ReadDir already sorts by name. Keep only representable children, then
	// page over names before stat-ing or representing anything, so a large
	// directory does not build every child to return one page (section 23).
	type dirChild struct {
		entry os.DirEntry
		isDir bool
	}
	visible := make([]dirChild, 0, len(entries))
	for _, de := range entries {
		if ignoredContentName(de.Name()) {
			continue
		}
		if de.Type()&os.ModeSymlink != 0 {
			continue
		}
		isDir, isRegular := de.IsDir(), de.Type().IsRegular()
		if !isDir && !isRegular {
			// The entry type may be unknown on some filesystems (or name a
			// special file); stat to decide whether it is representable.
			info, err := de.Info()
			if err != nil {
				return nil, errAPI(err)
			}
			isDir, isRegular = info.IsDir(), info.Mode().IsRegular()
		}
		if !isDir && !isRegular {
			continue
		}
		visible = append(visible, dirChild{entry: de, isDir: isDir})
	}
	total := len(visible)
	window := pageSlice(visible, limit, offset)
	children := make([]map[string]any, 0, len(window))
	indexed, err := a.indexedEntryIDs()
	if err != nil {
		return nil, errAPI(err)
	}
	for _, child := range window {
		childPath := joinContentPath(clean, child.entry.Name())
		if child.isDir {
			children = append(children, map[string]any{"path": childPath, "kind": "directory", "url": a.contentURL(childPath)})
			continue
		}
		info, err := child.entry.Info()
		if err != nil {
			return nil, errAPI(err)
		}
		entry, ae := a.entryForExistingFile(childPath, info)
		if ae != nil {
			return nil, ae
		}
		children = append(children, a.contentEntryRepresentationIndexed(entry, indexed[entry.ID]))
	}
	return map[string]any{
		"path":     clean,
		"kind":     "directory",
		"url":      a.contentURL(clean),
		"children": children,
		"page":     pageInfo(limit, offset, len(children), total),
	}, nil
}

// contentEntryRepresentation is the wire form of a page or file (section 64.4).
func (a *app) contentEntryRepresentation(entry contentEntry) map[string]any {
	return a.contentEntryRepresentationIndexed(entry, a.entryIndexed(entry.ID))
}

// contentEntryRepresentationIndexed is contentEntryRepresentation with a known
// index state, so a listing can resolve `indexed` for every entry in one query.
func (a *app) contentEntryRepresentationIndexed(entry contentEntry, indexed bool) map[string]any {
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
		"indexed":      indexed,
	}
}

// entryIndexed reports whether an entry has a searchable text-index row
// (section 64.6): native extracted text or agent-supplied text.
func (a *app) entryIndexed(id string) bool {
	var n int
	if err := a.db.QueryRow(`SELECT count(*) FROM content_search WHERE project=? AND id=?`, a.project, id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// indexedEntryIDs returns the IDs of entries that have a text-index row, so a
// listing can set `indexed` without a query per entry (section 64.6).
func (a *app) indexedEntryIDs() (map[string]bool, error) {
	rows, err := a.db.Query(`SELECT id FROM content_search WHERE project=?`, a.project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexed := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		indexed[id] = true
	}
	return indexed, rows.Err()
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
	if ae = serveDownload(w, r, target, entry.ContentType, wantsInline(r)); ae != nil {
		writeErr(w, ae)
	}
}

// wantsInline reports whether the request asked for an inline preview
// (section 64.5), e.g. GET /{project}/files/id/{id}?inline=1.
func wantsInline(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("inline"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// inlinePreviewSafe reports whether a content type may be rendered inline by the
// browser without executing active content in Clio's origin (section 64.5).
// HTML, SVG, XHTML and scripts are never safe; images/PDF/plain text/CSV/
// Markdown/audio/video are.
func inlinePreviewSafe(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "application/pdf", "text/plain", "text/csv", "text/markdown", "text/tab-separated-values":
		return true
	}
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return mediaType != "image/svg+xml"
	case strings.HasPrefix(mediaType, "audio/"), strings.HasPrefix(mediaType, "video/"):
		return true
	}
	return false
}

// serveDownload streams a regular file (section 64.5). The disposition and
// nosniff are set before http.ServeContent, which supplies HEAD support and byte
// ranges. The download disposition always carries the entry's file name so the
// browser saves it as the real name; an explicit inline request (inline true)
// uses "inline" for content types that are safe to display and stays
// "attachment" for active content and everything else. A zero modification time
// is passed so no Last-Modified header (and therefore no If-Modified-Since/304
// conditional requests) is exposed, matching section 3.1; ranges and HEAD still
// work.
func serveDownload(w http.ResponseWriter, r *http.Request, target, contentType string, inline bool) *apiError {
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
	disposition := "attachment"
	if inline && inlinePreviewSafe(contentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": info.Name()}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, info.Name(), time.Time{}, f)
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
	contentType := ""
	if contentKind(clean) == "file" {
		contentType = requestMediaType(r)
	}
	entry, existed, ae := a.storeContentFile(clean, body, contentType)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, a.contentEntryRepresentation(entry))
}

// storeContentFile writes bytes atomically at a canonical content path and
// records the entry, preserving the ID and created_at on replace. The files
// API and the WebDAV mount share it so both apply the same 16 MiB limit,
// timestamps and index updates (sections 64.2, 64.4 and 64.10).
func (a *app) storeContentFile(clean string, body []byte, contentType string) (contentEntry, bool, *apiError) {
	if ae := rejectIgnoredContentPath(clean); ae != nil {
		return contentEntry{}, false, ae
	}
	if int64(len(body)) > fileUploadLimit {
		return contentEntry{}, false, &apiError{http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"}
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		return contentEntry{}, false, ae
	}
	existed := false
	createdAt := ""
	if info, e := os.Lstat(target); e == nil {
		if info.IsDir() || !info.Mode().IsRegular() {
			return contentEntry{}, false, conflict("An incompatible resource already exists at this path")
		}
		existed = true
		entry, found, lookupErr := a.contentEntryByPath(clean)
		if lookupErr != nil {
			return contentEntry{}, false, errAPI(lookupErr)
		}
		if found {
			createdAt = entry.CreatedAt
		} else {
			createdAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
	} else if !os.IsNotExist(e) {
		return contentEntry{}, false, errAPI(e)
	}
	if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
		return contentEntry{}, false, ae
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return contentEntry{}, false, errAPI(err)
	}
	if err := atomicWriteFile(target, body); err != nil {
		return contentEntry{}, false, errAPI(err)
	}
	if err := a.saveContentEntryTyped(clean, body, createdAt, contentType); err != nil {
		return contentEntry{}, false, errAPI(err)
	}
	entry, found, err := a.contentEntryByPath(clean)
	if err != nil || !found {
		return contentEntry{}, false, errAPI(firstError(err, fmt.Errorf("content entry missing after write: %s", clean)))
	}
	return entry, existed, nil
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
	if ae := a.removeContentPath(clean); ae != nil {
		writeErr(w, ae)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeContentPath deletes a file or a directory subtree and its entries,
// enforcing the attachment-reference 409 (section 64.9). The files API and the
// WebDAV mount share it (sections 64.4 and 64.10).
func (a *app) removeContentPath(clean string) *apiError {
	if clean == "/" {
		return conflict("The content root cannot be deleted")
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		return ae
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return missing("File")
	}
	if err != nil {
		return errAPI(err)
	}
	if info.IsDir() {
		ids, lookupErr := a.contentEntryIDsUnder(clean)
		if lookupErr != nil {
			return errAPI(lookupErr)
		}
		if e := a.attachmentDeleteConflict(ids); e != nil {
			return e
		}
		if err = os.RemoveAll(target); err != nil {
			return errAPI(err)
		}
		if err = a.deleteContentEntriesUnder(clean); err != nil {
			return errAPI(err)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return missing("File")
	}
	entry, found, lookupErr := a.contentEntryByPath(clean)
	if lookupErr != nil {
		return errAPI(lookupErr)
	}
	if found {
		if e := a.attachmentDeleteConflict([]string{entry.ID}); e != nil {
			return e
		}
	}
	if err = os.Remove(target); err != nil {
		return errAPI(err)
	}
	if err = a.deleteContentEntry(clean); err != nil {
		return errAPI(err)
	}
	return nil
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
	if conflictErr := a.attachmentDeleteConflict([]string{entry.ID}); conflictErr != nil {
		writeErr(w, conflictErr)
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
	if ae := a.transferContent(from, to, copying); ae != nil {
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

// transferContent moves or copies a file or directory between two canonical
// content paths. A move preserves the entry ID (and descendant IDs); a copy
// assigns new IDs (sections 64.2 and 64.4). The files API and the WebDAV mount
// share it (section 64.10).
func (a *app) transferContent(from, to string, copying bool) *apiError {
	if ae := rejectIgnoredContentPath(to); ae != nil {
		return ae
	}
	if from == "/" || to == "/" {
		return invalid("The content root cannot be moved or copied")
	}
	if from == to {
		return conflict("Source and destination are the same")
	}
	if strings.HasPrefix(to+"/", from+"/") {
		return conflict("A directory cannot be moved or copied into itself")
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	src, ae := a.contentPath(from)
	if ae != nil {
		return ae
	}
	dst, ae := a.contentPath(to)
	if ae != nil {
		return ae
	}
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return missing("File")
	}
	if err != nil {
		return errAPI(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return missing("File")
	}
	if _, err = os.Lstat(dst); err == nil {
		return conflict("A resource already exists at the destination")
	} else if !os.IsNotExist(err) {
		return errAPI(err)
	}
	if ae = a.checkNoFileParent(filepath.Dir(dst)); ae != nil {
		return ae
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return errAPI(err)
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
		return missing("File")
	}
	return ae
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

// rescanFiles serves POST /api/v1/{project}/files/rescan (section 64.4). An
// optional full=true re-hashes every entry and rebuilds its native index;
// full=false (the default) trusts stored hashes for unchanged entries. Any
// other full value is refused with 422.
func (a *app) rescanFiles(w http.ResponseWriter, r *http.Request) {
	full := false
	if q := r.URL.Query(); q.Has("full") {
		switch q.Get("full") {
		case "true":
			full = true
		case "false":
		default:
			writeAPIError(w, invalid("full must be true or false"))
			return
		}
	}
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	summary, err := a.rescanContentMode(full)
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
