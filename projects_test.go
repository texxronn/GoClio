package main

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCreateListReadDelete(t *testing.T) {
	a := newTestApp(t)

	// The implicit default project always exists and is listed first.
	list := testRequest(t, a, http.MethodGet, "/api/v1/projects", nil, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list projects status = %d: %s", list.Code, list.Body.String())
	}
	var projects []map[string]any
	testJSON(t, list, &projects)
	if len(projects) != 1 || projects[0]["name"] != "default" {
		t.Fatalf("initial projects = %#v", projects)
	}
	if projects[0]["label"] != "Default" || projects[0]["url"] != "http://clio.test/default/" || projects[0]["api_url"] != "http://clio.test/api/v1/projects/default" {
		t.Errorf("default project representation = %#v", projects[0])
	}
	if projects[0]["created_at"] == "" {
		t.Errorf("default project created_at missing: %#v", projects[0])
	}

	// Create and read a project.
	create := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "Bills", "label": "Bills", "description": "Household bills", "order": 3}, "application/json")
	if create.Code != http.StatusCreated {
		t.Fatalf("create project status = %d: %s", create.Code, create.Body.String())
	}
	var created map[string]any
	testJSON(t, create, &created)
	if created["name"] != "bills" {
		t.Errorf("project name = %v, want lower-case bills", created["name"])
	}
	if created["label"] != "Bills" || created["description"] != "Household bills" || created["order"].(float64) != 3 {
		t.Errorf("project representation = %#v", created)
	}
	if created["url"] != "http://clio.test/bills/" || created["api_url"] != "http://clio.test/api/v1/projects/bills" {
		t.Errorf("project URLs = %#v", created)
	}

	read := testRequest(t, a, http.MethodGet, "/api/v1/projects/bills", nil, "")
	if read.Code != http.StatusOK {
		t.Fatalf("read project status = %d: %s", read.Code, read.Body.String())
	}
	var fetched map[string]any
	testJSON(t, read, &fetched)
	if fetched["name"] != "bills" || fetched["label"] != "Bills" {
		t.Errorf("read project = %#v", fetched)
	}

	// A duplicate name conflicts.
	duplicate := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json")
	assertAPIError(t, duplicate, http.StatusConflict)

	// A missing project is not found.
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/projects/missing", nil, ""), http.StatusNotFound)

	// Delete an empty project.
	remove := testRequest(t, a, http.MethodDelete, "/api/v1/projects/bills", nil, "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete project status = %d: %s", remove.Code, remove.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/projects/bills", nil, ""), http.StatusNotFound)
}

func TestProjectReservedNamesAndValidation(t *testing.T) {
	a := newTestApp(t)
	for _, name := range []string{"api", "health", "help", "projects", "assets", "favicon.svg", "default", "API"} {
		response := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": name}, "application/json")
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("create reserved project %q status = %d, want 422: %s", name, response.Code, response.Body.String())
		}
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{}, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bad name"}, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "ok", "label": 5}, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "ok", "order": "x"}, "application/json"), http.StatusUnprocessableEntity)

	// The default project cannot be deleted.
	remove := testRequest(t, a, http.MethodDelete, "/api/v1/projects/default", nil, "")
	if remove.Code != http.StatusUnprocessableEntity {
		t.Errorf("delete default status = %d, want 422: %s", remove.Code, remove.Body.String())
	}

	// Method restrictions.
	assertAPIError(t, testRequest(t, a, http.MethodPatch, "/api/v1/projects", nil, ""), http.StatusMethodNotAllowed)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/projects", nil, ""), http.StatusMethodNotAllowed)
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/projects/bills", nil, ""), http.StatusMethodNotAllowed)
}

func TestProjectDeleteRejectsNonEmpty(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project status = %d: %s", w.Code, w.Body.String())
	}
	// A project with a group is not empty. Scoped group creation arrives with
	// project-scoped routing, so seed the row directly.
	if _, err := a.db.Exec(`INSERT INTO groups_meta(project,name,label) VALUES('bills','utility','Utility')`); err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/projects/bills", nil, ""), http.StatusConflict)
}

func TestHealthReportsProjectCount(t *testing.T) {
	a := newTestApp(t)
	read := func() float64 {
		response := testRequest(t, a, http.MethodGet, "/api/v1/health", nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("health status = %d", response.Code)
		}
		var health map[string]any
		testJSON(t, response, &health)
		n, ok := health["projects"].(float64)
		if !ok {
			t.Fatalf("health projects missing: %#v", health)
		}
		return n
	}
	if got := read(); got != 1 {
		t.Errorf("initial project count = %v, want 1", got)
	}
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project status = %d: %s", w.Code, w.Body.String())
	}
	if got := read(); got != 2 {
		t.Errorf("project count after create = %v, want 2", got)
	}
}

