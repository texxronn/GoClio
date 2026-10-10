package main

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// filesURL builds the files collection URL with an optional ?path= value.
func filesURL(project, path string) string {
	if path == "" {
		return "/api/v1/" + project + "/files"
	}
	return "/api/v1/" + project + "/files?path=" + url.QueryEscape(path)
}

func putFile(t *testing.T, a *app, project, path, body, contentType string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodPut, filesURL(project, path), body, contentType)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("PUT %s status = %d, want 201 or 200: %s", path, w.Code, w.Body.String())
	}
	var entry map[string]any
	testJSON(t, w, &entry)
	return entry
}

func TestFilesCRUDByPathAndID(t *testing.T) {
	a := newTestApp(t)
	created := putFile(t, a, "default", "/docs/a.txt", "hello", "text/plain")
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("PUT response has no id: %#v", created)
	}
	if created["path"] != "/docs/a.txt" || created["kind"] != "file" || created["content_type"] != "text/plain" {
		t.Errorf("entry = %#v", created)
	}
	if created["size"] != float64(5) {
		t.Errorf("size = %v, want 5", created["size"])
	}
	if created["sha256"] == "" {
		t.Error("sha256 was not populated on write")
	}
	if want := "http://clio.test/default/files/id/" + id; created["stable_url"] != want {
		t.Errorf("stable_url = %v, want %v", created["stable_url"], want)
	}

	// Read back by ID and by path.
	byID := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id, nil, "")
	if byID.Code != http.StatusOK {
		t.Fatalf("GET by id status = %d: %s", byID.Code, byID.Body.String())
	}
	var fetched map[string]any
	testJSON(t, byID, &fetched)
	if fetched["id"] != id || fetched["path"] != "/docs/a.txt" {
		t.Errorf("GET by id = %#v", fetched)
	}
	byPath := testRequest(t, a, http.MethodGet, filesURL("default", "/docs/a.txt"), nil, "")
	if byPath.Code != http.StatusOK {
		t.Fatalf("GET by path status = %d: %s", byPath.Code, byPath.Body.String())
	}
	var node map[string]any
	testJSON(t, byPath, &node)
	if node["kind"] != "file" || node["id"] != id {
		t.Errorf("GET by path = %#v", node)
	}

	// Replace preserves the ID and refreshes bytes.
	replaced := testRequest(t, a, http.MethodPut, filesURL("default", "/docs/a.txt"), "longer content", "text/plain")
	if replaced.Code != http.StatusOK {
		t.Fatalf("replace status = %d, want 200: %s", replaced.Code, replaced.Body.String())
	}
	var second map[string]any
	testJSON(t, replaced, &second)
	if second["id"] != id {
		t.Errorf("replace changed id: %v -> %v", id, second["id"])
	}
	if second["size"] != float64(len("longer content")) {
		t.Errorf("replace size = %v", second["size"])
	}
	if got := diskFile(t, a, "default", "/docs/a.txt"); got != "longer content" {
		t.Errorf("stored bytes = %q", got)
	}

	// Delete by ID, then the entry is gone.
	if w := testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+id, nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE by id status = %d: %s", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id, nil, ""), http.StatusNotFound)
	if _, err := os.Stat(filepath.Join(a.content, "default", "docs", "a.txt")); !os.IsNotExist(err) {
		t.Errorf("file still on disk after delete: %v", err)
	}
}

func diskFile(t *testing.T, a *app, project, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.content, project, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read disk file %s: %v", path, err)
	}
	return string(data)
}

