package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func contentEntryRow(t *testing.T, a *app, project, path string) (contentEntry, bool) {
	t.Helper()
	entry, found, err := a.withProject(project).contentEntryByPath(path)
	if err != nil {
		t.Fatalf("read content entry %s:%s: %v", project, path, err)
	}
	return entry, found
}

func TestContentEntryIdentityStableAcrossReplace(t *testing.T) {
	a := newTestApp(t)
	create := testRequest(t, a, http.MethodPut, filesURL("default", "/docs/a.md"), "version one", "text/markdown")
	if create.Code != http.StatusCreated {
		t.Fatalf("create page: %d %s", create.Code, create.Body.String())
	}
	first, found := contentEntryRow(t, a, "default", "/docs/a.md")
	if !found {
		t.Fatal("content entry missing after create")
	}
	if first.Kind != "page" || first.ContentType != "text/markdown" {
		t.Errorf("entry kind/type = %q/%q, want page/text/markdown", first.Kind, first.ContentType)
	}
	sum := sha256.Sum256([]byte("version one"))
	if first.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("entry sha256 = %q, want %q", first.SHA256, hex.EncodeToString(sum[:]))
	}
	if first.Size != int64(len("version one")) {
		t.Errorf("entry size = %d, want %d", first.Size, len("version one"))
	}

	replace := testRequest(t, a, http.MethodPut, filesURL("default", "/docs/a.md"), "version two, longer", "text/markdown")
	if replace.Code != http.StatusOK {
		t.Fatalf("replace page: %d %s", replace.Code, replace.Body.String())
	}
	second, found := contentEntryRow(t, a, "default", "/docs/a.md")
	if !found {
		t.Fatal("content entry missing after replace")
	}
	if second.ID != first.ID {
		t.Errorf("entry id changed across replace: %q -> %q", first.ID, second.ID)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("created_at changed across replace: %q -> %q", first.CreatedAt, second.CreatedAt)
	}
	if second.SHA256 == first.SHA256 || second.Size == first.Size {
		t.Errorf("entry bytes were not refreshed: first=%+v second=%+v", first, second)
	}

	// The entry representation reads back the preserved timestamps.
	read := testRequest(t, a, http.MethodGet, filesURL("default", "/docs/a.md"), nil, "")
	var page map[string]any
	testJSON(t, read, &page)
	if page["created_at"] != first.CreatedAt || page["updated_at"] != second.UpdatedAt {
		t.Errorf("entry timestamps = %v/%v, want %v/%v", page["created_at"], page["updated_at"], first.CreatedAt, second.UpdatedAt)
	}
}

func TestContentReconciliationAddsAndRemoves(t *testing.T) {
	a := newTestApp(t)
	root := filepath.Join(a.content, "default")
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "keep.md"), []byte("# Keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	scoped := a.withProject("default")
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	keep, found := contentEntryRow(t, a, "default", "/notes/keep.md")
	if !found || keep.Kind != "page" {
		t.Fatalf("reconciled page entry = %+v found=%v", keep, found)
	}
	loose, found := contentEntryRow(t, a, "default", "/loose.txt")
	if !found || loose.Kind != "file" || loose.Size != 5 {
		t.Fatalf("reconciled file entry = %+v found=%v", loose, found)
	}

	// Add a path, remove another, and rename one directly on disk.
	if err := os.WriteFile(filepath.Join(root, "added.bin"), []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "loose.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "notes", "keep.md"), filepath.Join(root, "notes", "renamed.md")); err != nil {
		t.Fatal(err)
	}
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if _, found := contentEntryRow(t, a, "default", "/loose.txt"); found {
		t.Error("removed path kept its content entry")
	}
	if _, found := contentEntryRow(t, a, "default", "/notes/keep.md"); found {
		t.Error("renamed path kept its old content entry")
	}
	added, found := contentEntryRow(t, a, "default", "/added.bin")
	if !found || added.Kind != "file" {
		t.Fatalf("added file entry = %+v found=%v", added, found)
	}
	renamed, found := contentEntryRow(t, a, "default", "/notes/renamed.md")
	if !found || renamed.ID == keep.ID {
		t.Errorf("on-disk rename should yield a new id: keep=%q renamed=%q", keep.ID, renamed.ID)
	}
}