func TestOpenDatabaseMigratesLegacyTablesToProjectScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open(querySQLiteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE groups_meta (name TEXT PRIMARY KEY, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE tables_meta (group_name TEXT NOT NULL, name TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, timestamp_field TEXT, indexes TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(group_name,name), FOREIGN KEY(group_name) REFERENCES groups_meta(name) ON DELETE CASCADE)`,
		`CREATE TABLE fields_meta (group_name TEXT NOT NULL, table_name TEXT NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL, definition TEXT NOT NULL, PRIMARY KEY(group_name,table_name,name), FOREIGN KEY(group_name,table_name) REFERENCES tables_meta(group_name,name) ON DELETE CASCADE)`,
		`CREATE TABLE records (id TEXT PRIMARY KEY, group_name TEXT NOT NULL, table_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, data TEXT NOT NULL, timestamp_value TEXT, FOREIGN KEY(group_name,table_name) REFERENCES tables_meta(group_name,name) ON DELETE CASCADE)`,
		`INSERT INTO groups_meta(name,label) VALUES('pool','Pool')`,
		`INSERT INTO tables_meta(group_name,name,label,kind) VALUES('pool','readings','Readings','record')`,
		`INSERT INTO fields_meta(group_name,table_name,name,position,definition) VALUES('pool','readings','value',0,'{"name":"value","type":"integer"}')`,
		`INSERT INTO records(id,group_name,table_name,created_at,updated_at,data) VALUES('r1','pool','readings','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{"value":1}')`,
	}
	for _, statement := range statements {
		if _, err = legacy.Exec(statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := openDatabase(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer db.Close()

	for table, want := range map[string]string{"groups_meta": "pool", "tables_meta": "readings", "fields_meta": "value", "records": "r1"} {
		column := "name"
		if table == "records" {
			column = "id"
		}
		var project, got string
		if err = db.QueryRow(`SELECT project,`+column+` FROM `+table).Scan(&project, &got); err != nil {
			t.Fatalf("read migrated %s: %v", table, err)
		}
		if project != "default" || got != want {
			t.Errorf("migrated %s = (%q,%q), want (default,%q)", table, project, got, want)
		}
	}

	// The default project now exists.
	var created string
	if err = db.QueryRow(`SELECT created_at FROM projects WHERE name='default'`).Scan(&created); err != nil {
		t.Fatalf("default project missing after migration: %v", err)
	}
	if created == "" {
		t.Error("default project created_at is empty")
	}

	// Group names are unique per project, not globally.
	if _, err = db.Exec(`INSERT INTO projects(name,label,created_at) VALUES('bills','Bills','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed second project: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO groups_meta(project,name,label) VALUES('bills','pool','Pool')`); err != nil {
		t.Fatalf("same group name in a second project should be allowed: %v", err)
	}
	// Reopening is a no-op and preserves the migrated rows.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openDatabase(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer reopened.Close()
	var count int
	if err = reopened.QueryRow(`SELECT count(*) FROM groups_meta`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("group count after reopen = %d, want 2", count)
	}
}

func TestBareRoutesRedirectToDefaultProject(t *testing.T) {
	a := newTestApp(t)
	for _, test := range []struct{ path, location string }{
		{"/", "/default/"},
		{"/api/v1", "/api/v1/default"},
		{"/default", "/default/data"},
	} {
		response := testRequest(t, a, http.MethodGet, test.path, nil, "")
		if response.Code != http.StatusFound || response.Header().Get("Location") != test.location {
			t.Errorf("GET %s = %d location=%q, want 302 %q", test.path, response.Code, response.Header().Get("Location"), test.location)
		}
	}
}

func TestUnknownProjectReturnsNotFound(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{
		"/api/v1/nope/data/groups",
		"/api/v1/nope/files/directories",
		"/nope/data",
		"/nope/files",
		"/nope/collections",
	} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, path, nil, ""), http.StatusNotFound)
	}
}

