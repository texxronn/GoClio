package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipEntry struct {
	name string
	body string
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("create ZIP entry %q: %v", entry.name, err)
		}
		if _, err := file.Write([]byte(entry.body)); err != nil {
			t.Fatalf("write ZIP entry %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	return buffer.Bytes()
}

func TestContentPathValidation(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{
		"/a//b.md",
		"/a/./b.md",
		"/../x.md",
		"/a\\b.md",
		"/a\u0007b.md",
		"/r/c.md/",
		"/id/x.md",
		"/dav/x.md",
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", path), "x", "text/markdown"), http.StatusUnprocessableEntity)
	}
	for _, body := range []string{
		`{"path":"relative"}`,
		`{"path":"/a//b"}`,
		`{"path":"/a/"}`,
		`{"path":"/id/private"}`,
		`{"path":"/dav/private"}`,
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestContentPathsUseFormerRootNames(t *testing.T) {
	// Former site-root route names are ordinary content names now that content
	// lives under /{project}/files/ (section 66.4); a project may also use the
	// names `t` and `collections`, which are not reserved project names.
	a := newTestApp(t)
	for _, path := range []string{"/api/note.md", "/health/x.md", "/help/x.md", "/assets/x.md", "/t/x.md", "/collections/x.md", "/favicon.svg/x.md"} {
		if w := testRequest(t, a, http.MethodPut, filesURL("default", path), "x", "text/markdown"); w.Code != http.StatusCreated {
			t.Errorf("PUT %s status = %d, want 201: %s", path, w.Code, w.Body.String())
		}
	}
	for _, name := range []string{"t", "collections"} {
		if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": name}, "application/json"); w.Code != http.StatusCreated {
			t.Fatalf("create project %q status = %d: %s", name, w.Code, w.Body.String())
		}
		// Human routing must serve the project overview rather than 404.
		if w := testRequest(t, a, http.MethodGet, "/"+name+"/", nil, ""); w.Code != http.StatusOK {
			t.Errorf("GET /%s/ status = %d, want 200: %s", name, w.Code, w.Body.String())
		}
		// Its files API works and a content path using the same name works.
		if w := testRequest(t, a, http.MethodPut, filesURL(name, "/"+name+"/a.md"), "# A", "text/markdown"); w.Code != http.StatusCreated {
			t.Errorf("PUT %s file status = %d, want 201: %s", name, w.Code, w.Body.String())
		}
	}
}

func TestContentResourceIdentity(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/shared.md"}`, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create directory named like a page: %d %s", w.Code, w.Body.String())
	}
	// A page cannot take the canonical path of an existing directory.
	assertAPIError(t, testRequest(t, a, http.MethodPut, filesURL("default", "/shared.md"), "x", "text/markdown"), http.StatusConflict)
	// A directory cannot take the canonical path of an existing page.
	if w := testRequest(t, a, http.MethodPut, filesURL("default", "/doc.md"), "x", "text/markdown"); w.Code != http.StatusCreated {
		t.Fatalf("create page: %d %s", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/doc.md"}`, "application/json"), http.StatusConflict)
}

func TestZipResponseBodyAndNoTopLevelStripping(t *testing.T) {
	a := newTestApp(t)
	archive := buildZip(t, []zipEntry{
		{"pool/weekly.md", "# Weekly"},
		{"pool/measurements/latest.md", "# Latest"},
	})
	upload := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Freports", archive, "application/zip")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", upload.Code, upload.Body.String())
	}
	var response map[string]any
	testJSON(t, upload, &response)
	if response["path"] != "/reports" {
		t.Errorf("upload response path = %v, want /reports", response["path"])
	}
	urls, ok := response["urls"].([]any)
	if !ok || len(urls) != 2 {
		t.Fatalf("upload response urls = %#v, want two URLs", response["urls"])
	}
	got := map[string]bool{}
	for _, u := range urls {
		got[u.(string)] = true
	}
	if !got["http://clio.test/default/files/reports/pool/weekly.md"] || !got["http://clio.test/default/files/reports/pool/measurements/latest.md"] {
		t.Fatalf("upload URLs not relative to destination: %#v", urls)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "reports", "pool", "weekly.md")); err != nil {
		t.Fatalf("archive top-level directory was stripped: %v", err)
	}
}

