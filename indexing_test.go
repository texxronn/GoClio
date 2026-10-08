package main

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func TestAutomaticAndExplicitIndexesAreAppliedAndReconciled(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "fleet")
	createTestTable(t, a, "fleet", map[string]any{
		"name": "service",
		"fields": []any{
			map[string]any{"name": "serial", "type": "string", "required": true, "unique": true},
			map[string]any{"name": "category", "type": "string"},
			map[string]any{"name": "serviced_at", "type": "datetime", "required": true, "role": "timestamp"},
		},
		"indexes": []any{map[string]any{"fields": []any{"category", "serviced_at"}}},
	})

	first := createTestRecord(t, a, "fleet", "service", map[string]any{"serial": "A-1", "category": "repair", "serviced_at": "2026-03-01T00:00:00Z"})
	createTestRecord(t, a, "fleet", "service", map[string]any{"serial": "B-1", "category": "repair", "serviced_at": "2026-04-01T00:00:00Z"})
	duplicate := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/fleet/tables/service/records", map[string]any{"serial": "A-1", "category": "maintenance", "serviced_at": "2026-05-01T00:00:00Z"}, "application/json")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate unique value status = %d, want 409: %s", duplicate.Code, duplicate.Body.String())
	}

	listed := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/fleet/tables/service/records", nil, "")
	var result map[string]any
	testJSON(t, listed, &result)
	rows := result["data"].([]any)
	if rows[0].(map[string]any)["serviced_at"] != "2026-04-01T00:00:00Z" {
		t.Fatalf("temporal default ordering did not return latest first: %#v", rows)
	}

	var plan string
	rowsPlan, err := a.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM records WHERE project='default' AND group_name='fleet' AND table_name='service' AND json_extract(data, '$."serial"')='A-1'`)
	if err != nil {
		t.Fatal(err)
	}
	for rowsPlan.Next() {
		var id, parent, unused int
		var detail string
		if err = rowsPlan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	rowsPlan.Close()
	if !strings.Contains(plan, "clio_data_") {
		t.Fatalf("unique field query did not use managed index: %s", plan)
	}
	rowsPlan, err = a.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM records WHERE project='default' AND group_name='fleet' AND table_name='service' AND json_extract(data, '$."category"')='repair'`)
	if err != nil {
		t.Fatal(err)
	}
	plan = ""
	for rowsPlan.Next() {
		var id, parent, unused int
		var detail string
		if err = rowsPlan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	rowsPlan.Close()
	if !strings.Contains(plan, "clio_data_") {
		t.Fatalf("explicit compound index was not usable for its leading field: %s", plan)
	}

	metadata := testRequest(t, a, http.MethodGet, "/api/v1/default/data/groups/fleet/tables/service", nil, "")
	var table map[string]any
	testJSON(t, metadata, &table)
	if got := len(table["indexes"].([]any)); got != 1 {
		t.Fatalf("metadata indexes = %d, want explicit declaration", got)
	}

	updated := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/fleet/tables/service", map[string]any{"indexes": []any{}}, "application/json")
	if updated.Code != http.StatusOK {
		t.Fatalf("remove explicit index status = %d: %s", updated.Code, updated.Body.String())
	}
	var managed int
	if err = a.db.QueryRow(`SELECT count(*) FROM managed_indexes WHERE group_name='fleet' AND table_name='service'`).Scan(&managed); err != nil {
		t.Fatal(err)
	}
	if managed != 1 { // The unique serial index remains; temporal values use the shared timestamp index.
		t.Fatalf("managed indexes after reconciliation = %d, want 1", managed)
	}
	if first["id"] == "" {
		t.Fatal("created record has no id")
	}
}

func TestAddingUniqueConstraintWithDuplicatesRollsBackSchemaUpdate(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "lookup")
	createTestTable(t, a, "lookup", map[string]any{"name": "values", "fields": []any{map[string]any{"name": "code", "type": "string"}}})
	createTestRecord(t, a, "lookup", "values", map[string]any{"code": "same"})
	createTestRecord(t, a, "lookup", "values", map[string]any{"code": "same"})
	update := testRequest(t, a, http.MethodPatch, "/api/v1/default/data/groups/lookup/tables/values", map[string]any{"fields": []any{map[string]any{"name": "code", "type": "string", "unique": true}}}, "application/json")
	if update.Code != http.StatusConflict {
		t.Fatalf("unique schema update status = %d, want 409: %s", update.Code, update.Body.String())
	}

	var unique bool
	if err := a.db.QueryRow(`SELECT json_extract(definition, '$.unique') FROM fields_meta WHERE group_name='lookup' AND table_name='values' AND name='code'`).Scan(&unique); err != nil {
		t.Fatal(err)
	}
	if unique {
		t.Fatal("failed unique index update was partially committed")
	}
}

func TestOpenDatabaseAddsIndexMetadataToExistingSchema(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`CREATE TABLE tables_meta (group_name TEXT NOT NULL, name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, timestamp_field TEXT, PRIMARY KEY(group_name,name))`); err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var defaultValue sql.NullString
	if err = db.QueryRow(`SELECT dflt_value FROM pragma_table_info('tables_meta') WHERE name='indexes'`).Scan(&defaultValue); err != nil {
		t.Fatal(err)
	}
	if !defaultValue.Valid || strings.Trim(defaultValue.String, "'()") != "[]" {
		t.Fatalf("indexes migration default = %v", defaultValue)
	}
}
