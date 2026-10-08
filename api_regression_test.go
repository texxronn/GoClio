package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var body map[string]any
	testJSON(t, response, &body)
	if body["error"] == nil || body["message"] == nil {
		t.Fatalf("error response lacks error/message fields: %#v", body)
	}
}

func TestAPIRequestAndQueryErrors(t *testing.T) {
	a := newTestApp(t)
	for _, body := range []string{"", `{"name":`, `[]`, `{"name":"valid"} {"extra":true}`} {
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups", body, "application/json")
		assertAPIError(t, response, http.StatusUnprocessableEntity)
	}
	for _, body := range []string{`{"name":"Bad.Name"}`, `{"name":"bad name"}`} {
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups", body, "application/json")
		assertAPIError(t, response, http.StatusUnprocessableEntity)
	}
	createTestGroup(t, a, "query")
	createTestTable(t, a, "query", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
		map[string]any{"name": "enabled", "type": "boolean"},
		map[string]any{"name": "amount", "type": "decimal"},
	}})
	base := "/api/v1/default/data/groups/query/tables/items/records"
	for _, query := range []string{
		"limit=0", "offset=-1", "sort=missing", "sort=label&order=sideways",
		"filter.missing=value", "filter.enabled.contains=true", "aggregate=label:sum", "bucket=hour",
	} {
		t.Run(query, func(t *testing.T) {
			assertAPIError(t, testRequest(t, a, http.MethodGet, base+"?"+query, nil, ""), http.StatusUnprocessableEntity)
		})
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/missing", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/query/tables/missing", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodPut, "/api/v1/default/data/groups/query", nil, ""), http.StatusMethodNotAllowed)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/not-an-endpoint", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups?bad=%zz", nil, ""), http.StatusBadRequest)
}

func TestPageAPIUpdateDeleteAndValidation(t *testing.T) {
	a := newTestApp(t)
	create := func(path, contentType, content string) *httptest.ResponseRecorder {
		return testRequest(t, a, http.MethodPost, "/api/v1/default/files/pages", map[string]any{"path": path, "content_type": contentType, "content": content}, "application/json")
	}
	for _, test := range []struct {
		path, contentType string
	}{
		{"/note.txt", "text/plain"},
		{"/note.md", "text/html"},
		{"/api/note.md", "text/markdown"},
	} {
		assertAPIError(t, create(test.path, test.contentType, "invalid"), http.StatusUnprocessableEntity)
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/pages", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/pages?path=%2Fmissing.md", nil, ""), http.StatusNotFound)

	created := create("/reports/current.md", "text/markdown", "first")
	if created.Code != http.StatusCreated {
		t.Fatalf("create page status = %d, want 201: %s", created.Code, created.Body.String())
	}
	var original map[string]any
	testJSON(t, created, &original)
	updated := create("/reports/current.md", "text/markdown", "second")
	if updated.Code != http.StatusOK {
		t.Fatalf("update page status = %d, want 200: %s", updated.Code, updated.Body.String())
	}
	var changed map[string]any
	testJSON(t, updated, &changed)
	if changed["content"] != "second" || changed["created_at"] != original["created_at"] || changed["updated_at"] == nil {
		t.Fatalf("page update did not preserve creation metadata and update content: before=%#v after=%#v", original, changed)
	}
	remove := testRequest(t, a, http.MethodDelete, "/api/v1/default/files/pages?path="+url.QueryEscape("/reports/current.md"), nil, "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete page status = %d, want 204: %s", remove.Code, remove.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/pages?path=%2Freports%2Fcurrent.md", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/pages", nil, ""), http.StatusUnprocessableEntity)
}

