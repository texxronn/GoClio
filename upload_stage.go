package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

// errUploadTooLarge is returned by uploadStage.Write past the limit.
var errUploadTooLarge = errors.New("upload exceeds the content limit")

// uploadStage streams an upload to a temporary file inside the project content
// root, hashing as it writes, so bytes are never held in memory and the final
// rename stays on one filesystem (sections 64.4 and 64.12). The ".clio-" name
// keeps it out of listings and rescans (section 36.1).
type uploadStage struct {
	file     *os.File
	hash     hash.Hash
	size     int64
	limit    int64
	exceeded bool
	failed   bool
}

func (a *app) newUploadStage(limit int64) (*uploadStage, *apiError) {
	root, ae := a.contentRootChecked()
	if ae != nil {
		return nil, ae
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, errAPI(err)
	}
	f, err := os.CreateTemp(root, ".clio-upload-*.tmp")
	if err != nil {
		return nil, errAPI(err)
	}
	return &uploadStage{file: f, hash: sha256.New(), limit: limit}, nil
}

func (s *uploadStage) Write(p []byte) (int, error) {
	if s.size+int64(len(p)) > s.limit {
		s.exceeded, s.failed = true, true
		return 0, errUploadTooLarge
	}
	n, err := s.file.Write(p)
	s.hash.Write(p[:n])
	s.size += int64(n)
	if err != nil {
		s.failed = true
	}
	return n, err
}

func (s *uploadStage) Size() int64    { return s.size }
func (s *uploadStage) Exceeded() bool { return s.exceeded }

// discard removes the staged bytes; it is safe to call more than once.
func (s *uploadStage) discard() {
	_ = s.file.Close()
	_ = os.Remove(s.file.Name())
}

// finish flushes the staged bytes and derives the catalog values, extracting
// native text from the staged file when it can yield any (section 64.6).
func (s *uploadStage) finish(clean, contentType string) (contentWrite, *apiError) {
	if err := s.file.Sync(); err != nil {
		return contentWrite{}, errAPI(err)
	}
	if err := s.file.Close(); err != nil {
		return contentWrite{}, errAPI(err)
	}
	if contentKind(clean) == "page" || contentType == "" {
		contentType = contentMediaType(clean)
	}
	cw := contentWrite{Size: s.size, SHA256: hex.EncodeToString(s.hash.Sum(nil)), ContentType: contentType, Title: capIndexedText(path.Base(clean))}
	if nativeExtractable(clean, contentType) && s.size <= nativeExtractReadLimit {
		data, err := os.ReadFile(s.file.Name())
		if err != nil {
			return contentWrite{}, errAPI(err)
		}
		cw.Title, cw.Body = extractNative(clean, contentType, data)
	}
	return cw, nil
}

// commitUpload takes the content lock and commits a finished stage.
func (a *app) commitUpload(clean string, stage *uploadStage, cw contentWrite) (contentEntry, bool, *apiError) {
	a.contentLock().Lock()
	defer a.contentLock().Unlock()
	return a.commitUploadLocked(clean, stage, cw)
}

// commitUploadLocked renames a finished stage into place and records it, with
// the catalog row and index row in one transaction that commits only after the
// rename succeeds. The caller holds the content lock. The stage is always
// consumed: renamed on success, removed on failure.
func (a *app) commitUploadLocked(clean string, stage *uploadStage, cw contentWrite) (contentEntry, bool, *apiError) {
	defer stage.discard() // no-op after a successful rename
	target, ae := a.contentPath(clean)
	if ae != nil {
		return contentEntry{}, false, ae
	}
	existed, createdAt := false, ""
	if info, err := os.Lstat(target); err == nil {
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
	} else if !os.IsNotExist(err) {
		return contentEntry{}, false, errAPI(err)
	}
	if ae = a.checkNoFileParent(filepath.Dir(target)); ae != nil {
		return contentEntry{}, false, ae
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return contentEntry{}, false, errAPI(err)
	}
	tx, err := a.db.Begin()
	if err != nil {
		return contentEntry{}, false, errAPI(err)
	}
	if err = saveContentWrite(tx, a.project, clean, cw, createdAt); err != nil {
		_ = tx.Rollback()
		return contentEntry{}, false, errAPI(err)
	}
	if err = os.Rename(stage.file.Name(), target); err != nil {
		_ = tx.Rollback()
		return contentEntry{}, false, errAPI(err)
	}
	if err = tx.Commit(); err != nil {
		// The bytes are in place but uncataloged; the next rescan adopts them.
		return contentEntry{}, false, errAPI(err)
	}
	entry, found, err := a.contentEntryByPath(clean)
	if err != nil || !found {
		return contentEntry{}, false, errAPI(firstError(err, errors.New("content entry missing after write")))
	}
	return entry, existed, nil
}

// storeContentStream stages a request body and commits it at a canonical
// path. The files API and WebDAV share it so both apply the same limit,
// timestamps and index updates (sections 64.4, 64.10 and 64.12).
func (a *app) storeContentStream(clean string, body io.Reader, contentType string, limit int64) (contentEntry, bool, *apiError) {
	if ae := rejectIgnoredContentPath(clean); ae != nil {
		return contentEntry{}, false, ae
	}
	stage, ae := a.newUploadStage(limit)
	if ae != nil {
		return contentEntry{}, false, ae
	}
	if _, err := io.Copy(stage, body); err != nil {
		stage.discard()
		var tooLarge *http.MaxBytesError
		if stage.Exceeded() || errors.As(err, &tooLarge) {
			return contentEntry{}, false, &apiError{http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"}
		}
		return contentEntry{}, false, invalid("Malformed request body")
	}
	cw, ae := stage.finish(clean, contentType)
	if ae != nil {
		stage.discard()
		return contentEntry{}, false, ae
	}
	return a.commitUpload(clean, stage, cw)
}
