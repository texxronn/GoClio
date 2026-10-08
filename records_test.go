package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testRequest(t *testing.T, a *app, method, path string, body any, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var input *bytes.Reader
	if body == nil {
		input = bytes.NewReader(nil)
	} else {
		var data []byte
		switch value := body.(type) {
		case []byte:
			data = value
		case string:
			data = []byte(value)
		default:
			var err error
			data, err = json.Marshal(value)
			if err != nil {
				t.Fatalf("encode request body: %v", err)
			}
		}
		input = bytes.NewReader(data)
	}
	r := httptest.NewRequest(method, path, input)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func testJSON(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode JSON response (status %d): %v; body=%s", w.Code, err, w.Body.String())
	}
}

func createTestGroup(t *testing.T, a *app, name string) {
	t.Helper()
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups", map[string]any{"name": name, "label": strings.Title(name)}, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create group status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func createTestTable(t *testing.T, a *app, group string, table map[string]any) {
	t.Helper()
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/"+group+"/tables", table, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create table status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func createTestRecord(t *testing.T, a *app, group, table string, input map[string]any) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodPost, fmt.Sprintf("/api/v1/default/data/groups/%s/tables/%s/records", group, table), input, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create record status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var record map[string]any
	testJSON(t, w, &record)
	return record
}

func recordFields() []any {
	return []any{
		map[string]any{"name": "name", "type": "string", "required": true, "min_length": 2},
		map[string]any{"name": "category", "type": "enum", "required": true, "values": []string{"service", "repair"}},
		map[string]any{"name": "amount", "type": "decimal"},
		map[string]any{"name": "enabled", "type": "boolean", "default": true},
		map[string]any{"name": "notes", "type": "text"},
		map[string]any{"name": "internal_code", "type": "string", "readonly": true},
	}
}

func setupRecordTable(t *testing.T, a *app) {
	t.Helper()
	createTestGroup(t, a, "vehicle")
	createTestTable(t, a, "vehicle", map[string]any{"name": "service", "label": "Vehicle Service", "fields": recordFields()})
}

func TestMetadataAndRecordCRUDValidation(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)

	metadata := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata/groups/vehicle/tables/service", nil, "")
	if metadata.Code != http.StatusOK {
		t.Fatalf("get table metadata status = %d, want %d", metadata.Code, http.StatusOK)
	}
	var table map[string]any
	testJSON(t, metadata, &table)
	if table["kind"] != "record" || table["url"] != "http://clio.test/default/data/vehicle/service" {
		t.Fatalf("unexpected table metadata: %#v", table)
	}
	fields, ok := table["fields"].([]any)
	if !ok || len(fields) != 6 {
		t.Fatalf("metadata fields = %#v, want six field definitions", table["fields"])
	}

	missingRequired := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/vehicle/tables/service/records", map[string]any{"category": "service"}, "application/json")
	if missingRequired.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing required field status = %d, want 422", missingRequired.Code)
	}
	unknownField := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/vehicle/tables/service/records", map[string]any{"name": "Oil", "category": "service", "extra": true}, "application/json")
	if unknownField.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d, want 422", unknownField.Code)
	}
	readonlyField := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/vehicle/tables/service/records", map[string]any{"name": "Oil", "category": "service", "internal_code": "x"}, "application/json")
	if readonlyField.Code != http.StatusUnprocessableEntity {
		t.Fatalf("readonly field status = %d, want 422", readonlyField.Code)
	}
	invalidEnum := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/vehicle/tables/service/records", map[string]any{"name": "Oil", "category": "other"}, "application/json")
	if invalidEnum.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid enum status = %d, want 422", invalidEnum.Code)
	}

	record := createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil", "category": "service", "amount": "1200.0300", "notes": "Annual"})
	if record["amount"] != "1200.03" || record["enabled"] != true || record["internal_code"] != nil {
		t.Fatalf("unexpected canonical/default/null field values: %#v", record)
	}
	if record["id"] == "" || record["created_at"] == nil || record["updated_at"] == nil {
		t.Fatalf("record lacks system fields: %#v", record)
	}

	patch := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service/records/"+record["id"].(string), map[string]any{"amount": nil}, "application/json")
	if patch.Code != http.StatusOK {
		t.Fatalf("patch record status = %d, want 200: %s", patch.Code, patch.Body.String())
	}
	var changed map[string]any
	testJSON(t, patch, &changed)
	if changed["name"] != "Oil" || changed["amount"] != nil || changed["notes"] != "Annual" {
		t.Fatalf("PATCH did not preserve omitted fields and clear explicit null: %#v", changed)
	}
	readonlyPatch := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service/records/"+record["id"].(string), map[string]any{"internal_code": "bad"}, "application/json")
	if readonlyPatch.Code != http.StatusUnprocessableEntity {
		t.Fatalf("readonly PATCH status = %d, want 422", readonlyPatch.Code)
	}
	clearRequired := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service/records/"+record["id"].(string), map[string]any{"name": nil}, "application/json")
	if clearRequired.Code != http.StatusUnprocessableEntity {
		t.Fatalf("clearing required field status = %d, want 422", clearRequired.Code)
	}

	remove := testRequest(t, a, http.MethodDelete, "/api/v1/default/data/groups/vehicle/tables/service/records/"+record["id"].(string), nil, "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete record status = %d, want 204", remove.Code)
	}
	missing := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/vehicle/tables/service/records/"+record["id"].(string), nil, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get deleted record status = %d, want 404", missing.Code)
	}
}

