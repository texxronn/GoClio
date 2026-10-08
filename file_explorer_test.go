package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestFileExplorerShellRoutes covers the /{project}/files explorer shell and
// the retained server-rendered directory pages (section 64.14).
func TestFileExplorerShellRoutes(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create directory: %v", ae)
	}

	shell := testRequest(t, a, http.MethodGet, "/default/files", nil, "")
	if shell.Code != http.StatusOK {
		t.Fatalf("GET /default/files status=%d: %s", shell.Code, shell.Body.String())
	}
	for _, want := range []string{"File explorer", "assets/clio.js", "Clio.FileBrowser.mount", "clio-file-browser", "<noscript>", "notes", ".fb-tree{", ".fb-tree-pane{", ".fb-staging{", ".fb-staging-row{"} {
		if !strings.Contains(shell.Body.String(), want) {
			t.Errorf("GET /default/files missing %q", want)
		}
	}

	// Content paths under /files still serve directory pages, rendered pages
	// and downloads.
	directory := testRequest(t, a, http.MethodGet, "/default/files/notes", nil, "")
	if directory.Code != http.StatusOK || !strings.Contains(directory.Body.String(), `class="content-directory"`) {
		t.Errorf("GET /default/files/notes status=%d missing directory page: %s", directory.Code, directory.Body.String())
	}

	// The explorer shell only serves GET/HEAD.
	if r := testRequest(t, a, http.MethodPut, "/default/files", nil, ""); r.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /default/files status=%d, want 405", r.Code)
	}
}

// TestSearchUIWorkflow covers the human search page at /{project}/search:
// routing, the safe snippet rendering and the validation status codes
// (sections 64.7 and 64.14).
func TestSearchUIWorkflow(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/notes/answer.txt", "the needle is <script>alert(1)</script> here", "text/plain")
	putFile(t, a, "default", "/notes/other.md", "# Nothing to see", "text/markdown")

	// The empty form is served.
	form := testRequest(t, a, http.MethodGet, "/default/search", nil, "")
	if form.Code != http.StatusOK || !strings.Contains(form.Body.String(), `name="q"`) {
		t.Fatalf("GET /default/search status=%d missing form: %s", form.Code, form.Body.String())
	}

	results := testRequest(t, a, http.MethodGet, "/default/search?q=needle", nil, "")
	if results.Code != http.StatusOK {
		t.Fatalf("GET /default/search?q=needle status=%d: %s", results.Code, results.Body.String())
	}
	body := results.Body.String()
	for _, want := range []string{`/default/files/notes/answer.txt`, "<mark>needle</mark>", "1 result"} {
		if !strings.Contains(body, want) {
			t.Errorf("search results missing %q: %s", want, body)
		}
	}
	// The indexed content carries a literal <script>; it must be escaped before
	// the sentinels become markup, so it can never execute.
	if strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("search snippet did not escape indexed content: %s", body)
	}

	// No matches and empty queries are harmless.
	none := testRequest(t, a, http.MethodGet, "/default/search?q=absentterm", nil, "")
	if none.Code != http.StatusOK || !strings.Contains(none.Body.String(), "No pages or files matched") {
		t.Errorf("empty result page status=%d: %s", none.Code, none.Body.String())
	}

	// Oversized and bad paging inputs are rejected like the search API.
	oversized := "/default/search?q=" + strings.Repeat("a", maxSearchQueryBytes+1)
	if r := testRequest(t, a, http.MethodGet, oversized, nil, ""); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("oversized query status=%d, want 422", r.Code)
	}
	if r := testRequest(t, a, http.MethodGet, "/default/search?q=needle&limit=0", nil, ""); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad limit status=%d, want 422", r.Code)
	}

	// Routing and methods.
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/search/extra", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/default/search", nil, ""), http.StatusMethodNotAllowed)
}

// TestFileExplorerLightActions exercises the files API operations the explorer
// performs (create folder, upload, rename/move, delete) and the 409 conflict
// the explorer surfaces when a referenced entry is deleted.
func TestFileExplorerLightActions(t *testing.T) {
	a := newTestApp(t)

	// New folder.
	if r := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/docs"}, "application/json"); r.Code != http.StatusCreated {
		t.Fatalf("create folder status=%d: %s", r.Code, r.Body.String())
	}
	// Upload.
	uploaded := putFile(t, a, "default", "/docs/a.txt", "hello", "text/plain")
	id, _ := uploaded["id"].(string)
	if id == "" {
		t.Fatalf("upload has no id: %#v", uploaded)
	}
	// Rename/move preserves the ID.
	moved := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/docs/a.txt", "to": "/docs/b.txt"}, "application/json")
	if moved.Code != http.StatusOK {
		t.Fatalf("move status=%d: %s", moved.Code, moved.Body.String())
	}
	var entry map[string]any
	testJSON(t, moved, &entry)
	if entry["id"] != id || entry["path"] != "/docs/b.txt" {
		t.Errorf("move = %#v, want id %s at /docs/b.txt", entry, id)
	}
	// Delete.
	if r := testRequest(t, a, http.MethodDelete, filesURL("default", "/docs/b.txt"), nil, ""); r.Code != http.StatusNoContent {
		t.Errorf("delete status=%d, want 204", r.Code)
	}

	// A referenced entry cannot be deleted: the explorer surfaces the 409.
	referenced := putFile(t, a, "default", "/docs/keep.txt", "keep me", "text/plain")
	referencedID, _ := referenced["id"].(string)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name": "invoices",
		"fields": []any{
			map[string]any{"name": "title", "type": "string"},
			map[string]any{"name": "invoice", "type": "attachment"},
		},
	})
	createTestRecord(t, a, "billing", "invoices", map[string]any{"title": "One", "invoice": referencedID})

	conflict := testRequest(t, a, http.MethodDelete, filesURL("default", "/docs/keep.txt"), nil, "")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("referenced delete status=%d, want 409: %s", conflict.Code, conflict.Body.String())
	}
	var problem map[string]any
	testJSON(t, conflict, &problem)
	if problem["error"] != "conflict" || problem["message"] == "" {
		t.Errorf("conflict body = %#v, want an error code and message", problem)
	}
}