func TestContentIsolationBetweenProjects(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	for project, content := range map[string]string{"default": "# Default", "bills": "# Bills"} {
		w := testRequest(t, a, http.MethodPut, filesURL(project, "/shared.md"), content, "text/markdown")
		if w.Code != http.StatusCreated {
			t.Fatalf("create page in %s: %d %s", project, w.Code, w.Body.String())
		}
	}
	// Each project has its own subtree and entry.
	for project, want := range map[string]string{"default": "default/shared.md", "bills": "bills/shared.md"} {
		if _, err := os.Stat(filepath.Join(a.content, want)); err != nil {
			t.Errorf("project %s content file missing: %v", project, err)
		}
		entry, found := contentEntryRow(t, a, project, "/shared.md")
		if !found {
			t.Errorf("project %s has no entry", project)
		}
		if project == "bills" && entry.Project != "bills" {
			t.Errorf("bills entry project = %q", entry.Project)
		}
	}
	// The page source does not cross projects.
	billsEntry, found := contentEntryRow(t, a, "bills", "/shared.md")
	if !found {
		t.Fatal("bills page entry missing")
	}
	read := testRequest(t, a, http.MethodGet, "/api/v1/bills/files/"+billsEntry.ID+"/content", nil, "")
	if read.Code != http.StatusOK || read.Body.String() != "# Bills" {
		t.Errorf("bills page content = %d %q", read.Code, read.Body.String())
	}
	// A project with content is not empty and cannot be deleted.
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/projects/bills", nil, ""), http.StatusConflict)
}

func TestContentEntriesMigrationFromPageTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open(querySQLiteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`CREATE TABLE content_page_times (path TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`INSERT INTO content_page_times(path,created_at,updated_at) VALUES('/old/page.md','2026-01-01T00:00:00Z','2026-02-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := openDatabase(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer db.Close()

	var exists int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='content_page_times'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Error("legacy content_page_times table was not dropped")
	}
	var id, project, kind, contentType, created, updated string
	if err = db.QueryRow(`SELECT id,project,kind,content_type,created_at,updated_at FROM content_entries WHERE path='/old/page.md'`).Scan(&id, &project, &kind, &contentType, &created, &updated); err != nil {
		t.Fatalf("migrated entry missing: %v", err)
	}
	if id == "" || project != "default" || kind != "page" || contentType != "text/markdown" {
		t.Errorf("migrated entry = id=%q project=%q kind=%q type=%q", id, project, kind, contentType)
	}
	if created != "2026-01-01T00:00:00Z" || updated != "2026-02-01T00:00:00Z" {
		t.Errorf("migrated timestamps = %q/%q", created, updated)
	}
}

