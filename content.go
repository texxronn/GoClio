package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const bodyLimit int64 = 2 * 1024 * 1024
const zipLimit int64 = 32 * 1024 * 1024

func (a *app) directoriesAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := q.Get("path")
	if p == "" {
		p = "/"
	}
	switch r.Method {
	case "GET":
		v, e := a.directory(p)
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, v)
	case "DELETE":
		if !q.Has("path") {
			writeAPIError(w, invalid("path is required"))
			return
		}
		clean, ae := canonicalContentPath(p)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		if clean == "/" {
			writeAPIError(w, invalid("The content root cannot be deleted"))
			return
		}
		a.contentMu.Lock()
		defer a.contentMu.Unlock()
		target, ae := a.contentPath(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		info, e := os.Stat(target)
		if os.IsNotExist(e) || e == nil && !info.IsDir() {
			writeAPIError(w, missing("Directory"))
			return
		}
		if e != nil {
			writeErr(w, errAPI(e))
			return
		}
		tx, txErr := a.db.Begin()
		if txErr != nil {
			writeErr(w, errAPI(txErr))
			return
		}
		prefix := clean + "/"
		if _, txErr = tx.Exec(`DELETE FROM content_page_times WHERE path=? OR substr(path,1,length(?))=?`, clean, prefix, prefix); txErr != nil {
			tx.Rollback()
			writeErr(w, errAPI(txErr))
			return
		}
		if e = os.RemoveAll(target); e != nil {
			tx.Rollback()
			writeErr(w, errAPI(e))
			return
		}
		if txErr = tx.Commit(); txErr != nil {
			writeErr(w, errAPI(txErr))
			return
		}
		w.WriteHeader(204)
	case "POST":
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/zip") {
			a.uploadZip(w, r, p)
			return
		}
		input, e := readJSON(r, bodyLimit)
		if e != nil {
			writeErr(w, e)
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
		v, ae := a.directory(created)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		writeJSON(w, 201, v)
	default:
		writeAPIError(w, methodNotAllowed())
	}
}

func (a *app) createDirectory(raw string) (string, *apiError) {
	clean, ae := canonicalContentPath(raw)
	if ae != nil {
		return "", ae
	}
	if clean == "/" {
		return "", conflict("Root directory already exists")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	target, ae := a.contentPath(clean)
	if ae != nil {
		return "", ae
	}
	if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
		return "", ae
	}
	_, statErr := os.Lstat(target)
	if statErr == nil {
		return "", conflict("A resource already exists at this path")
	}
	if !os.IsNotExist(statErr) {
		return "", errAPI(statErr)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		if os.IsExist(err) {
			return "", conflict("A resource already exists at this path")
		}
		return "", errAPI(err)
	}
	return clean, nil
}

func (a *app) directory(raw string) (map[string]any, *apiError) {
	canonical, ae := canonicalContentPath(raw)
	if ae != nil {
		return nil, ae
	}
	target, ae := a.contentPath(canonical)
	if ae != nil {
		return nil, ae
	}
	entries, e := os.ReadDir(target)
	if os.IsNotExist(e) {
		return nil, missing("Directory")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	children := []map[string]any{}
	for _, entry := range entries {
		name := entry.Name()
		child := canonical
		if child == "/" {
			child = "/" + name
		} else {
			child += "/" + name
		}
		typ := "file"
		itemInfo, e := entry.Info()
		if e != nil {
			return nil, errAPI(e)
		}
		if itemInfo.IsDir() {
			typ = "directory"
		} else if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".html") {
			typ = "page"
		}
		item := map[string]any{"name": name, "type": typ, "url": absContentURL(a.baseURL, child)}
		if typ == "page" {
			if strings.HasSuffix(name, ".md") {
				item["content_type"] = "text/markdown"
			} else {
				item["content_type"] = "text/html"
			}
		}
		children = append(children, item)
	}
	sort.Slice(children, func(i, j int) bool { return children[i]["name"].(string) < children[j]["name"].(string) })
	u := a.baseURL
	if canonical != "/" {
		u = absContentURL(a.baseURL, canonical)
	}
	return map[string]any{"path": canonical, "url": u, "children": children}, nil
}

