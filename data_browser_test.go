package main

import (
	"fmt"
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
	for _, want := range []string{"Data Browser", "assets/clio.js", "Clio.DataBrowser.mount", "clio-data-browser", "browser-table-view", "browser-new-collection", "browser-new-table", "browser-create", "browser-field-row", "browser-field-config", "browser-field-enum", "browser-field-values", "browser-field-reference", "browser-field-reference-group", "browser-field-reference-table", "clio-theme-toggle", "clio-theme", ":root{color-scheme:light", `:root[data-theme="dark"]`, "prefers-color-scheme", "var(--page)", "var(--surface-raised)", "var(--text)", "var(--line)", "var(--accent)", "var(--accent-soft)", "var(--hover)", "var(--danger)", "var(--on-accent)"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("GET /default/data missing %q", want)
		}
	}
	// The Data Browser relies on the shared nav theme toggle; it no longer
	// renders or defines its own duplicate toggle (section 66.5 UI).
	if strings.Contains(response.Body.String(), "browser-theme-toggle") {
		t.Errorf("GET /default/data still defines the duplicate .browser-theme-toggle")
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

// metadataField reads one table's metadata and returns the named field
// definition, so a test can assert a field's type-specific configuration.
func metadataField(t *testing.T, a *app, group, table, field string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodGet, fmt.Sprintf("/api/v1/default/data/groups/%s/tables/%s", group, table), nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get %s/%s metadata status=%d: %s", group, table, w.Code, w.Body.String())
	}
	var body map[string]any
	testJSON(t, w, &body)
	fields, _ := body["fields"].([]any)
	for _, item := range fields {
		f, _ := item.(map[string]any)
		if f["name"] == field {
			return f
		}
	}
	t.Fatalf("field %q not found in %s/%s metadata", field, group, table)
	return nil
}

// TestEnumAndReferenceFieldsStayWithTheirTable verifies the invariant the Data
// Browser's field editor relies on: enum values and reference targets belong to
// the individual field definition of the individual table, so two same-named
// fields in two tables keep their own configuration and never leak into each
// other through a shared registry.
func TestEnumAndReferenceFieldsStayWithTheirTable(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestGroup(t, a, "catalog")
	createTestTable(t, a, "pool", map[string]any{"name": "measurements", "fields": []any{map[string]any{"name": "value", "type": "string"}}})
	createTestTable(t, a, "catalog", map[string]any{"name": "parts", "fields": []any{map[string]any{"name": "code", "type": "string"}}})

	createTestTable(t, a, "pool", map[string]any{"name": "readings", "fields": []any{
		map[string]any{"name": "state", "type": "enum", "values": []string{"open", "closed"}},
		map[string]any{"name": "target", "type": "reference", "group": "pool", "table": "measurements"},
	}})
	createTestTable(t, a, "pool", map[string]any{"name": "alerts", "fields": []any{
		map[string]any{"name": "state", "type": "enum", "values": []string{"draft", "published"}},
		map[string]any{"name": "target", "type": "reference", "group": "catalog", "table": "parts"},
	}})

	readingsState := metadataField(t, a, "pool", "readings", "state")
	if fmt.Sprint(readingsState["values"]) != "[open closed]" {
		t.Errorf("readings.state values = %#v, want [open closed]", readingsState["values"])
	}
	alertsState := metadataField(t, a, "pool", "alerts", "state")
	if fmt.Sprint(alertsState["values"]) != "[draft published]" {
		t.Errorf("alerts.state values = %#v, want [draft published]", alertsState["values"])
	}
	readingsTarget := metadataField(t, a, "pool", "readings", "target")
	if readingsTarget["group"] != "pool" || readingsTarget["table"] != "measurements" {
		t.Errorf("readings.target = %#v, want group pool table measurements", readingsTarget)
	}
	alertsTarget := metadataField(t, a, "pool", "alerts", "target")
	if alertsTarget["group"] != "catalog" || alertsTarget["table"] != "parts" {
		t.Errorf("alerts.target = %#v, want group catalog table parts", alertsTarget)
	}
}
