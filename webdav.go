package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

// davLockSystems holds one in-memory WebDAV lock system per project for the
// life of the process. Locking is provided but is not persistent across
// restarts (section 64.10).
var davLockSystems sync.Map // project name -> webdav.LockSystem

func (a *app) davLockSystem() webdav.LockSystem {
	project := a.projectName()
	if ls, ok := davLockSystems.Load(project); ok {
		return ls.(webdav.LockSystem)
	}
	ls, _ := davLockSystems.LoadOrStore(project, webdav.NewMemLS())
	return ls.(webdav.LockSystem)
}

// webdavMount serves the WebDAV mount whose URL prefix ends in .../files/dav
// (sections 64.10 and 66.8). WebDAV is opt-in: when disabled the mount does not
// exist and returns 404 like any other absent route. The mount runs behind the
// same authentication and transport policy as every other route because
// ServeHTTP authorizes before routing (section 54).
func (a *app) webdavMount(w http.ResponseWriter, r *http.Request, prefix string) {
	if !a.webdavEnabled {
		writeAPIError(w, missing("Endpoint"))
		return
	}
	// Bound a PUT body to the 16 MiB upload limit before the handler buffers
	// it (section 64.12). A declared Content-Length over the limit is rejected
	// immediately; a chunked or unknown-length body is read at most
	// limit+1 bytes so an over-limit body still returns 413 rather than
	// buffering unbounded memory (or surfacing as a 500 on Close).
	if r.Method == http.MethodPut {
		if r.ContentLength > fileUploadLimit {
			writeAPIError(w, &apiError{http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"})
			return
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, fileUploadLimit+1))
		if readErr != nil {
			writeAPIError(w, invalid("Malformed request body"))
			return
		}
		if int64(len(body)) > fileUploadLimit {
			writeAPIError(w, &apiError{http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	// A DELETE that would break an attachment reference returns 409 before the
	// WebDAV handler runs; its RemoveAll error cannot carry a custom status
	// (section 64.9).
	if r.Method == http.MethodDelete {
		if conflictErr := a.webdavDeleteConflict(r.URL.Path, prefix); conflictErr != nil {
			writeErr(w, conflictErr)
			return
		}
	}
	// A WebDAV GET/HEAD streams stored bytes directly. Apply the same download
	// protections as a path URL: nosniff on every response and an attachment
	// disposition for non-page content, so a file cannot execute in Clio's
	// origin. Pages keep their raw type without an attachment (section 64.5).
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if contentKind(davCleanName(strings.TrimPrefix(r.URL.Path, prefix))) != "page" {
			w.Header().Set("Content-Disposition", "attachment")
		}
	}
	handler := &webdav.Handler{
		Prefix:     prefix,
		FileSystem: davFileSystem{a: a},
		LockSystem: a.davLockSystem(),
	}
	handler.ServeHTTP(w, r)
}

// webdavDeleteConflict resolves a DELETE target to its entry IDs and enforces
// the attachment-reference integrity rule (section 64.9). Invalid or absent
// targets are left to the WebDAV handler so it can return its standard 404.
func (a *app) webdavDeleteConflict(requestPath, prefix string) *apiError {
	clean, ae := canonicalContentPath(davCleanName(strings.TrimPrefix(requestPath, prefix)))
	if ae != nil || clean == "/" {
		return nil
	}
	target, ae := a.contentPath(clean)
	if ae != nil {
		return nil
	}
	info, err := os.Lstat(target)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		ids, err := a.contentEntryIDsUnder(clean)
		if err != nil {
			return errAPI(err)
		}
		return a.attachmentDeleteConflict(ids)
	}
	entry, found, err := a.contentEntryByPath(clean)
	if err != nil {
		return errAPI(err)
	}
	if !found {
		return nil
	}
	return a.attachmentDeleteConflict([]string{entry.ID})
}

// davFileSystem adapts the project content tree to the webdav.FileSystem
// interface. Every mutation is delegated to the files API helpers so
// validation, limits, entry IDs, timestamps and the text index stay consistent
// with the REST API (section 64.10).
type davFileSystem struct{ a *app }

// davCleanName normalizes a WebDAV name: empty is the mount root, and a
// trailing slash on a collection is removed before the strict content-path
// rules run. Interior "." and ".." segments are still rejected by
// canonicalContentPath, so this cannot introduce a traversal.
func davCleanName(name string) string {
	if name == "" {
		return "/"
	}
	if name != "/" {
		name = strings.TrimSuffix(name, "/")
	}
	return name
}

// resolve maps a WebDAV name (a slash path relative to the mount root) to a
// canonical content path. It reports false for a path the files partition
// rejects, such as a traversal or a reserved segment.
func (fs davFileSystem) resolve(name string) (string, bool) {
	clean, ae := fs.a.canonicalContentPath(davCleanName(name))
	if ae != nil {
		return "", false
	}
	return clean, true
}

func (fs davFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	clean, ok := fs.resolve(name)
	if !ok || clean == "/" {
		return os.ErrExist
	}
	if _, ae := fs.a.createDirectory(clean); ae != nil {
		return apiErrorToOSError(ae)
	}
	return nil
}

func (fs davFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	clean, ok := fs.resolve(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	target, ae := fs.a.contentPath(clean)
	if ae != nil {
		return nil, os.ErrNotExist
	}
	if clean == "/" {
		// The project content root is created lazily so it lists as empty,
		// matching the files API.
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, err
			}
		}
	}
	info, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	return info, nil
}

func (fs davFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	clean, ok := fs.resolve(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	target, ae := fs.a.contentPath(clean)
	if ae != nil {
		return nil, os.ErrNotExist
	}
	// PUT, COPY and lock-null creation all open with O_TRUNC; buffer those and
	// commit through the shared content-file helper on Close. Any other flag
	// (for example PROPPATCH's O_RDWR) uses the OS file directly.
	if flag&os.O_TRUNC != 0 {
		if clean == "/" {
			return nil, os.ErrExist
		}
		return &davWriteFile{a: fs.a, clean: clean, now: time.Now()}, nil
	}
	if clean == "/" {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, err
			}
		}
	}
	// PROPPATCH opens with O_RDWR but only inspects dead properties. A
	// directory cannot be opened for writing, so fall back to a read open
	// rather than failing the request.
	openFlag := flag
	if info, err := os.Lstat(target); err == nil && info.IsDir() {
		openFlag = os.O_RDONLY
	}
	f, err := os.OpenFile(target, openFlag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fs davFileSystem) RemoveAll(ctx context.Context, name string) error {
	clean, ok := fs.resolve(name)
	if !ok {
		return os.ErrNotExist
	}
	if clean == "/" {
		return os.ErrInvalid
	}
	if ae := fs.a.removeContentPath(clean); ae != nil {
		return apiErrorToOSError(ae)
	}
	return nil
}

func (fs davFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	from, ok := fs.resolve(oldName)
	if !ok {
		return os.ErrNotExist
	}
	to, ok := fs.resolve(newName)
	if !ok {
		return os.ErrNotExist
	}
	if from == "/" || to == "/" {
		return os.ErrInvalid
	}
	if ae := fs.a.transferContent(from, to, false); ae != nil {
		return apiErrorToOSError(ae)
	}
	return nil
}

// davWriteFile buffers a WebDAV write and commits it on Close through the
// shared content-file helper, so a PUT or a copied destination gets the same
// ID, timestamp, limit and index treatment as the files API (section 64.10).
type davWriteFile struct {
	a     *app
	clean string
	now   time.Time
	buf   bytes.Buffer
	done  bool
}

func (f *davWriteFile) Read([]byte) (int, error) { return 0, errors.New("webdav: write-only file") }
func (f *davWriteFile) Seek(int64, int) (int64, error) {
	return 0, errors.New("webdav: write-only file")
}
func (f *davWriteFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, errors.New("webdav: not a directory")
}
func (f *davWriteFile) Write(p []byte) (int, error) {
	// Defensive bound: webdavMount caps the request body before delegation,
	// but this write path must never buffer unbounded either (section 64.12).
	if int64(f.buf.Len())+int64(len(p)) > fileUploadLimit {
		return 0, errors.New("webdav: upload exceeds the content limit")
	}
	return f.buf.Write(p)
}

// Stat reports the buffered size so the WebDAV handler can compute an ETag
// before the write is committed.
func (f *davWriteFile) Stat() (os.FileInfo, error) {
	return davFileInfo{name: path.Base(f.clean), size: int64(f.buf.Len()), mode: 0644, mod: f.now}, nil
}

func (f *davWriteFile) Close() error {
	if f.done {
		return nil
	}
	f.done = true
	_, _, ae := f.a.storeContentFile(f.clean, f.buf.Bytes(), "")
	return apiErrorToOSError(ae)
}

// davFileInfo describes a buffered WebDAV write before it is committed.
type davFileInfo struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (fi davFileInfo) Name() string       { return fi.name }
func (fi davFileInfo) Size() int64        { return fi.size }
func (fi davFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi davFileInfo) ModTime() time.Time { return fi.mod }
func (fi davFileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi davFileInfo) Sys() any           { return nil }

// apiErrorToOSError maps an API error onto an os error so the WebDAV handler
// chooses a standard status code: not-found, existence/conflict and permission
// errors are recognisable, and anything else is a generic failure.
func apiErrorToOSError(ae *apiError) error {
	if ae == nil {
		return nil
	}
	switch ae.Status {
	case http.StatusNotFound:
		return os.ErrNotExist
	case http.StatusConflict:
		return os.ErrExist
	case http.StatusForbidden, http.StatusUnauthorized:
		return os.ErrPermission
	default:
		return errors.New(ae.Message)
	}
}
