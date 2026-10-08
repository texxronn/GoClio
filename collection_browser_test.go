package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestCollectionBrowserRoutes(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	for _, route := range []string{"/default/collections", "/default/collections/vehicle", "/default/collections/vehicle/service"} {
		response := testRequest(t, a, http.MethodGet, route, nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d: %s", route, response.Code, response.Body.String())
		}
		for _, want := range []string{"Data Browser", "assets/clio.js", "Clio.DataBrowser.mount", "clio-data-browser", "browser-theme-toggle", "browser-table-view", "prefers-color-scheme"} {
			if !strings.Contains(response.Body.String(), want) {
				t.Errorf("GET %s missing %q", route, want)
			}
		}
	}
	for _, route := range []string{"/default/collections/bad.name/service", "/default/collections/vehicle/service/extra"} {
		response := testRequest(t, a, http.MethodGet, route, nil, "")
		if response.Code != http.StatusNotFound && response.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %s status=%d, want invalid route status", route, response.Code)
		}
	}
	if response := testRequest(t, a, http.MethodPost, "/default/collections/vehicle/service", nil, ""); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /default/collections status=%d, want 405", response.Code)
	}
	response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/pages", map[string]any{"path": "/collections/page.md", "content_type": "text/markdown", "content": "blocked"}, "application/json")
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("publishing under /default/collections status=%d, want 422", response.Code)
	}
}