func TestTableSchemaUpdateProtectsStoredData(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)
	createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil", "category": "service", "notes": "keep"})

	fields := recordFields()
	fields = append(fields, map[string]any{"name": "source", "type": "string", "default": "imported"})
	update := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service", map[string]any{"fields": fields}, "application/json")
	if update.Code != http.StatusOK {
		t.Fatalf("add field status = %d, want 200: %s", update.Code, update.Body.String())
	}
	list := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/vehicle/tables/service/records", nil, "")
	var result map[string]any
	testJSON(t, list, &result)
	rows := result["data"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["source"] != "imported" {
		t.Fatalf("new field default was not applied to existing record: %#v", result)
	}

	typeChange := recordFields()
	for _, field := range typeChange {
		f := field.(map[string]any)
		if f["name"] == "name" {
			f["type"] = "text"
		}
	}
	changedType := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service", map[string]any{"fields": typeChange}, "application/json")
	if changedType.Code != http.StatusConflict {
		t.Fatalf("type change with records status = %d, want 409", changedType.Code)
	}

	removeOccupied := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service", map[string]any{"remove_fields": []any{"notes"}}, "application/json")
	if removeOccupied.Code != http.StatusConflict {
		t.Fatalf("removing populated field status = %d, want 409", removeOccupied.Code)
	}
}

func tableFieldNames(t *testing.T, table map[string]any) []string {
	t.Helper()
	raw, ok := table["fields"].([]any)
	if !ok {
		t.Fatalf("table fields is not an array: %#v", table["fields"])
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		f, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("field is not an object: %#v", item)
		}
		names = append(names, fmt.Sprint(f["name"]))
	}
	return names
}

// A table metadata PATCH merges supplied fields by name. A partial list adds
// the named field without dropping the existing schema, its record values or
// the rendered table columns.
func TestPatchTableFieldsMergesByName(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "home_utilities")
	createTestTable(t, a, "home_utilities", map[string]any{"name": "water_bills", "label": "Water Bills", "fields": []any{
		map[string]any{"name": "account", "type": "string", "required": true},
		map[string]any{"name": "amount", "type": "decimal"},
		map[string]any{"name": "due_date", "type": "date"},
	}})
	record := createTestRecord(t, a, "home_utilities", "water_bills", map[string]any{"account": "A-1", "amount": "42.50", "due_date": "2026-10-01"})

	update := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/home_utilities/tables/water_bills", map[string]any{"fields": []any{
		map[string]any{"name": "notes", "label": "Notes", "type": "text"},
	}}, "application/json")
	if update.Code != http.StatusOK {
		t.Fatalf("add field status = %d, want 200: %s", update.Code, update.Body.String())
	}
	var updated map[string]any
	testJSON(t, update, &updated)
	want := "account,amount,due_date,notes"
	if got := strings.Join(tableFieldNames(t, updated), ","); got != want {
		t.Fatalf("fields after partial PATCH = %q, want %q", got, want)
	}

	metadata := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata/groups/home_utilities/tables/water_bills", nil, "")
	var table map[string]any
	testJSON(t, metadata, &table)
	if got := strings.Join(tableFieldNames(t, table), ","); got != want {
		t.Fatalf("metadata fields = %q, want %q", got, want)
	}
	defs := make([]map[string]any, 0, len(table["fields"].([]any)))
	for _, item := range table["fields"].([]any) {
		defs = append(defs, item.(map[string]any))
	}
	account := fieldByName(defs, "account")
	if account == nil || account["type"] != "string" || account["required"] != true || account["label"] != "account" {
		t.Fatalf("existing field definition changed: %#v", account)
	}

	stored := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/home_utilities/tables/water_bills/records/"+record["id"].(string), nil, "")
	var storedRecord map[string]any
	testJSON(t, stored, &storedRecord)
	if storedRecord["account"] != "A-1" || storedRecord["amount"] != record["amount"] || storedRecord["due_date"] != "2026-10-01" || storedRecord["notes"] != nil {
		t.Fatalf("record values after partial schema PATCH: %#v", storedRecord)
	}

	view := testRequest(t, a, http.MethodGet, "/default/data/home_utilities/water_bills", nil, "")
	if view.Code != http.StatusOK {
		t.Fatalf("table view status = %d, want 200", view.Code)
	}
	for _, header := range []string{"account", "amount", "due_date", "Notes"} {
		if !strings.Contains(view.Body.String(), header) {
			t.Fatalf("table view is missing column %q", header)
		}
	}

	// Named fields are merged onto the existing definition rather than replaced.
	relabel := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/home_utilities/tables/water_bills", map[string]any{"fields": []any{
		map[string]any{"name": "account", "label": "Account number"},
	}}, "application/json")
	if relabel.Code != http.StatusOK {
		t.Fatalf("relabel field status = %d, want 200: %s", relabel.Code, relabel.Body.String())
	}
	var relabeled map[string]any
	testJSON(t, relabel, &relabeled)
	defs = defs[:0]
	for _, item := range relabeled["fields"].([]any) {
		defs = append(defs, item.(map[string]any))
	}
	account = fieldByName(defs, "account")
	if account["label"] != "Account number" || account["type"] != "string" || account["required"] != true {
		t.Fatalf("partial field update dropped existing properties: %#v", account)
	}
	if got := strings.Join(tableFieldNames(t, relabeled), ","); got != want {
		t.Fatalf("relabel changed field list to %q", got)
	}

	// An empty fields list is a no-op rather than a full replacement.
	noop := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/home_utilities/tables/water_bills", map[string]any{"fields": []any{}}, "application/json")
	if noop.Code != http.StatusOK {
		t.Fatalf("empty fields PATCH status = %d, want 200: %s", noop.Code, noop.Body.String())
	}
	var noopResult map[string]any
	testJSON(t, noop, &noopResult)
	if got := strings.Join(tableFieldNames(t, noopResult), ","); got != want {
		t.Fatalf("empty fields PATCH changed fields to %q", got)
	}

	// Removing a field is explicit and only allowed without stored values.
	remove := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/home_utilities/tables/water_bills", map[string]any{"remove_fields": []any{"notes"}}, "application/json")
	if remove.Code != http.StatusOK {
		t.Fatalf("remove unused field status = %d, want 200: %s", remove.Code, remove.Body.String())
	}
	var removed map[string]any
	testJSON(t, remove, &removed)
	if got := strings.Join(tableFieldNames(t, removed), ","); got != "account,amount,due_date" {
		t.Fatalf("fields after remove = %q", got)
	}

	conflicting := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/home_utilities/tables/water_bills", map[string]any{
		"fields":        []any{map[string]any{"name": "account", "type": "string"}},
		"remove_fields": []any{"account"},
	}, "application/json")
	if conflicting.Code != http.StatusUnprocessableEntity {
		t.Fatalf("adding and removing the same field status = %d, want 422", conflicting.Code)
	}
}