func TestFilesDirectoryListingAndPageKind(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/reports"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d: %s", w.Code, w.Body.String())
	}
	page := putFile(t, a, "default", "/reports/week.md", "# Week", "")
	if page["kind"] != "page" || page["content_type"] != "text/markdown" {
		t.Errorf("page entry = %#v", page)
	}
	file := putFile(t, a, "default", "/reports/data.csv", "a,b\n1,2\n", "text/csv")
	if file["kind"] != "file" || file["content_type"] != "text/csv" {
		t.Errorf("file entry = %#v", file)
	}

	listing := testRequest(t, a, http.MethodGet, filesURL("default", "/reports"), nil, "")
	if listing.Code != http.StatusOK {
		t.Fatalf("listing status = %d: %s", listing.Code, listing.Body.String())
	}
	var directory map[string]any
	testJSON(t, listing, &directory)
	if directory["kind"] != "directory" || directory["path"] != "/reports" {
		t.Errorf("directory representation = %#v", directory)
	}
	children := directory["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children = %#v, want 2", children)
	}
	for _, child := range children {
		entry := child.(map[string]any)
		if entry["kind"] != "page" && entry["kind"] != "file" {
			t.Errorf("child kind = %v", entry["kind"])
		}
		if entry["id"] == nil && entry["kind"] != "directory" {
			t.Errorf("child missing id: %#v", entry)
		}
	}
	pageInfo := directory["page"].(map[string]any)
	if pageInfo["total"] != float64(2) || pageInfo["count"] != float64(2) {
		t.Errorf("page info = %#v", pageInfo)
	}

	// A missing node returns 404.
	assertAPIError(t, testRequest(t, a, http.MethodGet, filesURL("default", "/reports/missing.md"), nil, ""), http.StatusNotFound)
}

// TestFilesDirectoryListingPaging verifies that directory listing returns the
// requested page window with correct names and totals (sections 23 and 64.4).
func TestFilesDirectoryListingPaging(t *testing.T) {
	a := newTestApp(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		putFile(t, a, "default", "/page/"+name+".txt", name, "text/plain")
	}
	type listing struct {
		Children []map[string]any `json:"children"`
		Page     map[string]any   `json:"page"`
	}
	read := func(limit, offset int) listing {
		t.Helper()
		w := testRequest(t, a, http.MethodGet, filesURL("default", "/page")+"&limit="+strconv.Itoa(limit)+"&offset="+strconv.Itoa(offset), nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("listing limit=%d offset=%d status = %d: %s", limit, offset, w.Code, w.Body.String())
		}
		var got listing
		testJSON(t, w, &got)
		return got
	}

	first := read(2, 0)
	if len(first.Children) != 2 || first.Page["total"] != float64(5) || first.Page["count"] != float64(2) {
		t.Fatalf("first page children=%#v page=%#v", first.Children, first.Page)
	}
	if first.Children[0]["path"] != "/page/a.txt" || first.Children[1]["path"] != "/page/b.txt" {
		t.Errorf("first page paths = %v, %v", first.Children[0]["path"], first.Children[1]["path"])
	}

	last := read(2, 4)
	if len(last.Children) != 1 || last.Children[0]["path"] != "/page/e.txt" {
		t.Errorf("last page children = %#v", last.Children)
	}

	empty := read(2, 10)
	if len(empty.Children) != 0 || empty.Page["count"] != float64(0) || empty.Page["total"] != float64(5) {
		t.Errorf("empty page children=%#v page=%#v", empty.Children, empty.Page)
	}
}

func TestFilesListFiltersAndPaging(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/one/a.md", "a", "")
	putFile(t, a, "default", "/one/b.txt", "b", "text/plain")
	putFile(t, a, "default", "/two/c.md", "c", "")

	all := testRequest(t, a, http.MethodGet, "/api/v1/default/files", nil, "")
	var result map[string]any
	testJSON(t, all, &result)
	if result["page"].(map[string]any)["total"] != float64(3) {
		t.Errorf("catalog total = %v", result["page"])
	}

	pages := testRequest(t, a, http.MethodGet, "/api/v1/default/files?kind=page", nil, "")
	testJSON(t, pages, &result)
	if result["page"].(map[string]any)["total"] != float64(2) {
		t.Errorf("kind=page total = %v", result["page"])
	}
	for _, item := range result["data"].([]any) {
		if item.(map[string]any)["kind"] != "page" {
			t.Errorf("kind filter leaked %#v", item)
		}
	}

	byType := testRequest(t, a, http.MethodGet, "/api/v1/default/files?content_type=text%2Fplain", nil, "")
	testJSON(t, byType, &result)
	if result["page"].(map[string]any)["total"] != float64(1) {
		t.Errorf("content_type filter total = %v", result["page"])
	}

	byPrefix := testRequest(t, a, http.MethodGet, "/api/v1/default/files?prefix=%2Fone", nil, "")
	testJSON(t, byPrefix, &result)
	if result["page"].(map[string]any)["total"] != float64(2) {
		t.Errorf("prefix filter total = %v", result["page"])
	}

	paged := testRequest(t, a, http.MethodGet, "/api/v1/default/files?limit=1&offset=1", nil, "")
	testJSON(t, paged, &result)
	info := result["page"].(map[string]any)
	if info["limit"] != float64(1) || info["offset"] != float64(1) || info["total"] != float64(3) || len(result["data"].([]any)) != 1 {
		t.Errorf("paging = %#v", info)
	}

	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files?kind=bogus", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files?limit=0", nil, ""), http.StatusUnprocessableEntity)
}