func TestDirectoryAPIPathAndRootGuards(t *testing.T) {
	a := newTestApp(t)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/directories?path=%2F", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/directories", nil, ""), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/directories?path=%2Fmissing", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/reports/"}`, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", `{"path":"/api/private"}`, "application/json"), http.StatusUnprocessableEntity)
	created := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/reports"}, "application/json")
	if created.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d, want 201", created.Code)
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/reports"}, "application/json"), http.StatusConflict)
	page := testRequest(t, a, http.MethodPost, "/api/v1/default/files/pages", map[string]any{"path": "/block.md", "content_type": "text/markdown", "content": "blocked"}, "application/json")
	if page.Code != http.StatusCreated {
		t.Fatalf("create blocking page status = %d, want 201", page.Code)
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/block.md/child"}, "application/json"), http.StatusConflict)
}

func TestTableUIRootGroupEditDeleteAndErrors(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	for _, test := range []struct {
		path, contains string
	}{
		{"/default/files", "Vehicle Service"},
		{"/default/data/vehicle", "Vehicle tables"},
		{"/default/data/vehicle/service", "Vehicle Service"},
	} {
		response := testRequest(t, a, http.MethodGet, test.path, nil, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.contains) {
			t.Fatalf("GET %s status=%d missing %q: %s", test.path, response.Code, test.contains, response.Body.String())
		}
	}
	record := createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil", "category": "service"})
	id := record["id"].(string)
	edit := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service/"+id+"/edit", nil, "")
	if edit.Code != http.StatusOK || !strings.Contains(edit.Body.String(), `value="Oil"`) {
		t.Fatalf("edit form status=%d did not prefill record: %s", edit.Code, edit.Body.String())
	}
	delete := testRequest(t, a, http.MethodPost, "/default/data/vehicle/service/"+id+"/delete", nil, "")
	if delete.Code != http.StatusSeeOther || delete.Header().Get("Location") != "/default/data/vehicle/service" {
		t.Fatalf("delete form status=%d location=%q", delete.Code, delete.Header().Get("Location"))
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/data/vehicle/service/"+id, nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/default/data/vehicle/service", nil, ""), http.StatusMethodNotAllowed)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/data/missing/table", nil, ""), http.StatusNotFound)
}

func TestTableUIGroupingAggregationFilteringAndPaging(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil", "category": "service", "amount": "120"})
	createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Brake", "category": "repair", "amount": "80"})
	createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Filter", "category": "service", "amount": "30"})

	grouped := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service?group_by=category&aggregate=amount%3Asum&limit=1", nil, "")
	if grouped.Code != http.StatusOK {
		t.Fatalf("grouped table view status = %d: %s", grouped.Code, grouped.Body.String())
	}
	for _, want := range []string{`name=group_by`, `value="category" selected`, `name=aggregate`, `value="amount:sum" selected`, "amount SUM", "1–1 of 2 groups", "Next"} {
		if !strings.Contains(grouped.Body.String(), want) {
			t.Errorf("grouped table view missing %q: %s", want, grouped.Body.String())
		}
	}
	if !strings.Contains(grouped.Body.String(), "80</td>") || strings.Contains(grouped.Body.String(), "120</td>") {
		t.Errorf("first group page has incorrect aggregate rows: %s", grouped.Body.String())
	}
	if !strings.Contains(grouped.Body.String(), "group_by=category") || !strings.Contains(grouped.Body.String(), "aggregate=amount%3Asum") {
		t.Errorf("group pagination link dropped query settings: %s", grouped.Body.String())
	}
	nextGroup := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service?group_by=category&aggregate=amount%3Asum&limit=1&offset=1", nil, "")
	if nextGroup.Code != http.StatusOK || !strings.Contains(nextGroup.Body.String(), "150</td>") || strings.Contains(nextGroup.Body.String(), "80</td>") {
		t.Fatalf("next grouped page did not advance: status=%d body=%s", nextGroup.Code, nextGroup.Body.String())
	}

	filtered := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service?filter.category=service&sort=amount&order=desc&limit=25", nil, "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), "Oil") || strings.Contains(filtered.Body.String(), "Brake</a>") {
		t.Fatalf("filtered/sorted table view did not return matching records: status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	if strings.Index(filtered.Body.String(), ">Oil</a>") > strings.Index(filtered.Body.String(), ">Filter</a>") {
		t.Fatalf("sort control did not retain descending numeric order: %s", filtered.Body.String())
	}
	summary := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service?aggregate=amount%3Asum", nil, "")
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(), "amount SUM") || !strings.Contains(summary.Body.String(), "230</td>") {
		t.Fatalf("aggregate summary was not rendered: status=%d body=%s", summary.Code, summary.Body.String())
	}
	blankFilters := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service?filter.name=&filter.category=&filter.amount=&group_by=&aggregate=&limit=100", nil, "")
	if blankFilters.Code != http.StatusOK || !strings.Contains(blankFilters.Body.String(), "Oil") {
		t.Fatalf("blank query controls should leave the record list unfiltered: status=%d body=%s", blankFilters.Code, blankFilters.Body.String())
	}
}

func TestTimeseriesTableUITimeRangeAndBuckets(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "measurements", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "label": "Measured at", "type": "datetime", "required": true},
		map[string]any{"name": "temperature", "label": "Temperature", "type": "decimal"},
	}})
	createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-04-01T12:00:00Z", "temperature": "20"})
	createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-04-02T12:00:00Z", "temperature": "24"})

	view := testRequest(t, a, http.MethodGet, "/default/data/pool/measurements?bucket=day&aggregate=temperature%3Aavg&from=2026-04-01T00%3A00%3A00Z&to=2026-04-02T00%3A00%3A00Z", nil, "")
	if view.Code != http.StatusOK {
		t.Fatalf("time-bucket table view status = %d: %s", view.Code, view.Body.String())
	}
	for _, want := range []string{`name=bucket`, `value="day" selected`, `name=from`, `name=to`, "Bucket start", "Temperature AVG", "2026-04-01T00:00:00Z", "20</td>"} {
		if !strings.Contains(view.Body.String(), want) {
			t.Errorf("time-bucket view missing %q: %s", want, view.Body.String())
		}
	}
	defaultAggregate := testRequest(t, a, http.MethodGet, "/default/data/pool/measurements?bucket=day", nil, "")
	if defaultAggregate.Code != http.StatusOK || !strings.Contains(defaultAggregate.Body.String(), "Count") {
		t.Fatalf("time bucket without aggregate should default to count: status=%d body=%s", defaultAggregate.Code, defaultAggregate.Body.String())
	}
	calendarGroup := testRequest(t, a, http.MethodGet, "/default/data/pool/measurements?group_by=week&aggregate=count", nil, "")
	if calendarGroup.Code != http.StatusOK || !strings.Contains(calendarGroup.Body.String(), `value="week" selected`) {
		t.Fatalf("timeseries calendar grouping was not rendered: status=%d body=%s", calendarGroup.Code, calendarGroup.Body.String())
	}
}

func makeZipWithEmptyFiles(t *testing.T, count int) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for i := 0; i < count; i++ {
		if _, err := writer.Create(fmt.Sprintf("entry-%05d.txt", i)); err != nil {
			t.Fatalf("create ZIP entry %d: %v", i, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	return archive.Bytes()
}

func TestDirectoryZipRejectsCompressedAndEntryCountLimits(t *testing.T) {
	t.Run("compressed size", func(t *testing.T) {
		a := newTestApp(t)
		body := bytes.Repeat([]byte("x"), int(zipLimit+1))
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", body, "application/zip")
		assertAPIError(t, response, http.StatusUnprocessableEntity)
	})
	t.Run("entry count", func(t *testing.T) {
		a := newTestApp(t)
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", makeZipWithEmptyFiles(t, 10001), "application/zip")
		assertAPIError(t, response, http.StatusUnprocessableEntity)
		entries, err := os.ReadDir(a.content)
		if err != nil || len(entries) != 0 {
			t.Fatalf("entry-count rejection published content: entries=%v err=%v", entries, err)
		}
	})
}

func TestDirectoryZipRejectsTotalExpandedSize(t *testing.T) {
	a := newTestApp(t)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	fileData := bytes.Repeat([]byte("z"), 16*1024*1024)
	for i := 0; i < 17; i++ {
		file, err := writer.Create(fmt.Sprintf("expanded-%02d.bin", i))
		if err != nil {
			t.Fatalf("create ZIP entry: %v", err)
		}
		if _, err := file.Write(fileData); err != nil {
			t.Fatalf("write ZIP entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", archive.Bytes(), "application/zip")
	assertAPIError(t, response, http.StatusUnprocessableEntity)
	for i := 0; i < 17; i++ {
		if _, err := os.Stat(filepath.Join(a.content, fmt.Sprintf("expanded-%02d.bin", i))); !os.IsNotExist(err) {
			t.Fatalf("total-size rejection published entry %d: %v", i, err)
		}
	}
}
