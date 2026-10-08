package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	root := t.TempDir()
	db, err := openDatabase(filepath.Join(root, "clio.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	content := filepath.Join(root, "content")
	if err := os.MkdirAll(content, 0755); err != nil {
		t.Fatalf("create test content directory: %v", err)
	}
	return &app{db: db, content: content, baseURL: "http://clio.test", contentMu: &sync.Mutex{}}
}

func TestDirectoryUIShowsCreateForm(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}

	r := httptest.NewRequest(http.MethodGet, "/default/files/notes", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /default/files/notes status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `<form method="post" action="/default/files/notes">`) || !strings.Contains(w.Body.String(), `name="name"`) || !strings.Contains(w.Body.String(), `class="content-directory"`) || !strings.Contains(w.Body.String(), `aria-label="Breadcrumb"`) {
		t.Fatal("directory page does not contain the create-directory form")
	}
}

func TestHomePageAndFavicon(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodGet, "/default/files", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /default/files status = %d, want %d", w.Code, http.StatusOK)
	}
	for _, want := range []string{"Everything you need", "home-hero", "Open data browser", "Published content", `href='/favicon.svg'`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("home page missing %q", want)
		}
	}

	// Bare / redirects to the default project.
	redirect := httptest.NewRequest(http.MethodGet, "/", nil)
	redirectRecorder := httptest.NewRecorder()
	a.ServeHTTP(redirectRecorder, redirect)
	if redirectRecorder.Code != http.StatusFound || redirectRecorder.Header().Get("Location") != "/default/" {
		t.Errorf("GET / = %d location=%q, want 302 /default/", redirectRecorder.Code, redirectRecorder.Header().Get("Location"))
	}

	r = httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /favicon.svg status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Errorf("favicon content type = %q, want image/svg+xml", got)
	}
	if !strings.Contains(w.Body.String(), "<svg") {
		t.Error("favicon response does not contain SVG markup")
	}
}

func TestHelpAndHealthPagesUseWorkspaceLayout(t *testing.T) {
	a := newTestApp(t)
	for _, test := range []struct {
		path string
		want []string
	}{
		{path: "/help", want: []string{`class="help-page"`, `class="help-document"`, "Clio API v1", "DOCUMENTATION"}},
		{path: "/health", want: []string{`class="health-page"`, `class="health-grid"`, "All systems operational", "View raw health report"}},
	} {
		r := httptest.NewRequest(http.MethodGet, test.path, nil)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", test.path, w.Code, http.StatusOK)
		}
		for _, want := range test.want {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("GET %s missing %q", test.path, want)
			}
		}
	}
}

func TestDirectoryUIPostCreatesChildAndRedirects(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}

	r := httptest.NewRequest(http.MethodPost, "/default/files/notes", strings.NewReader(url.Values{"name": {"weekly"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /default/files/notes status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/default/files/notes/weekly" {
		t.Fatalf("redirect location = %q, want /default/files/notes/weekly", got)
	}
	if info, err := os.Stat(filepath.Join(a.content, "default", "notes", "weekly")); err != nil || !info.IsDir() {
		t.Fatalf("created child directory missing or not a directory: info=%v err=%v", info, err)
	}
}

func TestDirectoryUIPostCreatesRootChild(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodPost, "/default/files", strings.NewReader(url.Values{"name": {"garden"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/default/files/garden" {
		t.Fatalf("root directory POST returned status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	if info, err := os.Stat(filepath.Join(a.content, "default", "garden")); err != nil || !info.IsDir() {
		t.Fatalf("root child directory missing or not a directory: info=%v err=%v", info, err)
	}
}

func TestDirectoryUIPostRejectsInvalidAndExistingNames(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}
	if _, ae := a.createDirectory("/notes/existing"); ae != nil {
		t.Fatalf("create existing test child: %v", ae)
	}

	tests := []struct {
		name string
		want int
	}{
		{name: "../escape", want: http.StatusUnprocessableEntity},
		{name: "two/segments", want: http.StatusUnprocessableEntity},
		{name: "existing", want: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/default/files/notes", strings.NewReader(url.Values{"name": {test.name}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("POST /default/files/notes name=%q status = %d, want %d", test.name, w.Code, test.want)
			}
		})
	}
}

func TestFilesDirectoryCreation(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/default/files/directories", strings.NewReader(`{"path":"/from-api"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/default/files/directories status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode API response: %v", err)
	}
	if got["path"] != "/from-api" {
		t.Fatalf("created path = %v, want /from-api", got["path"])
	}
}
