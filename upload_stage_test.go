package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractionRunsOutsideContentLock pins that slow text extraction never
// blocks other writers (finding F4).
func TestExtractionRunsOutsideContentLock(t *testing.T) {
	a := newTestApp(t)
	heldDuringExtraction := false
	original := extractNative
	extractNative = func(p, ct string, data []byte) (string, string) {
		if a.contentLock().TryLock() {
			a.contentLock().Unlock()
		} else {
			heldDuringExtraction = true
		}
		return original(p, ct, data)
	}
	t.Cleanup(func() { extractNative = original })
	putFile(t, a, "default", "/notes/a.txt", "searchable words", "text/plain")
	if heldDuringExtraction {
		t.Fatal("extraction ran while the content lock was held")
	}
}

// TestFailedCatalogWriteLeavesOldBytes pins that bytes and catalog commit
// together: a failing catalog update must not replace the stored file.
func TestFailedCatalogWriteLeavesOldBytes(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/a.txt", "old", "text/plain")
	if _, err := a.db.Exec(`CREATE TRIGGER fail_update BEFORE UPDATE ON content_entries BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	w := testRequest(t, a, http.MethodPut, filesURL("default", "/a.txt"), "new", "text/plain")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if got := diskFile(t, a, "default", "/a.txt"); got != "old" {
		t.Fatalf("disk bytes = %q, want old", got)
	}
	assertNoStagedFiles(t, a)
}

// TestWebDAVAbortedPutStoresNothing pins Review Focus #2: x/net/webdav calls
// Close even when copying the body failed; nothing may be committed.
func TestWebDAVAbortedPutStoresNothing(t *testing.T) {
	a := newTestApp(t)
	a.webdavEnabled = true
	r := httptest.NewRequest(http.MethodPut, "/api/v1/default/files/dav/partial.bin", failingReader{})
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code < 400 {
		t.Fatalf("aborted PUT status = %d, want an error", w.Code)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "partial.bin")); !os.IsNotExist(err) {
		t.Fatalf("aborted PUT left a file: %v", err)
	}
	if _, found := contentEntryForPath(t, a, "default", "/partial.bin"); found {
		t.Fatal("aborted PUT left a catalog row")
	}
	assertNoStagedFiles(t, a)
}

func TestPutStreamsAndRecordsHash(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/b.txt", "hello", "text/plain")
	// sha256("hello")
	if entry["sha256"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" || entry["size"] != float64(5) {
		t.Fatalf("entry = %#v", entry)
	}
	results := testRequest(t, a, http.MethodGet, "/api/v1/default/search?q=hello", nil, "")
	if !strings.Contains(results.Body.String(), "/b.txt") {
		t.Fatalf("search did not find streamed text: %s", results.Body.String())
	}
	assertNoStagedFiles(t, a)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("client went away") }

func assertNoStagedFiles(t *testing.T, a *app) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(a.content, "*", ".clio-upload-*"))
	if len(matches) > 0 {
		t.Fatalf("staged temp files left behind: %v", matches)
	}
}
