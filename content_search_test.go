package main

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// contentSearchRow reads one index row by entry ID.
func contentSearchRow(t *testing.T, a *app, project, id string) (map[string]string, bool) {
	t.Helper()
	var path, kind, source, title, body string
	err := a.db.QueryRow(
		`SELECT path,kind,source,title,body FROM content_search WHERE project=? AND id=?`,
		project, id,
	).Scan(&path, &kind, &source, &title, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read content_search row: %v", err)
	}
	return map[string]string{"path": path, "kind": kind, "source": source, "title": title, "body": body}, true
}

// ftsMatchIDs returns the entry IDs matching a literal FTS5 term in a project.
func ftsMatchIDs(t *testing.T, a *app, project, term string) []string {
	t.Helper()
	rows, err := a.db.Query(`SELECT id FROM content_search WHERE content_search MATCH ? AND project=?`, term, project)
	if err != nil {
		t.Fatalf("match content_search: %v", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatalf("scan match id: %v", err)
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("match rows: %v", err)
	}
	sort.Strings(ids)
	return ids
}

func TestFTS5Available(t *testing.T) {
	a := newTestApp(t)
	var name string
	if err := a.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='content_search'`).Scan(&name); err != nil {
		t.Fatalf("content_search virtual table missing (is -tags sqlite_fts5 set?): %v", err)
	}
	// A real MATCH round trip proves the FTS5 module is compiled in.
	if _, err := a.db.Exec(`INSERT INTO content_search(id,project,path,kind,source,title,body) VALUES('probe','default','/probe.txt','file','native','probe.txt','fts availability probe')`); err != nil {
		t.Fatalf("insert into content_search: %v", err)
	}
	ids := ftsMatchIDs(t, a, "default", "availability")
	if len(ids) != 1 || ids[0] != "probe" {
		t.Fatalf("FTS5 match ids = %v, want [probe]", ids)
	}
}

func TestContentSearchIndexesWritesAndDrops(t *testing.T) {
	a := newTestApp(t)
	created := putFile(t, a, "default", "/docs/a.txt", "the needful appears here", "text/plain")
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("PUT response has no id: %#v", created)
	}
	row, ok := contentSearchRow(t, a, "default", id)
	if !ok {
		t.Fatal("PUT did not index the entry")
	}
	if row["source"] != "native" || row["kind"] != "file" || row["path"] != "/docs/a.txt" || row["title"] != "a.txt" {
		t.Errorf("index row = %#v", row)
	}
	if !strings.Contains(row["body"], "needful") {
		t.Errorf("index body = %q, want to contain %q", row["body"], "needful")
	}
	if ids := ftsMatchIDs(t, a, "default", "needful"); len(ids) != 1 || ids[0] != id {
		t.Errorf("MATCH needful = %v, want [%s]", ids, id)
	}

	// Replace refreshes the indexed text; the old text is gone.
	putFile(t, a, "default", "/docs/a.txt", "replacement words", "text/plain")
	row, ok = contentSearchRow(t, a, "default", id)
	if !ok || !strings.Contains(row["body"], "replacement") || strings.Contains(row["body"], "needful") {
		t.Errorf("index row after replace = %#v", row)
	}
	if ids := ftsMatchIDs(t, a, "default", "needful"); len(ids) != 0 {
		t.Errorf("stale text still matches: %v", ids)
	}

	// Content with no native text (binary, and a PDF with no text layer) leaves
	// no index row at all.
	binary := putFile(t, a, "default", "/bin/blob.bin", "\x00\x01\x02\xff", "application/octet-stream")
	if row, ok := contentSearchRow(t, a, "default", binary["id"].(string)); ok {
		t.Errorf("binary file was indexed: %#v", row)
	}
	scanned := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	if row, ok := contentSearchRow(t, a, "default", scanned["id"].(string)); ok {
		t.Errorf("image-only PDF was indexed: %#v", row)
	}

	// A page created through the files API is indexed too.
	page := testRequest(t, a, http.MethodPut, filesURL("default", "/docs/p.md"), "# Alpha heading", "text/markdown")
	if page.Code != http.StatusCreated {
		t.Fatalf("create page: %d %s", page.Code, page.Body.String())
	}
	pageEntry, found := contentEntryRow(t, a, "default", "/docs/p.md")
	if !found {
		t.Fatal("page entry missing")
	}
	if row, ok = contentSearchRow(t, a, "default", pageEntry.ID); !ok || row["kind"] != "page" || !strings.Contains(row["body"], "Alpha") {
		t.Errorf("page index row = %#v, ok=%v", row, ok)
	}

	// ZIP upload writes index through the transaction path.
	archive := buildZip(t, []zipEntry{{name: "zipped.txt", body: "zipped searchable content"}})
	upload := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fzips", archive, "application/zip")
	if upload.Code != http.StatusCreated {
		t.Fatalf("zip upload: %d %s", upload.Code, upload.Body.String())
	}
	zipEntryRow, found := contentEntryRow(t, a, "default", "/zips/zipped.txt")
	if !found {
		t.Fatal("zip entry missing")
	}
	if row, ok = contentSearchRow(t, a, "default", zipEntryRow.ID); !ok || !strings.Contains(row["body"], "searchable") {
		t.Errorf("zip index row = %#v, ok=%v", row, ok)
	}

	// Delete by path drops the row.
	if w := testRequest(t, a, http.MethodDelete, filesURL("default", "/docs/a.txt"), nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete file: %d %s", w.Code, w.Body.String())
	}
	if _, ok := contentSearchRow(t, a, "default", id); ok {
		t.Error("deleted file kept its index row")
	}

	// Delete by ID drops the row.
	if w := testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+pageEntry.ID, nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete by id: %d %s", w.Code, w.Body.String())
	}
	if _, ok := contentSearchRow(t, a, "default", pageEntry.ID); ok {
		t.Error("file deleted by id kept its index row")
	}

	// Deleting a directory subtree drops every descendant row.
	putFile(t, a, "default", "/dir/one.txt", "first descendant", "text/plain")
	putFile(t, a, "default", "/dir/two.txt", "second descendant", "text/plain")
	if w := testRequest(t, a, http.MethodDelete, filesURL("default", "/dir"), nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete directory: %d %s", w.Code, w.Body.String())
	}
	if ids := ftsMatchIDs(t, a, "default", "descendant"); len(ids) != 0 {
		t.Errorf("deleted subtree rows remain: %v", ids)
	}
}

func TestContentSearchRebuiltByRescan(t *testing.T) {
	a := newTestApp(t)
	root := filepath.Join(a.content, "default")
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "keep.txt"), []byte("rebuilt body"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<h1>Header</h1> Body text"), 0644); err != nil {
		t.Fatal(err)
	}
	scoped := a.withProject("default")
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	keep, found := contentEntryRow(t, a, "default", "/notes/keep.txt")
	if !found {
		t.Fatal("text entry missing after reconcile")
	}
	if row, ok := contentSearchRow(t, a, "default", keep.ID); !ok || !strings.Contains(row["body"], "rebuilt") {
		t.Errorf("reconciled text row = %#v, ok=%v", row, ok)
	}
	html, found := contentEntryRow(t, a, "default", "/page.html")
	if !found {
		t.Fatal("html entry missing after reconcile")
	}
	if row, ok := contentSearchRow(t, a, "default", html.ID); !ok || !strings.Contains(row["body"], "Header Body") || strings.Contains(row["body"], "<") {
		t.Errorf("reconciled html row = %#v, ok=%v", row, ok)
	}

	// The index is derived: dropping it and rescanning must rebuild it.
	if _, err := a.db.Exec(`DELETE FROM content_search`); err != nil {
		t.Fatal(err)
	}
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if row, ok := contentSearchRow(t, a, "default", keep.ID); !ok || !strings.Contains(row["body"], "rebuilt") {
		t.Errorf("rescan did not rebuild the index: %#v, ok=%v", row, ok)
	}

	// A removed file loses its row.
	if err := os.Remove(filepath.Join(root, "notes", "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if err := scoped.reconcileContent(); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if _, ok := contentSearchRow(t, a, "default", keep.ID); ok {
		t.Error("rescan kept the index row for a removed file")
	}
	if ids := ftsMatchIDs(t, a, "default", "rebuilt"); len(ids) != 0 {
		t.Errorf("removed file still matches: %v", ids)
	}
}

func TestContentSearchMoveKeepsIDAndPath(t *testing.T) {
	a := newTestApp(t)
	created := putFile(t, a, "default", "/docs/a.txt", "movable content", "text/plain")
	id, _ := created["id"].(string)
	moved := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/docs/a.txt", "to": "/renamed/b.txt"}, "application/json")
	if moved.Code != http.StatusOK {
		t.Fatalf("move file: %d %s", moved.Code, moved.Body.String())
	}
	row, ok := contentSearchRow(t, a, "default", id)
	if !ok || row["path"] != "/renamed/b.txt" || !strings.Contains(row["body"], "movable") {
		t.Errorf("index row after move = %#v, ok=%v", row, ok)
	}

	// Directory move re-homes every descendant row.
	first := putFile(t, a, "default", "/tree/one.txt", "first tree", "text/plain")
	second := putFile(t, a, "default", "/tree/sub/two.txt", "second tree", "text/plain")
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/tree", "to": "/forest"}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("move directory: %d %s", w.Code, w.Body.String())
	}
	firstID, _ := first["id"].(string)
	secondID, _ := second["id"].(string)
	if row, ok := contentSearchRow(t, a, "default", firstID); !ok || row["path"] != "/forest/one.txt" {
		t.Errorf("descendant row = %#v, ok=%v", row, ok)
	}
	if row, ok := contentSearchRow(t, a, "default", secondID); !ok || row["path"] != "/forest/sub/two.txt" {
		t.Errorf("nested descendant row = %#v, ok=%v", row, ok)
	}
}

// TestContentSearchMoveRefreshesMetadata guards that a rename refreshes the
// index row's kind and title from the new path and re-extracts native text when
// the extension changes, while a same-extension rename keeps agent enrichment
// (sections 64.6 and 64.8).
func TestContentSearchMoveRefreshesMetadata(t *testing.T) {
	a := newTestApp(t)

	// A file renamed to a page must report the page kind and the new title, and
	// the old title must no longer match.
	created := putFile(t, a, "default", "/docs/notes.txt", "alpha body words", "text/plain")
	id, _ := created["id"].(string)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/docs/notes.txt", "to": "/docs/notes.md"}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("move txt->md: %d %s", w.Code, w.Body.String())
	}
	row, ok := contentSearchRow(t, a, "default", id)
	if !ok || row["kind"] != "page" || row["title"] != "notes.md" || row["path"] != "/docs/notes.md" {
		t.Errorf("index row after txt->md move = %#v, ok=%v", row, ok)
	}
	if !strings.Contains(row["body"], "alpha") {
		t.Errorf("native text not re-extracted after move: %q", row["body"])
	}
	if res := search(t, a, "default", "notes.txt"); len(res.Data) != 0 {
		t.Errorf("old title still matches after move: %#v", res.Data)
	}
	if res := search(t, a, "default", "alpha"); len(res.Data) != 1 || res.Data[0]["kind"] != "page" || res.Data[0]["path"] != "/docs/notes.md" {
		t.Errorf("search after move = %#v", res.Data)
	}

	// A same-extension rename keeps agent enrichment and retitles it.
	entry := putFile(t, a, "default", "/docs/a.txt", "plain words", "text/plain")
	entryID, _ := entry["id"].(string)
	fingerprint := diskFingerprint(t, a, "/docs/a.txt")
	if w := putEnrichment(t, a, extractionURL("default", "/docs/a.txt"), map[string]any{
		"fingerprint": fingerprint,
		"text":        "agent quokka text",
		"provider":    "ocr:test",
	}); w.Code != http.StatusOK {
		t.Fatalf("enrichment: %d %s", w.Code, w.Body.String())
	}
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/docs/a.txt", "to": "/docs/b.txt"}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("move txt->txt: %d %s", w.Code, w.Body.String())
	}
	row, ok = contentSearchRow(t, a, "default", entryID)
	if !ok || row["source"] != "agent:ocr:test" || row["title"] != "b.txt" || row["path"] != "/docs/b.txt" {
		t.Errorf("agent row after same-extension move = %#v, ok=%v", row, ok)
	}
	if res := search(t, a, "default", "quokka"); len(res.Data) != 1 || res.Data[0]["source"] != "agent:ocr:test" {
		t.Errorf("agent enrichment lost on same-extension move: %#v", res.Data)
	}
}

func TestContentSearchProjectScoped(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	defaultEntry := putFile(t, a, "default", "/shared.txt", "alpha default only", "text/plain")
	billsEntry := putFile(t, a, "bills", "/shared.txt", "alpha bills only", "text/plain")
	defaultID, _ := defaultEntry["id"].(string)
	billsID, _ := billsEntry["id"].(string)

	if ids := ftsMatchIDs(t, a, "default", "alpha"); len(ids) != 1 || ids[0] != defaultID {
		t.Errorf("default MATCH alpha = %v, want [%s]", ids, defaultID)
	}
	if ids := ftsMatchIDs(t, a, "bills", "alpha"); len(ids) != 1 || ids[0] != billsID {
		t.Errorf("bills MATCH alpha = %v, want [%s]", ids, billsID)
	}
	if ids := ftsMatchIDs(t, a, "default", "bills"); len(ids) != 0 {
		t.Errorf("bills text leaked into default: %v", ids)
	}
}
