package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSection59AcceptanceWalkthrough runs the end-to-end acceptance walkthrough
// of SPEC.md section 59 against a fresh instance, adapted to the mandatory
// project-scoped routes of section 66. It covers the record table, the
// generated UI and forms, querying, time-series tables, metadata discovery,
// publishing, directory-tree upload, stable URLs and client-side Markdown.
func TestSection59AcceptanceWalkthrough(t *testing.T) {
	a := newTestApp(t)

	// Step 1 — Create group Vehicle.
	group := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups",
		map[string]any{"name": "vehicle", "label": "Vehicle"}, "application/json")
	if group.Code != http.StatusCreated {
		t.Fatalf("step 1 create group status=%d: %s", group.Code, group.Body.String())
	}

	// Step 2 — Create the Vehicle/Service record table through the API.
	table := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/vehicle/tables",
		map[string]any{
			"name": "service", "label": "Service", "kind": "record",
			"fields": []map[string]any{
				{"name": "date", "label": "Date", "type": "date", "required": true},
				{"name": "odometer", "label": "Odometer", "type": "integer", "required": true},
				{"name": "type", "label": "Type", "type": "enum", "values": []string{"service", "repair"}},
				{"name": "cost", "label": "Cost", "type": "decimal"},
				{"name": "notes", "label": "Notes", "type": "text"},
			},
		}, "application/json")
	if table.Code != http.StatusCreated {
		t.Fatalf("step 2 create table status=%d: %s", table.Code, table.Body.String())
	}

	// Step 3 — Use the generated UI to create, open and edit a record.
	for _, page := range []string{"/default/data/vehicle/service", "/default/data/vehicle/service/new"} {
		if w := testRequest(t, a, http.MethodGet, page, nil, ""); w.Code != http.StatusOK {
			t.Fatalf("step 3 GET %s status=%d: %s", page, w.Code, w.Body.String())
		}
	}
	create := testRequest(t, a, http.MethodPost, "/default/data/vehicle/service/new",
		url.Values{
			"date": {"2026-09-20"}, "odometer": {"42000"}, "type": {"service"},
			"cost": {"320.50"}, "notes": {"Oil and filter"},
		}.Encode(), "application/x-www-form-urlencoded")
	if create.Code != http.StatusSeeOther {
		t.Fatalf("step 3 create form status=%d: %s", create.Code, create.Body.String())
	}
	location := create.Header().Get("Location")
	const recordPrefix = "/default/data/vehicle/service/"
	if !strings.HasPrefix(location, recordPrefix) {
		t.Fatalf("step 3 create form redirect=%q", location)
	}
	id := strings.TrimPrefix(location, recordPrefix)
	recordPage := testRequest(t, a, http.MethodGet, location, nil, "")
	if recordPage.Code != http.StatusOK || !strings.Contains(recordPage.Body.String(), "Oil and filter") {
		t.Fatalf("step 3 record page status=%d body=%s", recordPage.Code, recordPage.Body.String())
	}
	edit := testRequest(t, a, http.MethodPost, location+"/edit",
		url.Values{
			"date": {"2026-09-20"}, "odometer": {"42000"}, "type": {"service"},
			"cost": {"320.50"}, "notes": {"Oil, filter and wipers"},
		}.Encode(), "application/x-www-form-urlencoded")
	if edit.Code != http.StatusSeeOther {
		t.Fatalf("step 3 edit form status=%d: %s", edit.Code, edit.Body.String())
	}
	edited := testRequest(t, a, http.MethodGet, location, nil, "")
	if edited.Code != http.StatusOK || !strings.Contains(edited.Body.String(), "Oil, filter and wipers") {
		t.Fatalf("step 3 edited record not persisted: status=%d body=%s", edited.Code, edited.Body.String())
	}

	// Step 4 — Query data: list, sort, filter, page, group and aggregate.
	recordsPath := "/api/v1/default/data/groups/vehicle/tables/service/records"
	list := testRequest(t, a, http.MethodGet, recordsPath, nil, "")
	if list.Code != http.StatusOK {
		t.Fatalf("step 4 list status=%d: %s", list.Code, list.Body.String())
	}
	var listing struct {
		Data []map[string]any `json:"data"`
		Page map[string]any   `json:"page"`
	}
	testJSON(t, list, &listing)
	if len(listing.Data) != 1 || listing.Data[0]["id"] != id {
		t.Fatalf("step 4 list data=%#v, want the created record %s", listing.Data, id)
	}
	for _, query := range []string{
		recordsPath + "?sort=odometer&order=desc",
		recordsPath + "?filter.type=service",
		recordsPath + "?filter.cost.gte=100",
		recordsPath + "?limit=1&offset=0",
		recordsPath + "?group_by=type&aggregate=count",
		recordsPath + "?aggregate=count,cost:sum",
	} {
		if w := testRequest(t, a, http.MethodGet, query, nil, ""); w.Code != http.StatusOK {
			t.Fatalf("step 4 query %s status=%d: %s", query, w.Code, w.Body.String())
		}
	}

	// Step 5 — Create the Pool/Measurements time-series table and query it.
	pool := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups",
		map[string]any{"name": "pool", "label": "Pool"}, "application/json")
	if pool.Code != http.StatusCreated {
		t.Fatalf("step 5 create pool group status=%d: %s", pool.Code, pool.Body.String())
	}
	timeseries := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/pool/tables",
		map[string]any{
			"name": "measurements", "label": "Measurements", "kind": "timeseries",
			"timestamp_field": "timestamp",
			"fields": []map[string]any{
				{"name": "timestamp", "type": "datetime", "required": true},
				{"name": "ph", "type": "decimal"},
				{"name": "free_chlorine", "type": "decimal"},
				{"name": "temperature", "type": "decimal"},
			},
		}, "application/json")
	if timeseries.Code != http.StatusCreated {
		t.Fatalf("step 5 create timeseries status=%d: %s", timeseries.Code, timeseries.Body.String())
	}
	measurementsPath := "/api/v1/default/data/groups/pool/tables/measurements/records"
	for _, reading := range []map[string]any{
		{"timestamp": "2026-09-20T08:00:00Z", "ph": "7.4", "free_chlorine": "2.1", "temperature": "27.5"},
		{"timestamp": "2026-09-21T08:00:00Z", "ph": "7.5", "free_chlorine": "1.9", "temperature": "28.0"},
	} {
		if w := testRequest(t, a, http.MethodPost, measurementsPath, reading, "application/json"); w.Code != http.StatusCreated {
			t.Fatalf("step 5 insert measurement status=%d: %s", w.Code, w.Body.String())
		}
	}
	for _, query := range []string{
		measurementsPath + "?sort=timestamp&order=desc&limit=1",
		measurementsPath + "?from=2026-09-20T00:00:00Z&to=2026-09-22T00:00:00Z",
		measurementsPath + "?bucket=day&aggregate=temperature:avg",
		measurementsPath + "?aggregate=temperature:avg,temperature:min,temperature:max",
	} {
		if w := testRequest(t, a, http.MethodGet, query, nil, ""); w.Code != http.StatusOK {
			t.Fatalf("step 5 query %s status=%d: %s", query, w.Code, w.Body.String())
		}
	}

	// Step 6 — Discover metadata entirely through the API.
	metadata := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata", nil, "")
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"groups"`) {
		t.Fatalf("step 6 metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	tableMetadata := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata/groups/pool/tables/measurements", nil, "")
	for _, want := range []string{`"kind":"timeseries"`, `"timestamp_field":"timestamp"`, `"temperature"`} {
		if tableMetadata.Code != http.StatusOK || !strings.Contains(tableMetadata.Body.String(), want) {
			t.Fatalf("step 6 table metadata missing %q: status=%d body=%s", want, tableMetadata.Code, tableMetadata.Body.String())
		}
	}

	// Step 7 — Publish Markdown under /pool/reports and open its URL.
	latest := testRequest(t, a, http.MethodPut, filesURL("default", "/pool/reports/latest.md"),
		"# Pool latest\n\nChlorine is **2.1 ppm**.", "text/markdown")
	if latest.Code != http.StatusCreated {
		t.Fatalf("step 7 publish status=%d: %s", latest.Code, latest.Body.String())
	}
	rendered := testRequest(t, a, http.MethodGet, "/default/files/pool/reports/latest.md", nil, "")
	if rendered.Code != http.StatusOK || !strings.Contains(rendered.Body.String(), "<h1>Pool latest</h1>") {
		t.Fatalf("step 7 rendered page status=%d body=%s", rendered.Code, rendered.Body.String())
	}

	// Step 8 — Upload a complete directory tree and browse it.
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, body := range map[string]string{
		"index.md":               "# Index",
		"measurements/latest.md": "# Latest",
		"reports/weekly.html":    "<!doctype html><title>Weekly</title>",
		"reports/monthly.md":     "# Monthly",
	} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("step 8 create zip entry %q: %v", name, err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatalf("step 8 write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("step 8 close zip: %v", err)
	}
	upload := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fpool%2Freports", archive.Bytes(), "application/zip")
	if upload.Code != http.StatusCreated {
		t.Fatalf("step 8 directory upload status=%d: %s", upload.Code, upload.Body.String())
	}
	for _, path := range []string{
		"/pool/reports/index.md",
		"/pool/reports/measurements/latest.md",
		"/pool/reports/reports/weekly.html",
		"/pool/reports/reports/monthly.md",
	} {
		entry := testRequest(t, a, http.MethodGet, "/api/v1/default/files?path="+url.QueryEscape(path), nil, "")
		if entry.Code != http.StatusOK {
			t.Fatalf("step 8 entry %s status=%d: %s", path, entry.Code, entry.Body.String())
		}
		var rep map[string]any
		testJSON(t, entry, &rep)
		id, _ := rep["id"].(string)
		if id == "" {
			t.Fatalf("step 8 entry %s has no id: %#v", path, rep)
		}
		// Open every resulting page through its path URL.
		page := "/default/files" + path
		if w := testRequest(t, a, http.MethodGet, page, nil, ""); w.Code != http.StatusOK {
			t.Fatalf("step 8 open %s status=%d", page, w.Code)
		}
		// Confirm the stable ID URL serves the current bytes as a download.
		stable := testRequest(t, a, http.MethodGet, "/default/files/id/"+id, nil, "")
		if stable.Code != http.StatusOK || stable.Header().Get("Content-Disposition") == "" {
			t.Fatalf("step 8 stable URL for %s status=%d disposition=%q", path, stable.Code, stable.Header().Get("Content-Disposition"))
		}
	}
	// Browse the uploaded directory through the files API.
	dir := testRequest(t, a, http.MethodGet, "/api/v1/default/files?path=%2Fpool%2Freports%2Freports", nil, "")
	if dir.Code != http.StatusOK || !strings.Contains(dir.Body.String(), "/pool/reports/reports/weekly.html") {
		t.Fatalf("step 8 directory listing status=%d body=%s", dir.Code, dir.Body.String())
	}

	// Step 9 — Client-side Markdown: fetch the source and render it with the asset.
	asset := testRequest(t, a, http.MethodGet, "/assets/clio-markdown.js", nil, "")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "ClioMarkdown") {
		t.Fatalf("step 9 markdown asset status=%d", asset.Code)
	}
	entry := testRequest(t, a, http.MethodGet, "/api/v1/default/files?path=%2Fpool%2Freports%2Flatest.md", nil, "")
	var entryRep map[string]any
	testJSON(t, entry, &entryRep)
	source := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+entryRep["id"].(string)+"/content", nil, "")
	if source.Code != http.StatusOK || !strings.Contains(source.Body.String(), "Chlorine is **2.1 ppm**") {
		t.Fatalf("step 9 source status=%d body=%s", source.Code, source.Body.String())
	}

	// Step 10 — /api/v1/help (and /help) must document enough to operate Clio.
	help := testRequest(t, a, http.MethodGet, "/api/v1/help", nil, "")
	for _, want := range []string{
		"# Clio API v1",
		"/api/v1/{project}/data/groups",
		"/api/v1/{project}/files",
		"/api/v1/{project}/search",
		"/api/v1/{project}/files/extraction",
		"/api/v1/projects",
		"sqlite_fts5",
		"CLIO_WEBDAV_ENABLED",
		"clio backup",
	} {
		if help.Code != http.StatusOK || !strings.Contains(help.Body.String(), want) {
			t.Fatalf("step 10 help missing %q: status=%d", want, help.Code)
		}
	}
}

// TestExamplesStayProjectScoped guards examples/README.md against a regression
// to the pre-project unscoped routes.
func TestExamplesStayProjectScoped(t *testing.T) {
	readme := string(readExample(t, "README.md"))
	for _, unscoped := range []string{
		"/api/v1/groups",
		"/api/v1/metadata",
		"/api/v1/files",
		"/api/v1/search",
		"/api/v1/directories",
		"/api/v1/pages",
	} {
		if strings.Contains(readme, unscoped) {
			t.Errorf("examples/README.md contains the unscoped route %q", unscoped)
		}
	}
	if !strings.Contains(readme, "/api/v1/default/data/groups") {
		t.Errorf("examples/README.md does not document the project-scoped data route")
	}
}