func TestReferencePreventsDeletingReferencedRecord(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "vehicle")
	createTestTable(t, a, "vehicle", map[string]any{"name": "parts", "fields": []any{map[string]any{"name": "label", "type": "string", "required": true}}})
	createTestTable(t, a, "vehicle", map[string]any{"name": "service", "fields": []any{
		map[string]any{"name": "name", "type": "string", "required": true},
		map[string]any{"name": "part", "type": "reference", "group": "vehicle", "table": "parts"},
	}})
	part := createTestRecord(t, a, "vehicle", "parts", map[string]any{"label": "Filter"})
	service := createTestRecord(t, a, "vehicle", "service", map[string]any{"name": "Oil change", "part": part["id"]})

	blocked := testRequest(t, a, http.MethodDelete, "/api/v1/default/data/groups/vehicle/tables/parts/records/"+part["id"].(string), nil, "")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("delete referenced record status = %d, want 409", blocked.Code)
	}
	clear := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/vehicle/tables/service/records/"+service["id"].(string), map[string]any{"part": nil}, "application/json")
	if clear.Code != http.StatusOK {
		t.Fatalf("clear reference status = %d, want 200", clear.Code)
	}
	deleted := testRequest(t, a, http.MethodDelete, "/api/v1/default/data/groups/vehicle/tables/parts/records/"+part["id"].(string), nil, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete unreferenced record status = %d, want 204", deleted.Code)
	}
}