func (a *app) pagesAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	raw := q.Get("path")
	switch r.Method {
	case "GET":
		if raw == "" {
			writeAPIError(w, invalid("path is required"))
			return
		}
		v, e := a.page(raw)
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, v)
	case "DELETE":
		if raw == "" {
			writeAPIError(w, invalid("path is required"))
			return
		}
		a.contentMu.Lock()
		defer a.contentMu.Unlock()
		clean, ae := canonicalContentPath(raw)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		target, ae := a.contentPath(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
			writeErr(w, ae)
			return
		}
		info, e := os.Stat(target)
		if os.IsNotExist(e) || e == nil && !info.Mode().IsRegular() {
			writeAPIError(w, missing("Page"))
			return
		}
		if e != nil {
			writeErr(w, errAPI(e))
			return
		}
		if e = os.Remove(target); e != nil {
			writeErr(w, errAPI(e))
			return
		}
		if _, e = a.db.Exec(`DELETE FROM content_page_times WHERE path=?`, clean); e != nil {
			writeErr(w, errAPI(e))
			return
		}
		w.WriteHeader(204)
	case "POST":
		a.contentMu.Lock()
		defer a.contentMu.Unlock()
		input, e := readJSON(r, bodyLimit)
		if e != nil {
			writeErr(w, e)
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
		if !strings.HasSuffix(clean, ".md") && !strings.HasSuffix(clean, ".html") {
			writeAPIError(w, invalid("Page paths must end with .md or .html"))
			return
		}
		contentType, ok := input["content_type"].(string)
		if !ok {
			writeAPIError(w, invalid("content_type is required"))
			return
		}
		want := "text/markdown"
		if strings.HasSuffix(clean, ".html") {
			want = "text/html"
		}
		if contentType != want {
			writeAPIError(w, invalid("content_type must be "+want+" for this path"))
			return
		}
		content, ok := input["content"].(string)
		if !ok {
			writeAPIError(w, invalid("content must be a string"))
			return
		}
		target, ae := a.contentPath(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
			writeErr(w, ae)
			return
		}
		if info, e := os.Lstat(target); e == nil {
			if info.IsDir() || !info.Mode().IsRegular() {
				writeAPIError(w, conflict("An incompatible resource already exists at this path"))
				return
			}
		}
		existed := false
		now := formatUTC(time.Now())
		createdAt := now
		if info, e := os.Lstat(target); e == nil {
			existed = true
			var stored string
			lookupErr := a.db.QueryRow(`SELECT created_at FROM content_page_times WHERE path=?`, clean).Scan(&stored)
			if lookupErr == nil {
				createdAt = stored
			} else if lookupErr == sql.ErrNoRows {
				createdAt = info.ModTime().UTC().Format(time.RFC3339Nano)
			} else {
				writeErr(w, errAPI(lookupErr))
				return
			}
		} else if !os.IsNotExist(e) {
			writeErr(w, errAPI(e))
			return
		}
		if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			writeErr(w, errAPI(e))
			return
		}
		tmp, err := os.CreateTemp(filepath.Dir(target), ".clio-*.tmp")
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		name := tmp.Name()
		defer os.Remove(name)
		if _, err = io.WriteString(tmp, content); err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		if err = os.Rename(name, target); err != nil {
			writeErr(w, errAPI(err))
			return
		}
		if _, err = a.db.Exec(`INSERT INTO content_page_times(path,created_at,updated_at) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET updated_at=excluded.updated_at`, clean, createdAt, now); err != nil {
			writeErr(w, errAPI(err))
			return
		}
		v, ae := a.page(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		status := 201
		if existed {
			status = 200
		}
		writeJSON(w, status, v)
	default:
		writeAPIError(w, methodNotAllowed())
	}
}

