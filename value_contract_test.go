package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func postRecordRaw(t *testing.T, a *app, group, table, body string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodPost, "/api/v1/groups/"+group+"/tables/"+table+"/records", body, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create raw record status=%d body=%s", w.Code, w.Body.String())
	}
	var record map[string]any
	testJSON(t, w, &record)
	return record
}

func createRawTable(t *testing.T, a *app, group, body string) {
	t.Helper()
	w := testRequest(t, a, http.MethodPost, "/api/v1/groups/"+group+"/tables", body, "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create raw table status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDecimalCanonicalization(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "money")
	createRawTable(t, a, "money", `{"name":"prices","fields":[{"name":"price","type":"decimal"}]}`)
	for input, want := range map[string]string{
		"00120.4500": "120.45",
		"-0":         "0",
		"+5":         "5",
		"1e2":        "100",
		".5":         "0.5",
		"2.5e-1":     "0.25",
		"100":        "100",
	} {
		record := postRecordRaw(t, a, "money", "prices", `{"price":"`+input+`"}`)
		if got := record["price"]; got != want {
			t.Errorf("decimal %q canonicalized to %v, want %q", input, got, want)
		}
	}
}

func TestValueCoercionRejections(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "types")
	createRawTable(t, a, "types", `{"name":"values","fields":[
		{"name":"i","type":"integer"},
		{"name":"d","type":"decimal"},
		{"name":"b","type":"boolean"},
		{"name":"dt","type":"datetime"},
		{"name":"day","type":"date"},
		{"name":"u","type":"url"},
		{"name":"e","type":"enum","values":["a","b"]}
	]}`)
	base := "/api/v1/groups/types/tables/values/records"

	// Accepted coercions.
	record := postRecordRaw(t, a, "types", "values", `{"i":1.0,"d":"5","b":true,"dt":"2026-09-27T22:30:00+10:00","day":"2026-09-27","u":"https://example.com/x","e":"a"}`)
	if record["i"] != float64(1) || record["dt"] != "2026-09-27T12:30:00Z" || record["day"] != "2026-09-27" {
		t.Fatalf("unexpected accepted coercion result: %#v", record)
	}
	// Rejections.
	for _, body := range []string{
		`{"i":1.5}`,
		`{"i":"5"}`,
		`{"d":5}`,
		`{"b":"true"}`,
		`{"dt":"2026-09-27"}`,
		`{"day":"2026-02-29"}`,
		`{"day":"2026-13-01"}`,
		`{"u":"/relative"}`,
		`{"e":"c"}`,
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, base, body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestFieldNameRules(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "naming")
	// Mixed case is normalized to lowercase.
	createRawTable(t, a, "naming", `{"name":"mixed","fields":[{"name":"Label","type":"string"}]}`)
	metadata := testRequest(t, a, http.MethodGet, "/api/v1/metadata/groups/naming/tables/mixed", nil, "")
	var table map[string]any
	testJSON(t, metadata, &table)
	fields := table["fields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["name"] != "label" {
		t.Fatalf("field name was not normalized to lowercase: %#v", fields)
	}
	for _, body := range []string{
		`{"name":"reserved","fields":[{"name":"id","type":"string"}]}`,
		`{"name":"reserved","fields":[{"name":"created_at","type":"string"}]}`,
		`{"name":"reserved","fields":[{"name":"updated_at","type":"string"}]}`,
		`{"name":"spaced","fields":[{"name":"bad name","type":"string"}]}`,
		`{"name":"dotted","fields":[{"name":"a.b","type":"string"}]}`,
		`{"name":"dupe","fields":[{"name":"x","type":"string"},{"name":"X","type":"string"}]}`,
		`{"name":"empty","fields":[{"name":"","type":"string"}]}`,
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/naming/tables", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestValidationConstraints(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "rules")
	createRawTable(t, a, "rules", `{"name":"strings","fields":[
		{"name":"code","type":"string","min_length":2,"max_length":3,"pattern":"^[a-z]+$"}
	]}`)
	base := "/api/v1/groups/rules/tables/strings/records"
	if w := testRequest(t, a, http.MethodPost, base, `{"code":"ab"}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("valid string status=%d body=%s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"code":"a"}`, `{"code":"abcd"}`, `{"code":"ab1"}`} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, base, body, "application/json"), http.StatusUnprocessableEntity)
	}

	createRawTable(t, a, "rules", `{"name":"numbers","fields":[{"name":"n","type":"integer","min":1,"max":5}]}`)
	numBase := "/api/v1/groups/rules/tables/numbers/records"
	if w := testRequest(t, a, http.MethodPost, numBase, `{"n":3}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("valid integer status=%d body=%s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"n":0}`, `{"n":6}`} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, numBase, body, "application/json"), http.StatusUnprocessableEntity)
	}

	// Invalid definitions are rejected.
	for _, body := range []string{
		`{"name":"bad1","fields":[{"name":"x","type":"integer","min":5,"max":1}]}`,
		`{"name":"bad2","fields":[{"name":"x","type":"integer","min_length":2}]}`,
		`{"name":"bad3","fields":[{"name":"x","type":"string","pattern":"("}]}`,
		`{"name":"bad4","fields":[{"name":"x","type":"string","min_length":3,"max_length":1}]}`,
		`{"name":"bad5","fields":[{"name":"x","type":"integer","default":"5"}]}`,
		`{"name":"bad6","fields":[{"name":"x","type":"decimal","default":5}]}`,
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/rules/tables", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestRecordIdentifierRules(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "ids")
	createTestTable(t, a, "ids", map[string]any{"name": "left", "fields": []any{map[string]any{"name": "label", "type": "string"}}})
	createTestTable(t, a, "ids", map[string]any{"name": "right", "fields": []any{map[string]any{"name": "label", "type": "string"}}})
	left := createTestRecord(t, a, "ids", "left", map[string]any{"label": "a"})
	right := createTestRecord(t, a, "ids", "right", map[string]any{"label": "b"})
	if left["id"] == nil || right["id"] == nil || left["id"] == right["id"] {
		t.Fatalf("record IDs are not distinct and opaque: %#v %#v", left["id"], right["id"])
	}
	for _, body := range []string{`{"label":"a","id":"chosen"}`, `{"label":"a","created_at":"2026-01-01T00:00:00Z"}`, `{"label":"a","updated_at":"2026-01-01T00:00:00Z"}`} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/ids/tables/left/records", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestUniqueSemantics(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "uniq")
	createTestTable(t, a, "uniq", map[string]any{"name": "one", "fields": []any{
		map[string]any{"name": "code", "type": "string", "unique": true},
	}})
	createTestTable(t, a, "uniq", map[string]any{"name": "two", "fields": []any{
		map[string]any{"name": "code", "type": "string", "unique": true},
	}})
	createTestRecord(t, a, "uniq", "one", map[string]any{"code": "shared"})
	// A different table may reuse the same value; uniqueness is per table.
	if w := testRequest(t, a, http.MethodPost, "/api/v1/groups/uniq/tables/two/records", `{"code":"shared"}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("cross-table unique value status=%d body=%s", w.Code, w.Body.String())
	}
	// Duplicate non-null value in the same table is a conflict.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/uniq/tables/one/records", `{"code":"shared"}`, "application/json"), http.StatusConflict)
	// Multiple nulls are allowed because SQL treats nulls as distinct.
	for i := 0; i < 2; i++ {
		if w := testRequest(t, a, http.MethodPost, "/api/v1/groups/uniq/tables/one/records", `{"code":null}`, "application/json"); w.Code != http.StatusCreated {
			t.Errorf("null unique value %d status=%d body=%s", i, w.Code, w.Body.String())
		}
	}
	// Enabling unique over existing duplicates rolls back with a conflict.
	createTestTable(t, a, "uniq", map[string]any{"name": "dupes", "fields": []any{
		map[string]any{"name": "code", "type": "string"},
	}})
	createTestRecord(t, a, "uniq", "dupes", map[string]any{"code": "x"})
	createTestRecord(t, a, "uniq", "dupes", map[string]any{"code": "x"})
	enable := testRequest(t, a, http.MethodPatch, "/api/v1/groups/uniq/tables/dupes", `{"fields":[{"name":"code","type":"string","unique":true}]}`, "application/json")
	assertAPIError(t, enable, http.StatusConflict)
}

func TestDeclaredIndexesAreNonUniqueAndValidated(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "indexed")
	createTestTable(t, a, "indexed", map[string]any{"name": "items", "indexes": []any{
		map[string]any{"fields": []any{"code"}},
	}, "fields": []any{
		map[string]any{"name": "code", "type": "string"},
	}})
	// A declared index is not a uniqueness constraint.
	createTestRecord(t, a, "indexed", "items", map[string]any{"code": "dup"})
	if w := testRequest(t, a, http.MethodPost, "/api/v1/groups/indexed/tables/items/records", `{"code":"dup"}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("declared index rejected duplicate value: status=%d", w.Code)
	}
	metadata := testRequest(t, a, http.MethodGet, "/api/v1/groups/indexed/tables/items", nil, "")
	var table map[string]any
	testJSON(t, metadata, &table)
	if indexes, ok := table["indexes"].([]any); !ok || len(indexes) != 1 {
		t.Fatalf("declared indexes not returned in metadata: %#v", table["indexes"])
	}
	for _, body := range []string{
		`{"name":"badindex1","indexes":[{"fields":["missing"]}],"fields":[{"name":"code","type":"string"}]}`,
		`{"name":"badindex2","indexes":[{"fields":["code"]},{"fields":["code"]}],"fields":[{"name":"code","type":"string"}]}`,
		`{"name":"badindex3","indexes":"nope","fields":[{"name":"code","type":"string"}]}`,
	} {
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/indexed/tables", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

func TestReferenceRules(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "refs")
	createTestTable(t, a, "refs", map[string]any{"name": "parts", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	// A reference field requires group and table.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/refs/tables", `{"name":"bad","fields":[{"name":"target","type":"reference"}]}`, "application/json"), http.StatusUnprocessableEntity)
	// A default reference must resolve.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/refs/tables", `{"name":"bad2","fields":[{"name":"target","type":"reference","group":"refs","table":"parts","default":"missing"}]}`, "application/json"), http.StatusUnprocessableEntity)

	createTestTable(t, a, "refs", map[string]any{"name": "service", "fields": []any{
		map[string]any{"name": "name", "type": "string", "required": true},
		map[string]any{"name": "part", "type": "reference", "group": "refs", "table": "parts"},
	}})
	part := createTestRecord(t, a, "refs", "parts", map[string]any{"label": "Filter"})
	// Unknown target ID is rejected.
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/groups/refs/tables/service/records", `{"name":"x","part":"nope"}`, "application/json"), http.StatusUnprocessableEntity)
	createTestRecord(t, a, "refs", "service", map[string]any{"name": "Oil", "part": part["id"]})
	// A table with records cannot be deleted.
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/groups/refs/tables/parts", nil, ""), http.StatusConflict)
	// An empty table referenced by another table's field cannot be deleted either.
	createTestTable(t, a, "refs", map[string]any{"name": "spares", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	createTestTable(t, a, "refs", map[string]any{"name": "orders", "fields": []any{
		map[string]any{"name": "spare", "type": "reference", "group": "refs", "table": "spares"},
	}})
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/groups/refs/tables/spares", nil, ""), http.StatusConflict)

	// Cross-group references are permitted.
	createTestGroup(t, a, "other")
	createTestTable(t, a, "other", map[string]any{"name": "links", "fields": []any{
		map[string]any{"name": "part", "type": "reference", "group": "refs", "table": "parts"},
	}})
	if w := testRequest(t, a, http.MethodPost, "/api/v1/groups/other/tables/links/records", `{"part":"`+part["id"].(string)+`"}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("cross-group reference status=%d body=%s", w.Code, w.Body.String())
	}
}

// TestIndexReconciliationSurvivesReopen guards the startup index reconciliation
// path: reopening an existing database must leave table-scoped uniqueness and
// declared indexes intact.
func TestIndexReconciliationSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clio.db")
	content := filepath.Join(dir, "content")
	if err := os.MkdirAll(content, 0755); err != nil {
		t.Fatalf("create content dir: %v", err)
	}
	db, err := openDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	a := &app{db: db, content: content, baseURL: "http://clio.test"}
	createTestGroup(t, a, "uniq")
	createTestTable(t, a, "uniq", map[string]any{"name": "one", "fields": []any{
		map[string]any{"name": "code", "type": "string", "unique": true},
	}})
	createTestTable(t, a, "uniq", map[string]any{"name": "two", "fields": []any{
		map[string]any{"name": "code", "type": "string"},
	}})
	createTestRecord(t, a, "uniq", "one", map[string]any{"code": "a"})
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened, err := openDatabase(dbPath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	b := &app{db: reopened, content: content, baseURL: "http://clio.test"}
	if w := testRequest(t, b, http.MethodPost, "/api/v1/groups/uniq/tables/two/records", `{"code":"a"}`, "application/json"); w.Code != http.StatusCreated {
		t.Errorf("duplicate in unrelated table after reopen status=%d body=%s", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, b, http.MethodPost, "/api/v1/groups/uniq/tables/one/records", `{"code":"a"}`, "application/json"), http.StatusConflict)
}

func TestPatchSystemFieldRejected(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "patch")
	createTestTable(t, a, "patch", map[string]any{"name": "records", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	record := createTestRecord(t, a, "patch", "records", map[string]any{"label": "a"})
	for _, body := range []string{`{"id":"x"}`, `{"created_at":"2026-01-01T00:00:00Z"}`, `{"updated_at":"2026-01-01T00:00:00Z"}`} {
		assertAPIError(t, testRequest(t, a, http.MethodPatch, "/api/v1/groups/patch/tables/records/records/"+record["id"].(string), body, "application/json"), http.StatusUnprocessableEntity)
	}
}
