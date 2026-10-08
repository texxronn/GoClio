package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestDataBrowserRoutes(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)

	// The client collection data browser is served at /{project}/data
	// (section 35 re-scoped by section 66.5); its state lives in query
	// parameters.
	response := testRequest(t, a, http.MethodGet, "/default/data", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /default/data status=%d: %s", response.Code, response.Body.String())
	}
	for _, want := range []string{"Data Browser", "assets/clio.js", "Clio.DataBrowser.mount", "clio-data-browser", "browser-theme-toggle", "browser-table-view", "clio-theme-toggle", "clio-theme", ":root{color-scheme:light", `:root[data-theme="dark"]`, "prefers-color-scheme", "var(--page)", "var(--surface-raised)", "var(--text)", "var(--line)", "var(--accent)", "var(--accent-soft)", "var(--hover)", "var(--danger)", "var(--on-accent)"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("GET /default/data missing %q", want)
		}
	}
	// The shared palette is defined once (in pageShell), not again in the data
	// browser body.
	if n := strings.Count(response.Body.String(), ":root{color-scheme:light"); n != 1 {
		t.Errorf("GET /default/data defines the palette %d times, want the single shared definition", n)
	}
	if strings.Contains(response.Body.String(), "#f4f7fb") {
		t.Errorf("GET /default/data still hardcodes the light page colour")
	}

	// The per-table server-rendered table UI and group listing remain under
	// /{project}/data/....
	for _, route := range []string{"/default/data/vehicle", "/default/data/vehicle/service"} {
		if r := testRequest(t, a, http.MethodGet, route, nil, ""); r.Code != http.StatusOK {
			t.Errorf("GET %s status=%d, want 200: %s", route, r.Code, r.Body.String())
		}
	}

	// The data root only serves GET/HEAD.
	if r := testRequest(t, a, http.MethodPost, "/default/data", nil, ""); r.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /default/data status=%d, want 405", r.Code)
	}

	// The superseded /collections route is gone (section 66.5).
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/collections", nil, ""), http.StatusNotFound)

	// Former reserved root names are ordinary content names now that content
	// lives under /{project}/files/ (section 66.4).
	if r := testRequest(t, a, http.MethodPut, filesURL("default", "/collections/page.md"), "allowed", "text/markdown"); r.Code != http.StatusCreated {
		t.Errorf("publishing under /default/collections status=%d, want 201", r.Code)
	}
}