func (a *app) page(raw string) (map[string]any, *apiError) {
	clean, ae := canonicalContentPath(raw)
	if ae != nil {
		return nil, ae
	}
	if !strings.HasSuffix(clean, ".md") && !strings.HasSuffix(clean, ".html") {
		return nil, missing("Page")
	}
	target, ae := a.contentPath(clean)
	if ae != nil {
		return nil, ae
	}
	info, e := os.Stat(target)
	if os.IsNotExist(e) || e == nil && !info.Mode().IsRegular() {
		return nil, missing("Page")
	}
	if e != nil {
		return nil, errAPI(e)
	}
	data, e := os.ReadFile(target)
	if e != nil {
		return nil, errAPI(e)
	}
	contentType := "text/markdown"
	if strings.HasSuffix(clean, ".html") {
		contentType = "text/html"
	}
	stamp := info.ModTime().UTC().Format(time.RFC3339Nano)
	created, updated := stamp, stamp
	if e = a.db.QueryRow(`SELECT created_at,updated_at FROM content_page_times WHERE path=?`, clean).Scan(&created, &updated); e != nil && e != sql.ErrNoRows {
		return nil, errAPI(e)
	}
	return map[string]any{"path": clean, "url": absContentURL(a.baseURL, clean), "content_type": contentType, "content": string(data), "created_at": created, "updated_at": updated}, nil
}

