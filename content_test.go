package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	return &app{db: db, content: content, baseURL: "http://clio.test"}
}

func TestDirectoryUIShowsCreateForm(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}

	r := httptest.NewRequest(http.MethodGet, "/notes", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /notes status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `<form method=post action="/notes">`) || !strings.Contains(w.Body.String(), `name=name`) {
		t.Fatal("directory page does not contain the create-directory form")
	}
}

func TestDirectoryUIPostCreatesChildAndRedirects(t *testing.T) {
	a := newTestApp(t)
	if _, ae := a.createDirectory("/notes"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}

	r := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(url.Values{"name": {"weekly"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /notes status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/notes/weekly" {
		t.Fatalf("redirect location = %q, want /notes/weekly", got)
	}
	if info, err := os.Stat(filepath.Join(a.content, "notes", "weekly")); err != nil || !info.IsDir() {
		t.Fatalf("created child directory missing or not a directory: info=%v err=%v", info, err)
	}
}

func TestDirectoryUIPostCreatesRootChild(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"name": {"garden"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/garden" {
		t.Fatalf("root directory POST returned status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	if info, err := os.Stat(filepath.Join(a.content, "garden")); err != nil || !info.IsDir() {
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
			r := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(url.Values{"name": {test.name}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("POST /notes name=%q status = %d, want %d", test.name, w.Code, test.want)
			}
		})
	}
}

func TestDirectoryAPICreationStillWorks(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/directories", strings.NewReader(`{"path":"/from-api"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/directories status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode API response: %v", err)
	}
	if got["path"] != "/from-api" {
		t.Fatalf("created path = %v, want /from-api", got["path"])
	}
}