func TestRecordQueriesFilteringSortingPagingDistinctAndAggregates(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "finance")
	createTestTable(t, a, "finance", map[string]any{"name": "costs", "fields": []any{
		map[string]any{"name": "category", "type": "enum", "required": true, "values": []string{"service", "repair"}},
		map[string]any{"name": "amount", "type": "decimal"},
	}})
	createTestRecord(t, a, "finance", "costs", map[string]any{"category": "service", "amount": "10.00"})
	createTestRecord(t, a, "finance", "costs", map[string]any{"category": "service", "amount": "20"})
	createTestRecord(t, a, "finance", "costs", map[string]any{"category": "repair", "amount": "5"})
	createTestRecord(t, a, "finance", "costs", map[string]any{"category": "repair"})

	filtered := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?filter.category=service&filter.amount.gte=10", nil, "")
	var filteredResult map[string]any
	testJSON(t, filtered, &filteredResult)
	filteredRows := filteredResult["data"].([]any)
	if len(filteredRows) != 2 {
		t.Fatalf("combined filters returned %d rows, want 2", len(filteredRows))
	}

	inQuery := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?filter.category.in=service&filter.category.in=repair", nil, "")
	var inResult map[string]any
	testJSON(t, inQuery, &inResult)
	if got := inResult["page"].(map[string]any)["total"]; got != float64(4) {
		t.Fatalf("repeated in total = %v, want 4", got)
	}

	nulls := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?filter.amount.isnull=true", nil, "")
	var nullResult map[string]any
	testJSON(t, nulls, &nullResult)
	if len(nullResult["data"].([]any)) != 1 {
		t.Fatalf("isnull filter returned %#v, want one row", nullResult["data"])
	}
	for _, test := range []struct {
		query string
		want  float64
	}{
		{"filter.amount.lt=10", 2}, // Null values retain the v1 comparison behavior.
		{"filter.amount.ne=10", 3},
	} {
		filtered := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?"+test.query, nil, "")
		var got map[string]any
		testJSON(t, filtered, &got)
		if total := got["page"].(map[string]any)["total"].(float64); total != test.want {
			t.Fatalf("%s total = %v, want %v", test.query, total, test.want)
		}
	}

	for _, order := range []string{"asc", "desc"} {
		ordered := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?sort=amount&order="+order, nil, "")
		var orderedResult map[string]any
		testJSON(t, ordered, &orderedResult)
		rows := orderedResult["data"].([]any)
		if rows[len(rows)-1].(map[string]any)["amount"] != nil {
			t.Fatalf("null did not sort last for %s: %#v", order, rows)
		}
	}

	page := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?sort=amount&order=asc&limit=1&offset=1", nil, "")
	var pageResult map[string]any
	testJSON(t, page, &pageResult)
	pageInfo := pageResult["page"].(map[string]any)
	if pageInfo["limit"] != float64(1) || pageInfo["offset"] != float64(1) || pageInfo["total"] != float64(4) || len(pageResult["data"].([]any)) != 1 {
		t.Fatalf("unexpected paging response: %#v", pageResult)
	}

	distinct := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?distinct=category", nil, "")
	var distinctResult map[string]any
	testJSON(t, distinct, &distinctResult)
	values := distinctResult["values"].([]any)
	if len(values) != 2 || values[0] != "repair" || values[1] != "service" {
		t.Fatalf("unexpected distinct values: %#v", values)
	}

	aggregate := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?aggregate=count,amount:sum,amount:avg", nil, "")
	var aggregateResult map[string]any
	testJSON(t, aggregate, &aggregateResult)
	aggs := aggregateResult["aggregate"].(map[string]any)
	if aggs["count"] != float64(4) || aggs["amount_sum"] != "35" || aggs["amount_avg"] != "11.6666666667" {
		t.Fatalf("unexpected aggregate result: %#v", aggs)
	}
	minMax := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?aggregate=amount:min,amount:max", nil, "")
	var minMaxResult map[string]any
	testJSON(t, minMax, &minMaxResult)
	if got := minMaxResult["aggregate"].(map[string]any); got["amount_min"] != "5" || got["amount_max"] != "20" {
		t.Fatalf("unexpected min/max result: %#v", got)
	}

	grouped := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/finance/tables/costs/records?group_by=category&aggregate=count,amount:sum", nil, "")
	var groupedResult map[string]any
	testJSON(t, grouped, &groupedResult)
	groups := groupedResult["groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("grouped aggregate returned %d groups, want 2", len(groups))
	}
	first := groups[0].(map[string]any)
	if first["category"] != "repair" || first["count"] != float64(2) || first["amount_sum"] != "5" {
		t.Fatalf("unexpected first aggregate group: %#v", first)
	}
}

func TestQueryPageKeepsCorrectTopRowsAndOrdering(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "ranking")
	createTestTable(t, a, "ranking", map[string]any{"name": "scores", "fields": []any{
		map[string]any{"name": "score", "type": "integer"},
	}})
	values := []int{3, 12, 1, 7, 12, 5}
	created := make([]map[string]any, len(values))
	for i, value := range values {
		created[i] = createTestRecord(t, a, "ranking", "scores", map[string]any{"score": value})
	}
	createTestRecord(t, a, "ranking", "scores", map[string]any{})

	response := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/ranking/tables/scores/records?sort=score&order=desc&limit=3&offset=1", nil, "")
	var result map[string]any
	testJSON(t, response, &result)
	rows := result["data"].([]any)
	if len(rows) != 3 || rows[0].(map[string]any)["score"] != float64(12) || rows[0].(map[string]any)["id"] != created[4]["id"] || rows[1].(map[string]any)["score"] != float64(7) || rows[2].(map[string]any)["score"] != float64(5) {
		t.Fatalf("unexpected descending page: %#v", rows)
	}
	page := result["page"].(map[string]any)
	if page["total"] != float64(7) {
		t.Fatalf("total = %v, want 7", page["total"])
	}

	ascending := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/ranking/tables/scores/records?sort=score&order=asc&limit=10", nil, "")
	var ascendingResult map[string]any
	testJSON(t, ascending, &ascendingResult)
	ascendingRows := ascendingResult["data"].([]any)
	if ascendingRows[0].(map[string]any)["score"] != float64(1) || ascendingRows[len(ascendingRows)-1].(map[string]any)["score"] != nil {
		t.Fatalf("null values should remain last in ascending sort: %#v", ascendingRows)
	}
}