func (a *app) uploadZip(w http.ResponseWriter, r *http.Request, destination string) {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	q := r.URL.Query()
	overwrite := false
	if q.Has("overwrite") {
		v := strings.ToLower(q.Get("overwrite"))
		if v != "true" && v != "false" {
			writeAPIError(w, invalid("overwrite must be true or false"))
			return
		}
		overwrite = v == "true"
	}
	dest, ae := canonicalContentPath(destination)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	destDir, ae := a.contentPath(dest)
	if ae != nil {
		writeErr(w, ae)
		return
	}
	if info, e := os.Stat(destDir); e == nil && !info.IsDir() {
		writeAPIError(w, conflict("Upload destination is not a directory"))
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, zipLimit+1))
	if e != nil {
		writeErr(w, errAPI(e))
		return
	}
	if int64(len(body)) > zipLimit {
		writeAPIError(w, invalid("Archive compressed size limit exceeded"))
		return
	}
	zr, e := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if e != nil {
		writeAPIError(w, invalid("Invalid ZIP archive"))
		return
	}
	if ae = a.checkNoFileParent(destDir); ae != nil {
		writeErr(w, ae)
		return
	}
	stage, e := os.MkdirTemp(filepath.Dir(a.content), ".clio-upload-")
	if e != nil {
		writeErr(w, errAPI(e))
		return
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	backupDir := filepath.Join(stage, "backups")
	if e = os.Mkdir(payload, 0755); e != nil {
		writeErr(w, errAPI(e))
		return
	}
	files := []string{}
	dirs := map[string]bool{}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	prefixKinds := map[string]bool{}
	expanded := int64(0)
	entryCount := 0
	for _, f := range zr.File {
		entryCount++
		if entryCount > 10000 {
			writeAPIError(w, invalid("Archive has more than 10000 entries"))
			return
		}
		name := f.Name
		if !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || strings.Contains(name, "\\") || regexpDrive(name) {
			writeAPIError(w, invalid("Archive contains an absolute or invalid path"))
			return
		}
		clean := strings.TrimSuffix(name, "/")
		if clean == "" {
			continue
		}
		parts := strings.Split(clean, "/")
		isDir := f.FileInfo().IsDir()
		depth := len(parts)
		if !isDir {
			depth--
		}
		if depth > 32 {
			writeAPIError(w, invalid("Archive directory depth exceeds 32"))
			return
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || hasControl(part) {
				writeAPIError(w, invalid("Archive contains an invalid path"))
				return
			}
		}
		if seen[clean] {
			writeAPIError(w, invalid("Archive contains a duplicate path: "+clean))
			return
		}
		seen[clean] = true
		for parent := path.Dir(clean); parent != "."; parent = path.Dir(parent) {
			if parentIsDir, ok := kinds[parent]; ok && !parentIsDir {
				writeAPIError(w, invalid("Archive file blocks another entry: "+parent))
				return
			}
		}
		if !isDir && prefixKinds[clean] {
			writeAPIError(w, invalid("Archive file blocks another entry: "+clean))
			return
		}
		kinds[clean] = isDir
		for parent := path.Dir(clean); parent != "."; parent = path.Dir(parent) {
			prefixKinds[parent] = true
		}
		relative := filepath.FromSlash(clean)
		full := filepath.Join(destDir, relative)
		if !within(a.content, full) {
			writeAPIError(w, invalid("Archive path escapes content directory"))
			return
		}
		joined := dest
		if joined == "/" {
			joined = ""
		}
		joined += "/" + filepath.ToSlash(relative)
		if _, e := canonicalContentPath(joined); e != nil {
			writeErr(w, e)
			return
		}
		stagePath := filepath.Join(payload, relative)
		if isDir {
			if e = os.MkdirAll(stagePath, 0755); e != nil {
				writeErr(w, errAPI(e))
				return
			}
			dirs[relative] = true
			continue
		}
		if f.UncompressedSize64 > 16*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if f.UncompressedSize64 > 256*1024*1024 || expanded+int64(f.UncompressedSize64) > 256*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if e = os.MkdirAll(filepath.Dir(stagePath), 0755); e != nil {
			writeErr(w, errAPI(e))
			return
		}
		src, e := f.Open()
		if e != nil {
			writeAPIError(w, invalid("Invalid ZIP archive"))
			return
		}
		dst, e := os.OpenFile(stagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			src.Close()
			writeErr(w, errAPI(e))
			return
		}
		n, copyErr := io.Copy(dst, io.LimitReader(src, 16*1024*1024+1))
		srcErr := src.Close()
		dstErr := dst.Close()
		if copyErr != nil || n > 16*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		if srcErr != nil || dstErr != nil {
			writeErr(w, errAPI(firstError(srcErr, dstErr)))
			return
		}
		expanded += n
		if expanded > 256*1024*1024 {
			writeAPIError(w, invalid("Archive expanded size limit exceeded"))
			return
		}
		files = append(files, relative)
	}
	for rel := range dirs {
		target := filepath.Join(destDir, rel)
		info, e := os.Lstat(target)
		if e == nil && !info.IsDir() {
			writeAPIError(w, conflict("Archive directory conflicts with an existing file: "+rel))
			return
		}
		if e != nil && !os.IsNotExist(e) {
			writeErr(w, errAPI(e))
			return
		}
		if ae := a.checkNoFileParent(target); ae != nil {
			writeErr(w, ae)
			return
		}
	}
	uploadTime := formatUTC(time.Now())
	pageCreated := map[string]string{}
	for _, rel := range files {
		target := filepath.Join(destDir, rel)
		if ae := a.checkNoFileParent(filepath.Dir(target)); ae != nil {
			writeErr(w, ae)
			return
		}
		info, e := os.Lstat(target)
		if e == nil && (!overwrite || info.IsDir()) {
			writeAPIError(w, conflict("Archive would replace an existing resource: "+rel))
			return
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			writeAPIError(w, conflict("Archive would replace an incompatible resource: "+rel))
			return
		}
		if e != nil && !os.IsNotExist(e) {
			writeErr(w, errAPI(e))
			return
		}
		if strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".html") {
			created := uploadTime
			var stored string
			lookupErr := a.db.QueryRow(`SELECT created_at FROM content_page_times WHERE path=?`, uploadContentPath(dest, rel)).Scan(&stored)
			if lookupErr == nil {
				created = stored
			} else if lookupErr == sql.ErrNoRows && e == nil {
				created = info.ModTime().UTC().Format(time.RFC3339Nano)
			} else if lookupErr != sql.ErrNoRows {
				writeErr(w, errAPI(lookupErr))
				return
			}
			pageCreated[rel] = created
		}
	}
	createdDirs := []string{}
	removeCreatedDirs := func() {
		for i := len(createdDirs) - 1; i >= 0; i-- {
			_ = os.Remove(createdDirs[i])
		}
	}
	for _, dir := range append([]string{destDir}, directoryTargets(destDir, dirs, files)...) {
		if e = ensureContentDirectory(a.content, dir, &createdDirs); e != nil {
			removeCreatedDirs()
			writeErr(w, errAPI(e))
			return
		}
	}
	if e = os.Mkdir(backupDir, 0755); e != nil {
		removeCreatedDirs()
		writeErr(w, errAPI(e))
		return
	}
	type installedFile struct {
		target, backup string
		installed      bool
	}
	installed := []installedFile{}
	rollback := func() {
		for i := len(installed) - 1; i >= 0; i-- {
			item := installed[i]
			if item.installed {
				_ = os.Remove(item.target)
			}
			if item.backup != "" {
				_ = os.Rename(item.backup, item.target)
			}
		}
		removeCreatedDirs()
	}
	urls := []string{}
	for i, rel := range files {
		target := filepath.Join(destDir, rel)
		item := installedFile{target: target}
		if info, statErr := os.Lstat(target); statErr == nil {
			if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				rollback()
				writeAPIError(w, conflict("Archive would replace an incompatible resource: "+rel))
				return
			}
			if !overwrite {
				rollback()
				writeAPIError(w, conflict("Archive would replace an existing resource: "+rel))
				return
			}
			item.backup = filepath.Join(backupDir, fmt.Sprintf("%d", i))
			if e = os.Rename(target, item.backup); e != nil {
				rollback()
				writeErr(w, errAPI(e))
				return
			}
		} else if !os.IsNotExist(statErr) {
			rollback()
			writeErr(w, errAPI(statErr))
			return
		}
		installed = append(installed, item)
		if e = os.Rename(filepath.Join(payload, rel), target); e != nil {
			rollback()
			writeErr(w, errAPI(e))
			return
		}
		installed[len(installed)-1].installed = true
		child := dest
		if child == "/" {
			child = ""
		}
		child += "/" + filepath.ToSlash(rel)
		urls = append(urls, absContentURL(a.baseURL, child))
	}
	if len(pageCreated) > 0 {
		tx, txErr := a.db.Begin()
		if txErr != nil {
			rollback()
			writeErr(w, errAPI(txErr))
			return
		}
		for rel, created := range pageCreated {
			_, txErr = tx.Exec(`INSERT INTO content_page_times(path,created_at,updated_at) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET updated_at=excluded.updated_at`, uploadContentPath(dest, rel), created, uploadTime)
			if txErr != nil {
				tx.Rollback()
				rollback()
				writeErr(w, errAPI(txErr))
				return
			}
		}
		if txErr = tx.Commit(); txErr != nil {
			rollback()
			writeErr(w, errAPI(txErr))
			return
		}
	}
	writeJSON(w, 201, map[string]any{"path": dest, "urls": urls})
}

