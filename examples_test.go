package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("examples", name))
	if err != nil {
		t.Fatalf("read example %s: %v", name, err)
	}
	return data
}

func TestExampleTableMetadataAndRecords(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "vehicle")
	createTestGroup(t, a, "pool")

	for _, example := range []struct {
		group, file, table, kind string
	}{
		{"vehicle", "record-table.json", "service", "record"},
		{"pool", "timeseries-table.json", "measurements", "timeseries"},
	} {
		response := testRequest(t, a, http.MethodPost, "/api/v1/groups/"+example.group+"/tables", readExample(t, example.file), "application/json")
		if response.Code != http.StatusCreated {
			t.Fatalf("create table from %s status = %d: %s", example.file, response.Code, response.Body.String())
		}
		var metadata map[string]any
		testJSON(t, response, &metadata)
		if metadata["name"] != example.table || metadata["kind"] != example.kind {
			t.Fatalf("unexpected metadata returned for %s: %#v", example.file, metadata)
		}
	}

	service := testRequest(t, a, http.MethodPost, "/api/v1/groups/vehicle/tables/service/records", map[string]any{
		"service_date": "2026-09-20", "odometer_km": 42000, "category": "service", "cost": "320.50", "notes": "Oil and filter",
	}, "application/json")
	if service.Code != http.StatusCreated {
		t.Fatalf("create example service record status = %d: %s", service.Code, service.Body.String())
	}
	measurement := testRequest(t, a, http.MethodPost, "/api/v1/groups/pool/tables/measurements/records", map[string]any{
		"measured_at": "2026-09-20T08:00:00Z", "ph": "7.4", "free_chlorine": "2.1", "temperature_c": "27.5",
	}, "application/json")
	if measurement.Code != http.StatusCreated {
		t.Fatalf("create example measurement status = %d: %s", measurement.Code, measurement.Body.String())
	}
}

func TestMarkdownAndClientRenderingExamplesPublish(t *testing.T) {
	a := newTestApp(t)
	for _, example := range []struct {
		path, contentType, file string
	}{
		{"/examples/report.md", "text/markdown", "report.md"},
		{"/examples/client-rendering.html", "text/html", "client-rendering.html"},
	} {
		created := testRequest(t, a, http.MethodPost, "/api/v1/pages", map[string]any{
			"path": example.path, "content_type": example.contentType, "content": string(readExample(t, example.file)),
		}, "application/json")
		if created.Code != http.StatusCreated {
			t.Fatalf("publish %s status = %d: %s", example.file, created.Code, created.Body.String())
		}
	}
	markdown := testRequest(t, a, http.MethodGet, "/examples/report.md", nil, "")
	for _, want := range []string{"<h1>Pool check</h1>", "<strong>7.4</strong>", "<code>2.1 ppm</code>"} {
		if markdown.Code != http.StatusOK || !strings.Contains(markdown.Body.String(), want) {
			t.Errorf("published report missing %q: status=%d body=%s", want, markdown.Code, markdown.Body.String())
		}
	}
	clientPage := testRequest(t, a, http.MethodGet, "/examples/client-rendering.html", nil, "")
	for _, want := range []string{"/assets/clio-markdown.js", "/api/v1/pages?path=%2Fexamples%2Freport.md", "ClioMarkdown.render(page.content)"} {
		if clientPage.Code != http.StatusOK || !strings.Contains(clientPage.Body.String(), want) {
			t.Errorf("client-rendering example missing %q: status=%d body=%s", want, clientPage.Code, clientPage.Body.String())
		}
	}
}