func TestFilesMoveAndCopy(t *testing.T) {
	a := newTestApp(t)
	file := putFile(t, a, "default", "/a/file.txt", "move me", "text/plain")
	fileID := file["id"].(string)
	page := putFile(t, a, "default", "/tree/keep.md", "# Keep", "")
	pageID := page["id"].(string)
	nested := putFile(t, a, "default", "/tree/sub/deep.md", "# Deep", "")
	nestedID := nested["id"].(string)

	// Move a file: the ID is preserved and the old path is gone.
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/a/file.txt", "to": "/b/file.txt"}, "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("move status = %d: %s", w.Code, w.Body.String())
	}
	movedID := entryID(t, a, "default", fileID)
	if movedID != fileID {
		t.Errorf("move changed id: %v -> %v", fileID, movedID)
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, filesURL("default", "/a/file.txt"), nil, ""), http.StatusNotFound)
	if got := diskFile(t, a, "default", "/b/file.txt"); got != "move me" {
		t.Errorf("moved bytes = %q", got)
	}

	// Copy a file: a new ID is assigned and the source remains.
	w = testRequest(t, a, http.MethodPost, "/api/v1/default/files/copy", map[string]any{"from": "/b/file.txt", "to": "/b/copy.txt"}, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("copy status = %d: %s", w.Code, w.Body.String())
	}
	var copied map[string]any
	testJSON(t, w, &copied)
	if copied["id"] == fileID {
		t.Error("copy reused the source id")
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "b", "copy.txt")); err != nil {
		t.Errorf("copy missing on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "b", "file.txt")); err != nil {
		t.Errorf("copy removed the source: %v", err)
	}

	// Move a directory: every descendant keeps its ID.
	w = testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/tree", "to": "/moved"}, "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("move directory status = %d: %s", w.Code, w.Body.String())
	}
	if entryPath(t, a, "default", pageID) != "/moved/keep.md" || entryPath(t, a, "default", nestedID) != "/moved/sub/deep.md" {
		t.Errorf("descendant paths not updated: %q %q", entryPath(t, a, "default", pageID), entryPath(t, a, "default", nestedID))
	}

	// Copy a directory: new IDs for every descendant.
	w = testRequest(t, a, http.MethodPost, "/api/v1/default/files/copy", map[string]any{"from": "/moved", "to": "/clone"}, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("copy directory status = %d: %s", w.Code, w.Body.String())
	}
	cloned, found := contentEntryForPath(t, a, "default", "/clone/sub/deep.md")
	if !found || cloned.ID == nestedID {
		t.Errorf("directory copy did not assign a new id: %+v", cloned)
	}
	if entryPath(t, a, "default", pageID) != "/moved/keep.md" {
		t.Error("copying the directory disturbed the source")
	}

	// Error cases.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/missing", "to": "/x"}, "application/json"), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/moved", "to": "/clone"}, "application/json"), http.StatusConflict)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/moved", "to": "/moved/inside"}, "application/json"), http.StatusConflict)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/", "to": "/x"}, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"to": "/x"}, "application/json"), http.StatusUnprocessableEntity)
}

func entryID(t *testing.T, a *app, project, id string) string {
	t.Helper()
	entry, found, err := a.withProject(project).contentEntryByID(id)
	if err != nil || !found {
		t.Fatalf("entry %s not found: err=%v found=%v", id, err, found)
	}
	return entry.ID
}

func entryPath(t *testing.T, a *app, project, id string) string {
	t.Helper()
	entry, found, err := a.withProject(project).contentEntryByID(id)
	if err != nil || !found {
		t.Fatalf("entry %s not found: err=%v found=%v", id, err, found)
	}
	return entry.Path
}