func directoryTargets(dest string, dirs map[string]bool, files []string) []string {
	all := []string{}
	for rel := range dirs {
		all = append(all, filepath.Join(dest, rel))
	}
	for _, rel := range files {
		all = append(all, filepath.Dir(filepath.Join(dest, rel)))
	}
	return all
}

func uploadContentPath(dest, rel string) string {
	if dest == "/" {
		return "/" + filepath.ToSlash(rel)
	}
	return dest + "/" + filepath.ToSlash(rel)
}

func ensureContentDirectory(root, target string, created *[]string) error {
	if !within(root, target) {
		return fmt.Errorf("directory escapes content root")
	}
	if filepath.Clean(target) == filepath.Clean(root) {
		return nil
	}
	info, err := os.Lstat(target)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(target)
	if err = ensureContentDirectory(root, parent, created); err != nil {
		return err
	}
	if err = os.Mkdir(target, 0755); err != nil {
		return err
	}
	*created = append(*created, target)
	return nil
}

func firstError(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func (a *app) checkNoFileParent(target string) *apiError {
	for p := target; within(a.content, p); p = filepath.Dir(p) {
		info, e := os.Stat(p)
		if e == nil && !info.IsDir() {
			return conflict("A file blocks content path: " + p)
		}
		if errors.Is(e, syscall.ENOTDIR) {
			return conflict("A file blocks content path: " + p)
		}
		if e != nil && !os.IsNotExist(e) {
			return errAPI(e)
		}
		if filepath.Clean(p) == filepath.Clean(a.content) {
			break
		}
	}
	return nil
}
func (a *app) canonicalContentPath(raw string) (string, *apiError) { return canonicalContentPath(raw) }
func canonicalContentPath(raw string) (string, *apiError) {
	if !utf8.ValidString(raw) {
		return "", invalid("Content paths must be valid UTF-8")
	}
	if raw == "" {
		return "/", nil
	}
	if !strings.HasPrefix(raw, "/") {
		return "", invalid("Content paths must start with /")
	}
	if raw == "/" {
		return raw, nil
	}
	if strings.HasSuffix(raw, "/") {
		return "", invalid("Trailing slash is not canonical")
	}
	parts := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." || strings.Contains(p, "\\") || hasControl(p) {
			return "", invalid("Invalid content path")
		}
		if i == 0 && reservedRoot[p] {
			return "", invalid("Reserved root path")
		}
	}
	return raw, nil
}
func (a *app) contentPath(raw string) (string, *apiError) {
	clean, e := canonicalContentPath(raw)
	if e != nil {
		return "", e
	}
	rel := strings.TrimPrefix(clean, "/")
	target := filepath.Join(a.content, filepath.FromSlash(rel))
	if !within(a.content, target) {
		return "", invalid("Path escapes content directory")
	}
	for p := target; p != a.content; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", invalid("Symbolic links are not permitted in content paths")
		}
		if errors.Is(e, syscall.ENOTDIR) {
			return "", conflict("A file blocks content path: " + p)
		}
		if e != nil && !os.IsNotExist(e) {
			return "", errAPI(e)
		}
	}
	return target, nil
}
func within(root, p string) bool {
	r, e := filepath.Rel(root, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
func regexpDrive(s string) bool {
	return len(s) >= 2 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':'
}

func (a *app) contentUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	raw := r.URL.Path
	clean, e := canonicalContentPath(raw)
	if e != nil {
		writeErr(w, e)
		return
	}
	target, e := a.contentPath(clean)
	if e != nil {
		writeErr(w, e)
		return
	}
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		writeAPIError(w, missing("Content"))
		return
	}
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	if info.IsDir() {
		if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
			if err := r.ParseForm(); err != nil {
				writeHTML(w, http.StatusBadRequest, a.pageShell("<h1>Invalid form</h1><p>Could not read the directory name.</p>", "Invalid form"))
				return
			}
			name := r.Form.Get("name")
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
				writeHTML(w, http.StatusUnprocessableEntity, a.pageShell("<h1>Invalid directory name</h1><p>Enter a single directory name.</p>", "Invalid directory name"))
				return
			}
			child := name
			if clean != "/" {
				child = clean + "/" + name
			} else {
				child = "/" + name
			}
			created, ae := a.createDirectory(child)
			if ae != nil {
				status := ae.Status
				message := esc(ae.Message)
				writeHTML(w, status, a.pageShell("<h1>Could not create directory</h1><p>"+message+"</p>", "Directory error"))
				return
			}
			http.Redirect(w, r, urlPath(created), http.StatusSeeOther)
			return
		}
		d, ae := a.directory(clean)
		if ae != nil {
			writeErr(w, ae)
			return
		}
		writeHTML(w, 200, a.directoryHTML(d))
		return
	}
	if r.Method != "GET" {
		writeAPIError(w, methodNotAllowed())
		return
	}
	if !info.Mode().IsRegular() {
		writeAPIError(w, missing("Content"))
		return
	}
	if strings.HasSuffix(target, ".md") {
		data, err := os.ReadFile(target)
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		writeHTML(w, 200, a.pageShell(markdownHTML(string(data)), clean))
		return
	}
	if strings.HasSuffix(target, ".html") {
		data, err := os.ReadFile(target)
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		writeHTML(w, 200, string(data))
		return
	}
	typeName := mime.TypeByExtension(filepath.Ext(target))
	if typeName == "" {
		typeName = "application/octet-stream"
	}
	data, err := os.ReadFile(target)
	if err != nil {
		writeErr(w, errAPI(err))
		return
	}
	writeText(w, 200, string(data), typeName)
}