func TestZipEdgeRejections(t *testing.T) {
	cases := map[string][]zipEntry{
		"duplicate entry":   {{"dup.md", "1"}, {"dup.md", "2"}},
		"file blocks dir":   {{"a", "file"}, {"a/b.md", "child"}},
		"absolute path":     {{"/absolute.md", "x"}},
		"backslash path":    {{"a\\b.md", "x"}},
		"drive letter path": {{"C:/x.md", "x"}},
		"excessive depth":   {{strings.Repeat("d/", 34) + "deep.md", "x"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t)
			response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2F", buildZip(t, entries), "application/zip")
			assertAPIError(t, response, http.StatusUnprocessableEntity)
			contents, err := os.ReadDir(a.content)
			if err != nil {
				t.Fatalf("read content dir: %v", err)
			}
			if len(contents) != 0 {
				t.Fatalf("rejected archive published content: %#v", contents)
			}
		})
	}
}

func TestMarkdownSubsetRendering(t *testing.T) {
	a := newTestApp(t)
	source := strings.Join([]string{
		"# Title",
		"",
		"- one",
		"- two",
		"",
		"```",
		"<script>alert(1)</script>",
		"```",
		"",
		"`inline` **bold** *italic* [link](https://example.com/x)",
		"[bad](javascript:alert(1))",
	}, "\n")
	create := testRequest(t, a, http.MethodPut, filesURL("default", "/md/subset.md"), source, "text/markdown")
	if create.Code != http.StatusCreated {
		t.Fatalf("create Markdown page: %d %s", create.Code, create.Body.String())
	}
	view := testRequest(t, a, http.MethodGet, "/default/files/md/subset.md", nil, "")
	if view.Code != http.StatusOK {
		t.Fatalf("view Markdown page: %d", view.Code)
	}
	body := view.Body.String()
	for _, want := range []string{
		"<h1>Title</h1>",
		"<ul>",
		"<li>one</li>",
		"<pre><code>&lt;script&gt;",
		"<code>inline</code>",
		"<strong>bold</strong>",
		"<em>italic</em>",
		`<a href="https://example.com/x">link</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered Markdown missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"<script>alert(1)", `href="javascript:`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("rendered Markdown contains unsafe %q: %s", forbidden, body)
		}
	}
}

func TestMarkdownAssetContract(t *testing.T) {
	a := newTestApp(t)
	response := testRequest(t, a, http.MethodGet, "/assets/clio-markdown.js", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("markdown asset status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/javascript") {
		t.Errorf("markdown asset content-type = %q", contentType)
	}
	for _, want := range []string{"ClioMarkdown", "render"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("markdown asset missing %q", want)
		}
	}
}

func TestHiddenFieldsOmittedFromUI(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "hidden")
	createTestTable(t, a, "hidden", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "shown", "label": "Shown Label", "type": "string"},
		map[string]any{"name": "secret", "label": "Secret Label", "type": "string", "hidden": true},
	}})
	record := createTestRecord(t, a, "hidden", "items", map[string]any{"shown": "a", "secret": "b"})

	for _, path := range []string{"/default/data/hidden/items", "/default/data/hidden/items/new"} {
		response := testRequest(t, a, http.MethodGet, path, nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Shown Label") {
			t.Errorf("GET %s omitted visible field", path)
		}
		if strings.Contains(response.Body.String(), "Secret Label") {
			t.Errorf("GET %s exposed hidden field", path)
		}
	}

	// The API still returns hidden field values.
	get := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/hidden/tables/items/records/"+record["id"].(string), nil, "")
	if !strings.Contains(get.Body.String(), `"secret":"b"`) {
		t.Errorf("API hid a hidden field: %s", get.Body.String())
	}
}