func TestContentLayoutMigrationMovesLegacyRoot(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	if err := os.MkdirAll(filepath.Join(content, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "notes", "a.md"), []byte("# A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "top.txt"), []byte("top"), 0644); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(filepath.Join(root, "clio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &app{db: db, content: content, baseURL: "http://clio.test", contentMu: &sync.Mutex{}}
	if err = a.migrateContentLayout(); err != nil {
		t.Fatalf("migrate content layout: %v", err)
	}
	for _, moved := range []string{"default/notes/a.md", "default/top.txt"} {
		if _, err = os.Stat(filepath.Join(content, moved)); err != nil {
			t.Errorf("legacy content not moved to %s: %v", moved, err)
		}
	}
	if _, err = os.Stat(filepath.Join(content, "notes")); !os.IsNotExist(err) {
		t.Errorf("legacy top-level directory still present: %v", err)
	}
	if err = a.reconcileAllContent(); err != nil {
		t.Fatalf("reconcile all: %v", err)
	}
	if _, found := contentEntryRow(t, a, "default", "/notes/a.md"); !found {
		t.Error("migrated content has no entry")
	}
}

// TestRescanPreservesDeclaredTypeAndEnrichment guards that rescan detects
// change from actual bytes, not from the stored content type: a file uploaded
// with a custom declared type keeps that type and its agent enrichment across a
// reconcile (sections 64.2, 64.6 and 64.8).
func TestRescanPreservesDeclaredTypeAndEnrichment(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/uploads/blob.bin", "custom bytes", "application/x-custom")
	id, _ := entry["id"].(string)
	if entry["content_type"] != "application/x-custom" {
		t.Fatalf("declared content type = %v", entry["content_type"])
	}
	fingerprint := diskFingerprint(t, a, "/uploads/blob.bin")
	if w := putEnrichment(t, a, extractionURL("default", "/uploads/blob.bin"), map[string]any{
		"fingerprint": fingerprint,
		"text":        "agent extracted quokka",
		"provider":    "ocr:test",
	}); w.Code != http.StatusOK {
		t.Fatalf("enrichment status = %d: %s", w.Code, w.Body.String())
	}

	scoped := a.withProject("default")
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stored, found := contentEntryRow(t, a, "default", "/uploads/blob.bin")
	if !found {
		t.Fatal("entry vanished after rescan")
	}
	if stored.ContentType != "application/x-custom" {
		t.Errorf("rescan overwrote the declared content type: %q", stored.ContentType)
	}
	if stored.ID != id {
		t.Errorf("rescan changed the entry id: %s -> %s", id, stored.ID)
	}
	if res := search(t, a, "default", "quokka"); len(res.Data) != 1 || res.Data[0]["source"] != "agent:ocr:test" {
		t.Errorf("agent enrichment lost on rescan: %#v", res.Data)
	}
}

// TestRescanDetectsSameSizeEditAndInvalidatesAgentText guards that a same-size
// external edit is detected by content hash, refreshing the stored hash and
// native text and dropping stale agent text (sections 64.2 and 64.8).
func TestRescanDetectsSameSizeEditAndInvalidatesAgentText(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/docs/note.txt", "alpha", "text/plain")
	id, _ := entry["id"].(string)
	fingerprint := diskFingerprint(t, a, "/docs/note.txt")
	if w := putEnrichment(t, a, extractionURL("default", "/docs/note.txt"), map[string]any{
		"fingerprint": fingerprint,
		"text":        "stale agent bravo",
		"provider":    "ocr:test",
	}); w.Code != http.StatusOK {
		t.Fatalf("enrichment status = %d: %s", w.Code, w.Body.String())
	}

	// "alpha" and "gamma" are the same length, so only the hash can tell them
	// apart.
	target := filepath.Join(a.content, "default", "docs", "note.txt")
	if err := os.WriteFile(target, []byte("gamma"), 0644); err != nil {
		t.Fatal(err)
	}
	scoped := a.withProject("default")
	summary, err := scoped.rescanContent()
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if summary.Refreshed != 1 {
		t.Errorf("rescan refreshed = %d, want 1", summary.Refreshed)
	}
	stored, found := contentEntryRow(t, a, "default", "/docs/note.txt")
	if !found || stored.ID != id {
		t.Fatalf("entry after same-size edit = %+v found=%v", stored, found)
	}
	sum := sha256.Sum256([]byte("gamma"))
	if stored.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 not refreshed: %q", stored.SHA256)
	}
	if res := search(t, a, "default", "bravo"); len(res.Data) != 0 {
		t.Errorf("stale agent text survived a same-size edit: %#v", res.Data)
	}
	if res := search(t, a, "default", "gamma"); len(res.Data) != 1 {
		t.Errorf("new content not indexed after a same-size edit: %#v", res.Data)
	}
}

// TestSymlinkedContentRootsAreRejected guards that a symlinked content
// directory or project subtree is never followed by reads or writes (section
// 64.2).
func TestSymlinkedContentRootsAreRejected(t *testing.T) {
	t.Run("project root", func(t *testing.T) {
		a := newTestApp(t)
		putFile(t, a, "default", "/keep.txt", "keep", "text/plain")
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(a.content, "default")
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
		if w := testRequest(t, a, http.MethodGet, filesURL("default", "/"), nil, ""); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET symlinked project root status = %d, want 422: %s", w.Code, w.Body.String())
		}
		if w := testRequest(t, a, http.MethodPut, filesURL("default", "/new.txt"), "new", "text/plain"); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("PUT symlinked project root status = %d, want 422: %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
			t.Errorf("write followed a symlinked project root: %v", err)
		}
	})

	t.Run("content directory", func(t *testing.T) {
		a := newTestApp(t)
		outside := t.TempDir()
		if err := os.RemoveAll(a.content); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, a.content); err != nil {
			t.Fatal(err)
		}
		if w := testRequest(t, a, http.MethodGet, filesURL("default", "/"), nil, ""); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET symlinked content directory status = %d, want 422: %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(outside, "default")); !os.IsNotExist(err) {
			t.Errorf("read followed a symlinked content directory: %v", err)
		}
	})
}

// TestContentMediaType pins the derived content type for the formats Clio
// stores most often: explicit audio/document types do not depend on the base
// image shipping /etc/mime.types, while common image types come from Go's
// built-in table (section 64.5).
func TestContentMediaType(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/docs/note.md", "text/markdown"},
		{"/docs/page.html", "text/html"},
		{"/audio/song.flac", "audio/flac"},
		{"/audio/song.mp3", "audio/mpeg"},
		{"/audio/voice.wav", "audio/wav"},
		{"/docs/report.pdf", "application/pdf"},
		{"/photos/photo.jpg", "image/jpeg"},
		{"/docs/blob.bin", "application/octet-stream"},
	} {
		if got := contentMediaType(tc.path); got != tc.want {
			t.Errorf("contentMediaType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