func (a *app) directoryHTML(d map[string]any) string {
	pathName := d["path"].(string)
	children, _ := d["children"].([]map[string]any)
	var b strings.Builder
	b.WriteString("<h1>" + esc(pathName) + "</h1><nav aria-label=breadcrumbs><a href=\"/\">root</a>")
	current := ""
	for _, part := range splitPath(strings.TrimPrefix(pathName, "/")) {
		current += "/" + part
		b.WriteString(" / <a href=\"" + urlPath(current) + "\">" + esc(part) + "</a>")
	}
	b.WriteString("</nav><ul>")
	for _, child := range children {
		u := child["url"].(string)
		u = strings.TrimPrefix(u, a.baseURL)
		name := child["name"].(string)
		suffix := ""
		if child["type"] == "directory" {
			suffix = "/"
		}
		b.WriteString("<li><a href=\"" + htmlAttr(u) + "\">" + esc(name) + suffix + "</a> <small>" + esc(child["type"]) + "</small></li>")
	}
	b.WriteString("</ul><form method=post action=\"" + htmlAttr(urlPath(pathName)) + "\"><label>New directory<input name=name required></label><button type=submit>Create directory</button></form>")
	if pathName == "/" {
		b.WriteString("<h2>Tables</h2><ul>")
		groups, _ := a.listGroups()
		for _, g := range groups {
			tables, _ := a.listTables(g["name"].(string))
			for _, t := range tables {
				href := "/t/" + esc(g["name"]) + "/" + esc(t["name"])
				b.WriteString("<li><a href=\"" + href + "\">" + esc(g["label"]) + " / " + esc(t["label"]) + "</a></li>")
			}
		}
		b.WriteString("</ul>")
	}
	return a.pageShell(b.String(), pathName)
}
func htmlAttr(s string) string { return strings.ReplaceAll(esc(s), "&#39;", "&#39;") }