func TestRecordValuesEscapedInUI(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	record := createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "<b>bold</b>", "category": "service"})
	id := record["id"].(string)
	for _, path := range []string{"/default/data/vehicle/service", "/default/data/vehicle/service/" + id} {
		response := testRequest(t, a, http.MethodGet, path, nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, response.Code)
		}
		if !strings.Contains(response.Body.String(), "&lt;b&gt;bold&lt;/b&gt;") {
			t.Errorf("GET %s did not escape record value", path)
		}
		if strings.Contains(response.Body.String(), "<b>bold</b>") {
			t.Errorf("GET %s rendered record value as HTML", path)
		}
	}
}

func TestDirectoryChildrenAndContentUI(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/pool"}`, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create directory: %d %s", w.Code, w.Body.String())
	}
	if w := testRequest(t, a, http.MethodPut, filesURL("default", "/pool/readme.md"), "# Pool", "text/markdown"); w.Code != http.StatusCreated {
		t.Fatalf("create markdown page: %d %s", w.Code, w.Body.String())
	}
	if w := testRequest(t, a, http.MethodPut, filesURL("default", "/pool/report.html"), "<h1>Report</h1>", "text/html"); w.Code != http.StatusCreated {
		t.Fatalf("create html page: %d %s", w.Code, w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(a.content, "default", "pool", "data.csv"), []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("write raw file: %v", err)
	}

	listing := testRequest(t, a, http.MethodGet, filesURL("default", "/pool"), nil, "")
	var directory map[string]any
	testJSON(t, listing, &directory)
	children := directory["children"].([]any)
	if len(children) != 3 {
		t.Fatalf("directory children = %#v, want 3", children)
	}
	wantKinds := map[string]string{"/pool/data.csv": "file", "/pool/readme.md": "page", "/pool/report.html": "page"}
	for i, child := range children {
		entry := child.(map[string]any)
		path := entry["path"].(string)
		if i > 0 && children[i-1].(map[string]any)["path"].(string) > path {
			t.Errorf("directory children not sorted: %#v", children)
		}
		if entry["kind"] != wantKinds[path] {
			t.Errorf("child %q kind = %v, want %v", path, entry["kind"], wantKinds[path])
		}
		if path == "/pool/readme.md" && entry["content_type"] != "text/markdown" {
			t.Errorf("markdown content_type = %v", entry["content_type"])
		}
	}

	for path, want := range map[string]string{
		"/default/":                       "Open data browser",
		"/default/files":                  "File explorer",
		"/default/files/pool":             "Published content",
		"/default/files/pool/readme.md":   "class=\"published-markdown\"",
		"/default/files/pool/report.html": "<h1>Report</h1>",
	} {
		response := testRequest(t, a, http.MethodGet, path, nil, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
			t.Errorf("GET %s status=%d missing %q", path, response.Code, want)
		}
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/files/pool/missing.md", nil, ""), http.StatusNotFound)
}

// TestLegacyPageAndDirectoryRoutesRemoved asserts the Page API (section 41) and
// the legacy directory facade (section 38) are gone: only POST
// /files/directories remains, for the files directory create and ZIP upload
// (sections 66.2 and 66.7).
func TestLegacyPageAndDirectoryRoutesRemoved(t *testing.T) {
	a := newTestApp(t)
	for _, test := range []struct {
		method, target string
	}{
		{http.MethodGet, "/api/v1/default/files/pages"},
		{http.MethodPost, "/api/v1/default/files/pages"},
		{http.MethodDelete, "/api/v1/default/files/pages?path=%2Fdocs%2Fa.md"},
		{http.MethodGet, "/api/v1/default/files/directories"},
		{http.MethodGet, "/api/v1/default/files/directories?path=%2Fpool"},
		{http.MethodDelete, "/api/v1/default/files/directories?path=%2Fpool"},
	} {
		response := testRequest(t, a, test.method, test.target, nil, "")
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404", test.method, test.target, response.Code)
		}
	}
	// POST /files/directories still serves both create forms.
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/kept"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("POST /files/directories status = %d, want 201: %s", w.Code, w.Body.String())
	}
	archive := buildZip(t, []zipEntry{{name: "weekly.md", body: "# Weekly"}})
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fzipped", archive, "application/zip"); w.Code != http.StatusCreated {
		t.Fatalf("ZIP POST /files/directories status = %d, want 201: %s", w.Code, w.Body.String())
	}
}
