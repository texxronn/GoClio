package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// countHashes replaces the rescan hash function for one test.
func countHashes(t *testing.T) *int {
	t.Helper()
	calls := 0
	original := hashContentFile
	hashContentFile = func(p string) (string, error) {
		calls++
		return original(p)
	}
	t.Cleanup(func() { hashContentFile = original })
	return &calls
}

func writeDiskFile(t *testing.T, a *app, project, p string, data []byte) string {
	t.Helper()
	target := filepath.Join(a.content, project, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestRescanSkipsHashingUnchangedFiles(t *testing.T) {
	a := newTestApp(t)
	scoped := a.withProject("default")
	writeDiskFile(t, a, "default", "/photos/a.jpg", []byte("jpeg-bytes"))
	putFile(t, a, "default", "/docs/b.txt", "hello", "text/plain")
	// First rescan adopts and stamps; second backfills anything left unknown.
	for i := 0; i < 2; i++ {
		if _, err := scoped.rescanContent(); err != nil {
			t.Fatalf("rescan %d: %v", i, err)
		}
	}
	hashes := countHashes(t)
	summary, err := scoped.rescanContent()
	if err != nil {
		t.Fatal(err)
	}
	if *hashes != 0 {
		t.Errorf("unchanged rescan hashed %d file(s), want 0", *hashes)
	}
	if summary.Added+summary.Removed+summary.Refreshed != 0 {
		t.Errorf("unchanged rescan summary = %+v", summary)
	}
}

func TestRescanDetectsChangedMtimeSameSize(t *testing.T) {
	a := newTestApp(t)
	scoped := a.withProject("default")
	target := writeDiskFile(t, a, "default", "/n.txt", []byte("alpha"))
	for i := 0; i < 2; i++ {
		if _, err := scoped.rescanContent(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("gamma"), 0644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(target, later, later); err != nil {
		t.Fatal(err)
	}
	summary, err := scoped.rescanContent()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Refreshed != 1 {
		t.Errorf("refreshed = %d, want 1", summary.Refreshed)
	}
}

func TestRescanFullRehashesUnchangedMetadata(t *testing.T) {
	a := newTestApp(t)
	scoped := a.withProject("default")
	target := writeDiskFile(t, a, "default", "/n.txt", []byte("alpha"))
	for i := 0; i < 2; i++ {
		if _, err := scoped.rescanContent(); err != nil {
			t.Fatal(err)
		}
	}
	info, _ := os.Stat(target)
	if err := os.WriteFile(target, []byte("gamma"), 0644); err != nil {
		t.Fatal(err)
	}
	// Restore the old modification time, as rsync -t would.
	if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/rescan?full=true", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("full rescan status = %d: %s", w.Code, w.Body.String())
	}
	var summary map[string]any
	testJSON(t, w, &summary)
	if summary["refreshed"] != float64(1) {
		t.Errorf("full rescan summary = %v, want refreshed 1", summary)
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/rescan?full=maybe", nil, ""), http.StatusUnprocessableEntity)
}

// TestRescanDoesNotRereadUnchangedNonTextFiles pins F3: a photo (non-extractable)
// and a scanned/image-only PDF (extractable but no text layer) must not be read
// again by a normal rescan; only `full=true` re-derives them.
func TestRescanDoesNotRereadUnchangedNonTextFiles(t *testing.T) {
	a := newTestApp(t)
	scoped := a.withProject("default")
	big := make([]byte, nativeExtractReadLimit+1)
	writeDiskFile(t, a, "default", "/photos/a.jpg", []byte("jpeg-bytes"))
	writeDiskFile(t, a, "default", "/scans/scan.pdf", tinyPDF(""))
	writeDiskFile(t, a, "default", "/huge.txt", big)
	// Two warm-up rescans adopt and index every entry.
	for i := 0; i < 2; i++ {
		if _, err := scoped.rescanContent(); err != nil {
			t.Fatalf("rescan %d: %v", i, err)
		}
	}
	reads := 0
	original := readContentFile
	readContentFile = func(p string) ([]byte, error) {
		reads++
		return original(p)
	}
	t.Cleanup(func() { readContentFile = original })

	if _, err := scoped.rescanContent(); err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Errorf("normal rescan read %d unchanged file(s), want 0", reads)
	}
	// A full rescan deliberately re-reads the extractable entries.
	if _, err := scoped.rescanContentMode(true); err != nil {
		t.Fatal(err)
	}
	if reads == 0 {
		t.Error("full rescan read no files, want it to re-derive the index")
	}
}
