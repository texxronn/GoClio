package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIErrorCodesAndHeaders(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "errors")

	cases := []struct {
		name     string
		method   string
		path     string
		body     any
		ctype    string
		status   int
		errorVal string
	}{
		{"validation", http.MethodPost, "/api/v1/default/data/groups", `{"name":"Bad.Name"}`, "application/json", http.StatusUnprocessableEntity, "validation_error"},
		{"not found", http.MethodGet, "/api/v1/default/data/groups/missing", nil, "", http.StatusNotFound, "not_found"},
		{"conflict", http.MethodPost, "/api/v1/default/data/groups", `{"name":"errors"}`, "application/json", http.StatusConflict, "conflict"},
		{"method not allowed", http.MethodDelete, "/api/v1/health", nil, "", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"bad request", http.MethodGet, "/api/v1/default/data/groups?bad=%zz", nil, "", http.StatusBadRequest, "bad_request"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := testRequest(t, a, test.method, test.path, test.body, test.ctype)
			assertAPIError(t, response, test.status)
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("error content-type = %q", contentType)
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("missing nosniff header")
			}
			if !strings.Contains(response.Body.String(), `"error":"`+test.errorVal+`"`) {
				t.Errorf("error code = %s, want %q", response.Body.String(), test.errorVal)
			}
			if !strings.Contains(response.Body.String(), `"message":`) {
				t.Errorf("error response lacks message: %s", response.Body.String())
			}
		})
	}

	t.Run("payload too large", func(t *testing.T) {
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups", strings.Repeat("x", int(bodyLimit+1)), "application/json")
		if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"error":"body_too_large"`) {
			t.Errorf("oversized body response = %d %s", response.Code, response.Body.String())
		}
	})
}

func TestMetadataShapes(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "vehicle")
	createTestTable(t, a, "vehicle", map[string]any{"name": "service", "label": "Vehicle Service", "fields": []any{
		map[string]any{"name": "date", "type": "date", "required": true},
	}})

	root := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata", nil, "")
	var rootBody map[string]any
	testJSON(t, root, &rootBody)
	if rootBody["base_url"] != "http://clio.test" || rootBody["api_version"] != "v1" {
		t.Errorf("metadata root = %#v", rootBody)
	}
	if groups, ok := rootBody["groups"].([]any); !ok || len(groups) != 1 {
		t.Errorf("metadata root groups = %#v", rootBody["groups"])
	}

	group := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/vehicle", nil, "")
	var groupBody map[string]any
	testJSON(t, group, &groupBody)
	if groupBody["name"] != "vehicle" || groupBody["url"] != "http://clio.test/default/data/vehicle" || groupBody["api_url"] != "http://clio.test/api/v1/default/data/groups/vehicle" {
		t.Errorf("group metadata = %#v", groupBody)
	}
	if tables, ok := groupBody["tables"].([]any); !ok || len(tables) != 1 {
		t.Errorf("group metadata tables = %#v", groupBody["tables"])
	}

	table := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata/groups/vehicle/tables/service", nil, "")
	var tableBody map[string]any
	testJSON(t, table, &tableBody)
	if tableBody["kind"] != "record" || tableBody["records_url"] != "http://clio.test/api/v1/default/data/groups/vehicle/tables/service/records" {
		t.Errorf("table metadata URLs = %#v", tableBody)
	}
	if template, ok := tableBody["record_url_template"].(string); !ok || !strings.HasSuffix(template, "/{id}") {
		t.Errorf("record_url_template = %v", tableBody["record_url_template"])
	}
	fields := tableBody["fields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["required"] != true {
		t.Errorf("table metadata fields = %#v", fields)
	}
}

func TestHealthCountsAndVersion(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "readings", "fields": []any{
		map[string]any{"name": "value", "type": "decimal"},
	}})
	createTestRecord(t, a, "pool", "readings", map[string]any{"value": "1"})
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/reports"}`, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create directory: %d", w.Code)
	}
	if w := testRequest(t, a, http.MethodPut, filesURL("default", "/reports/summary.md"), "# S", "text/markdown"); w.Code != http.StatusCreated {
		t.Fatalf("create page: %d", w.Code)
	}

	response := testRequest(t, a, http.MethodGet, "/api/v1/health", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
	var health map[string]any
	testJSON(t, response, &health)
	if health["status"] != "ok" || health["version"] != "2.2.0" {
		t.Errorf("health status/version = %#v", health)
	}
	if db := health["database"].(map[string]any); db["status"] != "ok" {
		t.Errorf("health database = %#v", db)
	}
	for key, min := range map[string]float64{"groups": 1, "tables": 1, "records": 1, "pages": 1, "directories": 1} {
		if got := health[key].(float64); got < min {
			t.Errorf("health %s = %v, want >= %v", key, got, min)
		}
	}
	if _, ok := health["memory"].(map[string]any); !ok {
		t.Errorf("health memory missing: %#v", health["memory"])
	}
}

func TestHelpFormats(t *testing.T) {
	a := newTestApp(t)
	human := testRequest(t, a, http.MethodGet, "/help", nil, "")
	if human.Code != http.StatusOK || !strings.HasPrefix(human.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET /help = %d %q", human.Code, human.Header().Get("Content-Type"))
	}
	machine := testRequest(t, a, http.MethodGet, "/api/v1/help", nil, "")
	if machine.Code != http.StatusOK || !strings.HasPrefix(machine.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("GET /api/v1/help = %d %q", machine.Code, machine.Header().Get("Content-Type"))
	}
	body := machine.Body.String()
	for _, want := range []string{"API v1", "filter.", "group_by", "bucket", "ClioMarkdown", "/assets/clio.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("machine help missing %q", want)
		}
	}
}

func TestMethodRestrictionsAndReservedRoutes(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{"/health", "/api/v1/health", "/help", "/api/v1/help", "/assets/clio-markdown.js", "/assets/clio.js"} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, path, nil, ""), http.StatusMethodNotAllowed)
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/nope", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api", nil, ""), http.StatusNotFound)

	favicon := testRequest(t, a, http.MethodGet, "/favicon.svg", nil, "")
	if favicon.Code != http.StatusOK || !strings.HasPrefix(favicon.Header().Get("Content-Type"), "image/svg+xml") {
		t.Errorf("GET /favicon.svg = %d %q", favicon.Code, favicon.Header().Get("Content-Type"))
	}
}