func markdownHTML(source string) string {
	var out strings.Builder
	inList, inCode := false, false
	var code strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "```") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			if inCode {
				out.WriteString("<pre><code>" + esc(code.String()) + "</code></pre>")
				code.Reset()
				inCode = false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			code.WriteString(line + "\n")
			continue
		}
		if strings.HasPrefix(line, "#") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			n := 0
			for n < len(line) && line[n] == '#' {
				n++
			}
			if n <= 6 && len(line) > n && line[n] == ' ' {
				out.WriteString(fmt.Sprintf("<h%d>%s</h%d>", n, inlineMarkdown(line[n+1:]), n))
			} else {
				out.WriteString("<p>" + inlineMarkdown(line) + "</p>")
			}
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inlineMarkdown(line[2:]) + "</li>")
		} else {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			if strings.TrimSpace(line) != "" {
				out.WriteString("<p>" + inlineMarkdown(line) + "</p>")
			}
		}
	}
	if inList {
		out.WriteString("</ul>")
	}
	if inCode {
		out.WriteString("<pre><code>" + esc(code.String()) + "</code></pre>")
	}
	return out.String()
}
func inlineMarkdown(s string) string {
	safe := esc(s)
	safe = regexpReplace(safe, "`([^`]+)`", "<code>$1</code>")
	safe = regexpReplace(safe, `\*\*([^*]+)\*\*`, `<strong>$1</strong>`)
	safe = regexpReplace(safe, `\*([^*]+)\*`, `<em>$1</em>`)
	re := regexp.MustCompile(`\[([^]]+)\]\((https?://[^ )]+)\)`)
	return re.ReplaceAllString(safe, `<a href="$2">$1</a>`)
}
func regexpReplace(s, pattern, replacement string) string {
	return regexp.MustCompile(pattern).ReplaceAllString(s, replacement)
}
func pageTitle(p string) string { return path.Base(p) }

var _ = json.Valid
var _ = url.QueryEscape