func TestProjectDataIsolation(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills", "label": "Bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}

	// The same group and table names in two projects must coexist.
	for _, project := range []string{"default", "bills"} {
		label := "Default Shared"
		if project == "bills" {
			label = "Bills Shared"
		}
		if w := testRequest(t, a, http.MethodPost, "/api/v1/"+project+"/data/groups", map[string]any{"name": "shared", "label": label}, "application/json"); w.Code != http.StatusCreated {
			t.Fatalf("create group in %s: %d %s", project, w.Code, w.Body.String())
		}
		table := map[string]any{"name": "items", "fields": []any{map[string]any{"name": "name", "type": "string"}}}
		if w := testRequest(t, a, http.MethodPost, "/api/v1/"+project+"/data/groups/shared/tables", table, "application/json"); w.Code != http.StatusCreated {
			t.Fatalf("create table in %s: %d %s", project, w.Code, w.Body.String())
		}
		record := map[string]any{"name": project}
		if w := testRequest(t, a, http.MethodPost, "/api/v1/"+project+"/data/groups/shared/tables/items/records", record, "application/json"); w.Code != http.StatusCreated {
			t.Fatalf("create record in %s: %d %s", project, w.Code, w.Body.String())
		}
	}

	// Each project sees only its own group.
	readGroups := func(project string) []map[string]any {
		response := testRequest(t, a, http.MethodGet, "/api/v1/"+project+"/data/groups", nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("list groups in %s: %d %s", project, response.Code, response.Body.String())
		}
		var groups []map[string]any
		testJSON(t, response, &groups)
		return groups
	}
	defaultGroups := readGroups("default")
	if len(defaultGroups) != 1 || defaultGroups[0]["label"] != "Default Shared" {
		t.Fatalf("default groups = %#v", defaultGroups)
	}
	billsGroups := readGroups("bills")
	if len(billsGroups) != 1 || billsGroups[0]["label"] != "Bills Shared" {
		t.Fatalf("bills groups = %#v", billsGroups)
	}

	// Each project sees only its own records, and IDs do not leak across.
	readRecords := func(project string) []map[string]any {
		response := testRequest(t, a, http.MethodGet, "/api/v1/"+project+"/data/groups/shared/tables/items/records", nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("list records in %s: %d %s", project, response.Code, response.Body.String())
		}
		var page map[string]any
		testJSON(t, response, &page)
		out := []map[string]any{}
		for _, item := range page["data"].([]any) {
			out = append(out, item.(map[string]any))
		}
		return out
	}
	defaultRecords := readRecords("default")
	if len(defaultRecords) != 1 || defaultRecords[0]["name"] != "default" {
		t.Fatalf("default records = %#v", defaultRecords)
	}
	billsRecords := readRecords("bills")
	if len(billsRecords) != 1 || billsRecords[0]["name"] != "bills" {
		t.Fatalf("bills records = %#v", billsRecords)
	}
	defaultID := defaultRecords[0]["id"].(string)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/bills/data/groups/shared/tables/items/records/"+defaultID, nil, ""), http.StatusNotFound)

	// A reference field may not target a record in another project.
	if w := testRequest(t, a, http.MethodPatch, "/api/v1/bills/data/groups/shared/tables/items", map[string]any{
		"fields": []any{
			map[string]any{"name": "name", "type": "string"},
			map[string]any{"name": "owner", "type": "reference", "group": "shared", "table": "items"},
		},
	}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("add reference field: %d %s", w.Code, w.Body.String())
	}
	cross := testRequest(t, a, http.MethodPost, "/api/v1/bills/data/groups/shared/tables/items/records", map[string]any{"name": "cross", "owner": defaultID}, "application/json")
	assertAPIError(t, cross, http.StatusUnprocessableEntity)
}

func TestProjectScopedHumanAndAPIRoutes(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/pool/tables", map[string]any{"name": "readings", "fields": []any{map[string]any{"name": "value", "type": "integer"}}}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create table: %d %s", w.Code, w.Body.String())
	}
	record := createTestRecord(t, a, "pool", "readings", map[string]any{"value": 1})
	id := record["id"].(string)

	// API metadata and table UI live under the data partition.
	for _, test := range []struct{ path, contains string }{
		{"/api/v1/default/data/metadata", `"api_version":"v1"`},
		{"/api/v1/default/data/groups/pool/tables/readings", `"name":"readings"`},
		{"/default/data", "Data"},
		{"/default/data/pool", "Pool tables"},
		{"/default/data/pool/readings", "pool · TABLE"},
	} {
		response := testRequest(t, a, http.MethodGet, test.path, nil, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.contains) {
			t.Errorf("GET %s status=%d missing %q: %s", test.path, response.Code, test.contains, response.Body.String())
		}
	}

	// The record page uses the project-scoped data URL and preserves it in links.
	recordPage := testRequest(t, a, http.MethodGet, "/default/data/pool/readings/"+id, nil, "")
	if recordPage.Code != http.StatusOK || !strings.Contains(recordPage.Body.String(), `href="/default/data/pool/readings/`+id+`/edit"`) {
		t.Fatalf("record page links not project-scoped: status=%d body=%s", recordPage.Code, recordPage.Body.String())
	}
}