func contentEntryForPath(t *testing.T, a *app, project, path string) (contentEntry, bool) {
	t.Helper()
	entry, found, err := a.withProject(project).contentEntryByPath(path)
	if err != nil {
		t.Fatalf("read entry %s: %v", path, err)
	}
	return entry, found
}

func TestFilesConflictsAndReservedSegment(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/dir"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create directory: %d %s", w.Code, w.Body.String())
	}
	// A directory cannot be replaced by a file, and a file cannot take a
	// directory's path.
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/dir"), "x", "text/plain"), http.StatusConflict)
	// A directory cannot be created over an existing file.
	putFile(t, a, "default", "/blocker", "x", "text/plain")
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/blocker"}, "application/json"), http.StatusConflict)
	// A file in the parent chain blocks a nested write.
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/blocker/child.txt"), "x", "text/plain"), http.StatusConflict)
	// The root cannot be deleted or created; creating it again conflicts.
	assertAPIError(t, testRequest(t, a, http.MethodDelete, filesURL("default", "/"), nil, ""), http.StatusConflict)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/"}, "application/json"), http.StatusConflict)
	// `id` is reserved directly under files.
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/id"), "x", "text/plain"), http.StatusUnprocessableEntity)
}

func TestFilesDeleteByPathRemovesSubtree(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/dir/a.md", "a", "")
	putFile(t, a, "default", "/dir/sub/b.txt", "b", "text/plain")
	if w := testRequest(t, a, http.MethodDelete, filesURL("default", "/dir"), nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete directory status = %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "dir")); !os.IsNotExist(err) {
		t.Errorf("directory still on disk: %v", err)
	}
	if _, found := contentEntryForPath(t, a, "default", "/dir/a.md"); found {
		t.Error("descendant entry survived directory delete")
	}
	assertAPIError(t, testRequest(t, a, http.MethodDelete, filesURL("default", "/dir"), nil, ""), http.StatusNotFound)
}

func TestFilesUploadLimitAndTraversal(t *testing.T) {
	a := newTestApp(t)
	oversized := bytes.Repeat([]byte("a"), int(defaultUploadLimit)+1)
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/big.bin"), oversized, "application/octet-stream"), http.StatusRequestEntityTooLarge)

	for _, bad := range []string{"/../escape.txt", "relative.txt", "/a//b.txt", "/a/./b.txt", "/id"} {
		t.Run(bad, func(t *testing.T) {
			assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", bad), "x", "text/plain"), http.StatusUnprocessableEntity)
		})
	}
	// A move with a traversing destination is rejected too.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/a.txt", "to": "/../b.txt"}, "application/json"), http.StatusUnprocessableEntity)
}

func TestFilesRescanSummary(t *testing.T) {
	a := newTestApp(t)
	root := filepath.Join(a.content, "default")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}

	var summary map[string]any
	testJSON(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/rescan", nil, ""), &summary)
	if summary["added"] != float64(1) || summary["removed"] != float64(0) {
		t.Fatalf("first rescan summary = %#v", summary)
	}
	if _, found := contentEntryForPath(t, a, "default", "/loose.txt"); !found {
		t.Fatal("rescan did not add the disk file")
	}

	// A size change is reported as refreshed.
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("a longer body"), 0644); err != nil {
		t.Fatal(err)
	}
	testJSON(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/rescan", nil, ""), &summary)
	if summary["refreshed"] != float64(1) {
		t.Fatalf("refresh summary = %#v", summary)
	}

	// A vanished file is removed.
	if err := os.Remove(filepath.Join(root, "loose.txt")); err != nil {
		t.Fatal(err)
	}
	testJSON(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/rescan", nil, ""), &summary)
	if summary["removed"] != float64(1) {
		t.Fatalf("removal summary = %#v", summary)
	}
}

func TestFilesProjectIsolation(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	def := putFile(t, a, "default", "/shared.txt", "default", "text/plain")
	bills := putFile(t, a, "bills", "/shared.txt", "bills", "text/plain")

	var result map[string]any
	testJSON(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files", nil, ""), &result)
	if result["page"].(map[string]any)["total"] != float64(1) {
		t.Errorf("default catalog leaked: %#v", result)
	}
	// A file ID is only resolvable in its own project.
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+bills["id"].(string), nil, ""), http.StatusNotFound)
	if entryID(t, a, "default", def["id"].(string)) != def["id"].(string) {
		t.Error("default entry not resolvable")
	}
}