func TestSQLQueriesPreserveExactNumericSemantics(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "exact")
	createTestTable(t, a, "exact", map[string]any{"name": "values", "fields": []any{
		map[string]any{"name": "score", "type": "integer"},
		map[string]any{"name": "price", "type": "decimal"},
	}})
	bodies := []string{
		`{"score":900719925474099312345678900,"price":"900719925474099312345678900.01"}`,
		`{"score":900719925474099312345678901,"price":"900719925474099312345678900.02"}`,
		`{"score":900719925474099312345678902,"price":"900719925474099312345678900.03"}`,
	}
	ids := make([]string, len(bodies))
	for i, body := range bodies {
		response := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/exact/tables/values/records", body, "application/json")
		if response.Code != http.StatusCreated {
			t.Fatalf("create exact-number record status = %d: %s", response.Code, response.Body.String())
		}
		var record map[string]any
		testJSON(t, response, &record)
		ids[i] = record["id"].(string)
	}

	ordered := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/exact/tables/values/records?sort=score&limit=3", nil, "")
	var orderedResult map[string]any
	testJSON(t, ordered, &orderedResult)
	rows := orderedResult["data"].([]any)
	for i, row := range rows {
		if row.(map[string]any)["id"] != ids[i] {
			t.Fatalf("exact integer sort order = %#v, want record %d first", rows, i)
		}
	}

	filtered := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/exact/tables/values/records?filter.score.gt=900719925474099312345678900", nil, "")
	var filteredResult map[string]any
	testJSON(t, filtered, &filteredResult)
	filteredRows := filteredResult["data"].([]any)
	gotIDs := map[string]bool{}
	for _, row := range filteredRows {
		gotIDs[row.(map[string]any)["id"].(string)] = true
	}
	if len(filteredRows) != 2 || !gotIDs[ids[1]] || !gotIDs[ids[2]] {
		t.Fatalf("exact integer filter returned %#v", filteredRows)
	}

	aggregate := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/exact/tables/values/records?aggregate=score:sum", nil, "")
	if aggregate.Code != http.StatusOK || !strings.Contains(aggregate.Body.String(), `"score_sum":2702159776422297937037036703`) {
		t.Fatalf("exact integer sum response = %d: %s", aggregate.Code, aggregate.Body.String())
	}

	decimalSort := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/exact/tables/values/records?sort=price&limit=3", nil, "")
	var decimalResult map[string]any
	testJSON(t, decimalSort, &decimalResult)
	decimalRows := decimalResult["data"].([]any)
	for i, row := range decimalRows {
		if row.(map[string]any)["id"] != ids[i] {
			t.Fatalf("exact decimal sort order = %#v, want record %d first", decimalRows, i)
		}
	}
}

func TestQuerySQLCollationsPreserveTypedOrdering(t *testing.T) {
	numbers := []string{"-100", "-2", "-1.21", "-1.2", "-0.02", "0", "0.001", "1.2", "1.21", "10"}
	for i := 1; i < len(numbers); i++ {
		if compareDecimalText(numbers[i-1], numbers[i]) >= 0 {
			t.Fatalf("numeric collation ordered %q before %q incorrectly", numbers[i-1], numbers[i])
		}
	}
	if compareDecimalText("1", "1.000") != 0 {
		t.Fatal("numeric collation did not equate numerically equal decimal values")
	}

	timestamps := []string{
		"2026-01-01T00:00:00Z",
		"2026-01-01T00:00:00.000000001Z",
		"2026-01-01T00:00:00.1Z",
		"2026-01-01T00:00:00.100000000Z",
		"2026-01-01T00:00:01Z",
	}
	for i := 1; i < len(timestamps); i++ {
		cmp := compareDateTimeText(timestamps[i-1], timestamps[i])
		if cmp > 0 || (i != 3 && cmp == 0) {
			t.Fatalf("datetime collation ordered %q before %q incorrectly (cmp=%d)", timestamps[i-1], timestamps[i], cmp)
		}
	}
}

