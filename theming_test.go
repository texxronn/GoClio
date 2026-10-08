package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestHumanUIThemingIsUnified asserts that every human page uses the single
// shared palette defined by pageShell, the shared theme controller script and
// the shared nav toggle, and that no server-rendered page still hardcodes the
// old light-only colours. This is the regression test for the drift between the
// Data Browser and File Explorer theme systems (section 66.5 UI).
func TestHumanUIThemingIsUnified(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	record := createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil", "category": "service"})
	id := record["id"].(string)
	putFile(t, a, "default", "/notes/readme.md", "# Hello\n\nBody text.", "text/markdown")

	pages := []struct {
		name string
		path string
	}{
		{"project overview", "/default/"},
		{"data browser", "/default/data"},
		{"table UI", "/default/data/vehicle/service"},
		{"new-record form", "/default/data/vehicle/service/new"},
		{"record view", "/default/data/vehicle/service/" + id},
		{"file explorer", "/default/files"},
		{"content directory", "/default/files/notes"},
		{"published Markdown page", "/default/files/notes/readme.md"},
		{"search", "/default/search"},
		{"help", "/help"},
		{"health", "/health"},
	}
	for _, page := range pages {
		response := testRequest(t, a, http.MethodGet, page.path, nil, "")
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status=%d: %s", page.path, response.Code, response.Body.String())
			continue
		}
		html := response.Body.String()
		for _, want := range []string{"clio-theme-toggle", "clio-theme", "var(--page)", "var(--surface)", "var(--text)", "var(--line)", "var(--accent)", "var(--on-accent)"} {
			if !strings.Contains(html, want) {
				t.Errorf("%s (%s) is missing the shared theme marker %q", page.name, page.path, want)
			}
		}
		if n := strings.Count(html, ":root{color-scheme:light"); n != 1 {
			t.Errorf("%s (%s) has %d shared palette definitions, want the single shared one", page.name, page.path, n)
		}
		if !strings.Contains(html, `:root[data-theme="dark"]`) || !strings.Contains(html, "prefers-color-scheme") {
			t.Errorf("%s (%s) is missing the dark overrides", page.name, page.path)
		}
		for _, hardcoded := range []string{"#f4f7fb", "#e3e9f1", "#718394", "#192a41", "#215f78", "#dce5eb", "#edf0f4"} {
			if strings.Contains(html, hardcoded) {
				t.Errorf("%s (%s) still hardcodes the light colour %s", page.name, page.path, hardcoded)
			}
		}
	}
}
