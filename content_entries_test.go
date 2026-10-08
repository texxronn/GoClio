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
	create := testRequest(t, a, http.MethodPost, "/api/v1/default/files/pages", map[string]any{
		"path": "/docs/a.md", "content_type": "text/markdown", "content": "version one",
	}, "application/json")
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

	replace := testRequest(t, a, http.MethodPost, "/api/v1/default/files/pages", map[string]any{
		"path": "/docs/a.md", "content_type": "text/markdown", "content": "version two, longer",
	}, "application/json")
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

	// The page reads back the preserved timestamps.
	read := testRequest(t, a, http.MethodGet, "/api/v1/default/files/pages?path=%2Fdocs%2Fa.md", nil, "")
	var page map[string]any
	testJSON(t, read, &page)
	if page["created_at"] != first.CreatedAt || page["updated_at"] != second.UpdatedAt {
		t.Errorf("page timestamps = %v/%v, want %v/%v", page["created_at"], page["updated_at"], first.CreatedAt, second.UpdatedAt)
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
		w := testRequest(t, a, http.MethodPost, "/api/v1/"+project+"/files/pages", map[string]any{
			"path": "/shared.md", "content_type": "text/markdown", "content": content,
		}, "application/json")
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
	read := testRequest(t, a, http.MethodGet, "/api/v1/bills/files/pages?path=%2Fshared.md", nil, "")
	var page map[string]any
	testJSON(t, read, &page)
	if page["content"] != "# Bills" {
		t.Errorf("bills page content = %v", page["content"])
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