func TestSQLQueryPagesBoundApplicationAllocations(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "bounded")
	createTestTable(t, a, "bounded", map[string]any{"name": "events", "fields": []any{
		map[string]any{"name": "category", "type": "string"},
		map[string]any{"name": "amount", "type": "integer"},
	}})

	const rowCount = 10_000
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO records(id,group_name,table_name,created_at,updated_at,data) VALUES(?, 'bounded','events',?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < rowCount; i++ {
		created := formatUTC(base.Add(time.Duration(i) * time.Second))
		data := fmt.Sprintf(`{"category":"group-%05d","amount":%d}`, i, i)
		if _, err = stmt.Exec(fmt.Sprintf("event-%05d", i), created, created, data); err != nil {
			t.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertBoundedAllocations := func(name, path string, wantTotal float64) {
		t.Helper()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		response := testRequest(t, a, http.MethodGet, path, nil, "")
		runtime.ReadMemStats(&after)
		if response.Code != http.StatusOK {
			t.Fatalf("%s query status = %d: %s", name, response.Code, response.Body.String())
		}
		allocated := after.TotalAlloc - before.TotalAlloc
		if allocated > 4<<20 {
			t.Fatalf("%s query allocated %d bytes for a bounded page over %d rows; want below 4 MiB", name, allocated, rowCount)
		}
		var result map[string]any
		testJSON(t, response, &result)
		total := result["page"].(map[string]any)["total"].(float64)
		if total != wantTotal {
			t.Fatalf("%s total = %v, want %v", name, total, wantTotal)
		}
		for _, key := range []string{"data", "values", "groups", "buckets"} {
			if rows, ok := result[key].([]any); ok {
				if len(rows) != min(2, int(wantTotal)) {
					t.Fatalf("%s returned %d rows, want %d", name, len(rows), min(2, int(wantTotal)))
				}
				return
			}
		}
		t.Fatalf("%s response has no bounded result page: %#v", name, result)
	}

	assertBoundedAllocations("grouped", "/api/v1/default/data/groups/bounded/tables/events/records?group_by=category&aggregate=count,amount:sum&limit=2", rowCount)
	assertBoundedAllocations("distinct", "/api/v1/default/data/groups/bounded/tables/events/records?distinct=category&limit=2", rowCount)
	assertBoundedAllocations("filtered page", "/api/v1/default/data/groups/bounded/tables/events/records?filter.category=group-09999&limit=2", 1)
	assertBoundedAllocations("contains filter", "/api/v1/default/data/groups/bounded/tables/events/records?filter.category.contains=GROUP-09999&limit=2", 1)

	createTestGroup(t, a, "bucketed")
	createTestTable(t, a, "bucketed", map[string]any{"name": "days", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "amount", "type": "integer"},
	}})
	tx, err = a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err = tx.Prepare(`INSERT INTO records(id,group_name,table_name,created_at,updated_at,data,timestamp_value) VALUES(?, 'bucketed','days',?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rowCount; i++ {
		timestamp := formatUTC(base.AddDate(0, 0, i))
		data, marshalErr := json.Marshal(map[string]any{"timestamp": timestamp, "amount": i})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = stmt.Exec(fmt.Sprintf("day-%05d", i), timestamp, timestamp, string(data), timestamp); err != nil {
			t.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertBoundedAllocations("bucket aggregate", "/api/v1/default/data/groups/bucketed/tables/days/records?bucket=day&aggregate=amount:sum&limit=2", rowCount)
}

func TestTimeseriesNormalizesUTCAndBucketsWeeks(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "measurements", "kind": "timeseries", "timestamp_field": "timestamp", "fields": []any{
		map[string]any{"name": "timestamp", "type": "datetime", "required": true},
		map[string]any{"name": "temperature", "type": "decimal"},
	}})

	first := createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-01-05T12:00:00+10:00", "temperature": "20"})
	createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-01-11T23:59:59Z", "temperature": "22"})
	createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-01-12T00:00:00Z", "temperature": "24"})
	createTestRecord(t, a, "pool", "measurements", map[string]any{"timestamp": "2026-02-01T00:30:00Z", "temperature": "26"})
	if first["timestamp"] != "2026-01-05T02:00:00Z" {
		t.Fatalf("timestamp not normalized to UTC: %v", first["timestamp"])
	}

	ranged := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/pool/tables/measurements/records?from=2026-01-05T02:00:00Z&to=2026-01-11T23:59:59Z", nil, "")
	var rangeResult map[string]any
	testJSON(t, ranged, &rangeResult)
	if got := rangeResult["page"].(map[string]any)["total"]; got != float64(1) {
		t.Fatalf("half-open time range total = %v, want 1 (the upper-bound record is excluded)", got)
	}

	bucketed := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/pool/tables/measurements/records?bucket=week&aggregate=count,temperature:avg", nil, "")
	var bucketResult map[string]any
	testJSON(t, bucketed, &bucketResult)
	buckets := bucketResult["buckets"].([]any)
	if len(buckets) != 3 {
		t.Fatalf("weekly buckets count = %d, want 3: %#v", len(buckets), buckets)
	}
	if buckets[0].(map[string]any)["bucket_start"] != "2026-01-05T00:00:00Z" || buckets[1].(map[string]any)["bucket_start"] != "2026-01-12T00:00:00Z" || buckets[2].(map[string]any)["bucket_start"] != "2026-01-26T00:00:00Z" {
		t.Fatalf("weeks are not bucketed from Monday UTC: %#v", buckets)
	}
	for unit, want := range map[string]int{"hour": 4, "day": 4, "month": 2} {
		response := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/pool/tables/measurements/records?bucket="+unit+"&aggregate=count", nil, "")
		var result map[string]any
		testJSON(t, response, &result)
		if got := len(result["buckets"].([]any)); got != want {
			t.Errorf("%s bucket count = %d, want %d: %#v", unit, got, want, result["buckets"])
		}
	}
}

func TestFilePageRoundTripAndPathSafety(t *testing.T) {
	a := newTestApp(t)
	created := testRequest(t, a, http.MethodPut, filesURL("default", "/reports/latest.md"), "# Report\n\n<script>alert(1)</script>", "text/markdown")
	if created.Code != http.StatusCreated {
		t.Fatalf("create page status = %d, want 201: %s", created.Code, created.Body.String())
	}
	var entry map[string]any
	testJSON(t, created, &entry)
	id, _ := entry["id"].(string)
	if entry["kind"] != "page" || entry["content_type"] != "text/markdown" {
		t.Fatalf("page entry = %#v", entry)
	}
	read := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id+"/content", nil, "")
	if read.Code != http.StatusOK || read.Body.String() != "# Report\n\n<script>alert(1)</script>" {
		t.Fatalf("stored page source = %d %q", read.Code, read.Body.String())
	}

	rendered := testRequest(t, a, http.MethodGet, "/default/files/reports/latest.md", nil, "")
	if rendered.Code != http.StatusOK || !strings.Contains(rendered.Body.String(), `class="published-markdown"`) || !strings.Contains(rendered.Body.String(), "&lt;script&gt;") || strings.Contains(rendered.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("Markdown page was not safely rendered: status=%d body=%s", rendered.Code, rendered.Body.String())
	}
	traversal := testRequest(t, a, http.MethodPut, filesURL("default", "/../escape.md"), "bad", "text/markdown")
	if traversal.Code != http.StatusUnprocessableEntity {
		t.Fatalf("page traversal status = %d, want 422", traversal.Code)
	}
	tooLarge := testRequest(t, a, http.MethodPut, filesURL("default", "/reports/large.md"), strings.Repeat("x", int(fileUploadLimit+1)), "text/markdown")
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status = %d, want 413", tooLarge.Code)
	}

	htmlPage := testRequest(t, a, http.MethodPut, filesURL("default", "/reports/trusted.html"), "<h1>Trusted</h1><script>run()</script>", "text/html")
	if htmlPage.Code != http.StatusCreated {
		t.Fatalf("create HTML page status = %d, want 201", htmlPage.Code)
	}
	htmlView := testRequest(t, a, http.MethodGet, "/default/files/reports/trusted.html", nil, "")
	if htmlView.Code != http.StatusOK || !strings.Contains(htmlView.Body.String(), "<script>run()</script>") {
		t.Fatalf("trusted HTML was not served as published: status=%d body=%s", htmlView.Code, htmlView.Body.String())
	}
}

func TestDirectoryZipUploadPreservesPathsAndRejectsTraversal(t *testing.T) {
	a := newTestApp(t)
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for name, content := range map[string]string{"weekly.md": "# Weekly", "measurements/latest.md": "# Latest"} {
		file, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	upload := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fpool%2Freports", buffer.Bytes(), "application/zip")
	if upload.Code != http.StatusCreated {
		t.Fatalf("directory upload status = %d, want 201: %s", upload.Code, upload.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "pool", "reports", "weekly.md")); err != nil {
		t.Fatalf("weekly page not published at destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "pool", "reports", "measurements", "latest.md")); err != nil {
		t.Fatalf("nested page not published at destination: %v", err)
	}

	makeSingleFileZip := func(contents string) []byte {
		t.Helper()
		var archive bytes.Buffer
		writer := zip.NewWriter(&archive)
		entry, err := writer.Create("weekly.md")
		if err != nil {
			t.Fatalf("create replacement ZIP entry: %v", err)
		}
		if _, err = entry.Write([]byte(contents)); err != nil {
			t.Fatalf("write replacement ZIP entry: %v", err)
		}
		if err = writer.Close(); err != nil {
			t.Fatalf("close replacement ZIP: %v", err)
		}
		return archive.Bytes()
	}
	conflict := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fpool%2Freports", makeSingleFileZip("replacement"), "application/zip")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("default ZIP overwrite status = %d, want 409", conflict.Code)
	}
	replaced := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fpool%2Freports&overwrite=true", makeSingleFileZip("# Replaced"), "application/zip")
	if replaced.Code != http.StatusCreated {
		t.Fatalf("explicit ZIP overwrite status = %d, want 201: %s", replaced.Code, replaced.Body.String())
	}
	weekly, err := os.ReadFile(filepath.Join(a.content, "default", "pool", "reports", "weekly.md"))
	if err != nil || string(weekly) != "# Replaced" {
		t.Fatalf("explicit overwrite content=%q err=%v", weekly, err)
	}

	var badBuffer bytes.Buffer
	badZip := zip.NewWriter(&badBuffer)
	badFile, err := badZip.Create("../escape.md")
	if err != nil {
		t.Fatalf("create traversal ZIP entry: %v", err)
	}
	_, _ = badFile.Write([]byte("bad"))
	_ = badZip.Close()
	rejected := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2Fpool%2Freports", badBuffer.Bytes(), "application/zip")
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("traversal upload status = %d, want 422", rejected.Code)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "pool", "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("traversal upload wrote outside destination: %v", err)
	}
}

func TestFilesDirectoryListAndDelete(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	if _, ae := a.createDirectory("/pool"); ae != nil {
		t.Fatalf("create test directory: %v", ae)
	}
	created := testRequest(t, a, http.MethodPut, filesURL("default", "/pool/note.md"), "note", "text/markdown")
	if created.Code != http.StatusCreated {
		t.Fatalf("create page status = %d, want 201", created.Code)
	}
	listing := testRequest(t, a, http.MethodGet, filesURL("default", "/pool"), nil, "")
	if listing.Code != http.StatusOK {
		t.Fatalf("list directory status = %d, want 200", listing.Code)
	}
	var directory map[string]any
	testJSON(t, listing, &directory)
	children := directory["children"].([]any)
	if len(children) != 1 || children[0].(map[string]any)["path"] != "/pool/note.md" {
		t.Fatalf("unexpected directory children: %#v", children)
	}
	deleted := testRequest(t, a, http.MethodDelete, filesURL("default", "/pool"), nil, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete directory status = %d, want 204", deleted.Code)
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "pool")); !os.IsNotExist(err) {
		t.Fatalf("directory still exists after delete: %v", err)
	}
}

func TestGenericTableFormsAndOperationalPages(t *testing.T) {
	a := newTestApp(t)
	setupRecordTable(t, a)

	listing := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service", nil, "")
	if listing.Code != http.StatusOK || !strings.Contains(listing.Body.String(), "/default/data/vehicle/service/new") {
		t.Fatalf("table view missing form link: status=%d", listing.Code)
	}
	newForm := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service/new", nil, "")
	if newForm.Code != http.StatusOK || !strings.Contains(newForm.Body.String(), `name="category"`) || !strings.Contains(newForm.Body.String(), `class="record-form-card"`) || !strings.Contains(newForm.Body.String(), "Save record") {
		t.Fatalf("new-record form missing fields: status=%d", newForm.Code)
	}
	form := url.Values{"name": {"Engine service"}, "category": {"service"}, "amount": {"45.50"}}
	created := testRequest(t, a, http.MethodPost, "/default/data/vehicle/service/new", form.Encode(), "application/x-www-form-urlencoded")
	if created.Code != http.StatusSeeOther || !strings.HasPrefix(created.Header().Get("Location"), "/default/data/vehicle/service/") {
		t.Fatalf("new-record form submission status=%d location=%q", created.Code, created.Header().Get("Location"))
	}
	id := strings.TrimPrefix(created.Header().Get("Location"), "/default/data/vehicle/service/")
	edit := testRequest(t, a, http.MethodPost, "/default/data/vehicle/service/"+id+"/edit", url.Values{"name": {"Engine service 2"}, "category": {"repair"}}.Encode(), "application/x-www-form-urlencoded")
	if edit.Code != http.StatusSeeOther {
		t.Fatalf("edit form submission status = %d, want 303", edit.Code)
	}
	recordPage := testRequest(t, a, http.MethodGet, "/default/data/vehicle/service/"+id, nil, "")
	if recordPage.Code != http.StatusOK || !strings.Contains(recordPage.Body.String(), "Engine service 2") || !strings.Contains(recordPage.Body.String(), `class="record-detail-page"`) || !strings.Contains(recordPage.Body.String(), "Edit record") {
		t.Fatalf("record page did not show patched value: status=%d", recordPage.Code)
	}

	for _, route := range []string{"/health", "/api/v1/health", "/help", "/api/v1/help", "/assets/clio-markdown.js"} {
		response := testRequest(t, a, http.MethodGet, route, nil, "")
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", route, response.Code)
		}
	}
}

func TestDirectoryZipRejectsExpandedFileLimit(t *testing.T) {
	a := newTestApp(t)
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	file, err := zw.Create("oversized.md")
	if err != nil {
		t.Fatalf("create ZIP entry: %v", err)
	}
	if _, err = file.Write(bytes.Repeat([]byte("x"), 16*1024*1024+1)); err != nil {
		t.Fatalf("write ZIP entry: %v", err)
	}
	if err = zw.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	response := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories?path=%2F", buffer.Bytes(), "application/zip")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expanded file limit status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.content, "default", "oversized.md")); !os.IsNotExist(err) {
		t.Fatalf("oversized file was published: %v", err)
	}

	var deepBuffer bytes.Buffer
	deepZip := zip.NewWriter(&deepBuffer)
	deepName := strings.Repeat("deep/", 33) + "page.md"
	deepFile, err := deepZip.Create(deepName)
	if err != nil {
		t.Fatalf("create too-deep ZIP entry: %v", err)
	}
	_, _ = deepFile.Write([]byte("deep"))
	_ = deepZip.Close()
	tooDeep := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", deepBuffer.Bytes(), "application/zip")
	if tooDeep.Code != http.StatusUnprocessableEntity {
		t.Fatalf("directory depth limit status = %d, want 422", tooDeep.Code)
	}
}
