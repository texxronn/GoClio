package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// This file covers two related features: the ?expand= query parameter on record
// reads (list and single) and the on_delete option on reference fields.

// --- expand fixtures ---------------------------------------------------------

// setupExpandTables builds a reference chain of depth three:
//
//	members -> households -> regions
//	members -> events -> households -> regions
func setupExpandTables(t *testing.T, a *app) {
	t.Helper()
	createTestGroup(t, a, "estate")
	createTestTable(t, a, "estate", map[string]any{"name": "regions", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
	}})
	createTestTable(t, a, "estate", map[string]any{"name": "households", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
		map[string]any{"name": "secret", "type": "text", "hidden": true},
		map[string]any{"name": "balance", "type": "decimal"},
		map[string]any{"name": "region", "type": "reference", "group": "estate", "table": "regions"},
	}})
	createTestTable(t, a, "estate", map[string]any{"name": "events", "fields": []any{
		map[string]any{"name": "title", "type": "string"},
		map[string]any{"name": "hidden_note", "type": "text", "hidden": true},
		map[string]any{"name": "household", "type": "reference", "group": "estate", "table": "households"},
	}})
	createTestTable(t, a, "estate", map[string]any{"name": "members", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
		map[string]any{"name": "household", "type": "reference", "group": "estate", "table": "households"},
		map[string]any{"name": "event", "type": "reference", "group": "estate", "table": "events"},
	}})
}

type expandFixture struct {
	region    map[string]any
	household map[string]any
	event     map[string]any
	member    map[string]any
}

func buildExpandFixture(t *testing.T, a *app) expandFixture {
	t.Helper()
	region := createTestRecord(t, a, "estate", "regions", map[string]any{"name": "north"})
	household := createTestRecord(t, a, "estate", "households", map[string]any{
		"name": "Smith", "secret": "sshh", "balance": "12.34", "region": region["id"],
	})
	event := createTestRecord(t, a, "estate", "events", map[string]any{
		"title": "Dinner", "hidden_note": "private", "household": household["id"],
	})
	member := createTestRecord(t, a, "estate", "members", map[string]any{
		"name": "Alice", "household": household["id"], "event": event["id"],
	})
	return expandFixture{region: region, household: household, event: event, member: member}
}

const (
	expandMembersBase = "/api/v1/default/data/groups/estate/tables/members/records"
)

func expandMemberURL(f expandFixture, id string) string {
	return expandMembersBase + "/" + id
}

// embeddedObject pulls one entry out of a record's "expanded" object.
func embeddedObject(t *testing.T, rec map[string]any, field string) map[string]any {
	t.Helper()
	expanded, ok := rec["expanded"].(map[string]any)
	if !ok {
		t.Fatalf("record has no expanded object: %#v", rec)
	}
	embedded, ok := expanded[field].(map[string]any)
	if !ok {
		t.Fatalf("expanded.%s missing from %#v", field, expanded)
	}
	return embedded
}

func TestExpandListAndSingleRead(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)

	rows := queryRows(t, a, expandMembersBase+"?expand=household")
	if len(rows) != 1 {
		t.Fatalf("expanded list returned %d rows, want 1", len(rows))
	}
	assertExpandedHousehold(t, rows[0].(map[string]any), f)

	single := queryResult(t, a, expandMemberURL(f, f.member["id"].(string))+"?expand=household")
	assertExpandedHousehold(t, single, f)
}

func assertExpandedHousehold(t *testing.T, rec map[string]any, f expandFixture) {
	t.Helper()
	// The reference field keeps its target ID string.
	if got := rec["household"]; got != f.household["id"] {
		t.Fatalf("reference field = %#v, want unchanged target id %v", got, f.household["id"])
	}
	embedded := embeddedObject(t, rec, "household")
	if embedded["id"] != f.household["id"] {
		t.Fatalf("embedded household id = %v, want %v", embedded["id"], f.household["id"])
	}
	if embedded["name"] != "Smith" {
		t.Fatalf("embedded household name = %v, want Smith", embedded["name"])
	}
	// Embedded records carry metadata and every defined target field.
	for _, key := range []string{"id", "created_at", "updated_at", "name", "balance", "region"} {
		if _, ok := embedded[key]; !ok {
			t.Fatalf("embedded household missing %q: %#v", key, embedded)
		}
	}
	// Hidden target fields are omitted.
	if _, ok := embedded["secret"]; ok {
		t.Fatalf("embedded household included hidden field secret: %#v", embedded)
	}
	// Decimal stays a canonical string by default.
	if embedded["balance"] != "12.34" {
		t.Fatalf("embedded decimal = %#v, want string 12.34", embedded["balance"])
	}
}

func TestExpandHiddenTargetFieldExcluded(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)

	rec := queryResult(t, a, expandMemberURL(f, f.member["id"].(string))+"?expand=event")
	embedded := embeddedObject(t, rec, "event")
	if _, ok := embedded["hidden_note"]; ok {
		t.Fatalf("embedded event included hidden field hidden_note: %#v", embedded)
	}
	if embedded["title"] != "Dinner" {
		t.Fatalf("embedded event title = %v, want Dinner", embedded["title"])
	}
	if embedded["household"] != f.household["id"] {
		t.Fatalf("embedded event household = %v, want %v", embedded["household"], f.household["id"])
	}
}

func TestExpandNullReferenceOmitted(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)
	blank := createTestRecord(t, a, "estate", "members", map[string]any{"name": "Bob"})

	// A null reference keeps its (null) value and adds no expanded entry.
	rec := queryResult(t, a, expandMemberURL(f, blank["id"].(string))+"?expand=household")
	if got, ok := rec["household"]; !ok || got != nil {
		t.Fatalf("null reference = %#v (present=%v), want explicit null", got, ok)
	}
	if _, ok := rec["expanded"]; ok {
		t.Fatalf("null reference produced an expanded object: %#v", rec)
	}

	// On a list the resolved row is expanded and the null row is not.
	rows := queryRows(t, a, expandMembersBase+"?expand=household")
	if len(rows) != 2 {
		t.Fatalf("expanded list returned %d rows, want 2", len(rows))
	}
	for _, raw := range rows {
		rec := raw.(map[string]any)
		_, hasExpanded := rec["expanded"]
		switch rec["name"] {
		case "Bob":
			if hasExpanded {
				t.Fatalf("null-reference row unexpectedly expanded: %#v", rec)
			}
		case "Alice":
			if !hasExpanded {
				t.Fatalf("resolved-reference row missing expanded: %#v", rec)
			}
		default:
			t.Fatalf("unexpected member row: %#v", rec)
		}
	}
}

func TestExpandValidationErrors(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)
	single := expandMemberURL(f, f.member["id"].(string))

	queries := []string{
		"expand=",                            // empty value
		"expand=household,",                  // trailing comma / empty path
		"expand=,household",                  // leading comma
		"expand=.household",                  // leading empty segment
		"expand=household..region",           // interior empty segment
		"expand=nope",                        // unknown field
		"expand=name",                        // non-reference field
		"expand=event.nope",                  // unknown nested field
		"expand=event.title",                 // non-reference nested field
		"expand=event.household.region.name", // depth four
		"expand=household,nope",              // one valid, one invalid
		"expand=event.household.region,zzz",  // invalid after a valid deep path
	}
	for _, query := range queries {
		assertAPIError(t, testRequest(t, a, http.MethodGet, expandMembersBase+"?"+query, nil, ""), http.StatusUnprocessableEntity)
		assertAPIError(t, testRequest(t, a, http.MethodGet, single+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}
}

func TestExpandDepthTwoAndThree(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)
	single := expandMemberURL(f, f.member["id"].(string))

	// Depth two: root.expanded.event, and the event nests its own household.
	rec := queryResult(t, a, single+"?expand=event.household")
	event := embeddedObject(t, rec, "event")
	if event["id"] != f.event["id"] {
		t.Fatalf("depth-two event id = %v, want %v", event["id"], f.event["id"])
	}
	if event["household"] != f.household["id"] {
		t.Fatalf("nested reference = %v, want target id %v", event["household"], f.household["id"])
	}
	nested := embeddedObject(t, event, "household")
	if nested["id"] != f.household["id"] {
		t.Fatalf("depth-two household id = %v, want %v", nested["id"], f.household["id"])
	}
	if _, ok := nested["region"]; !ok {
		t.Fatalf("depth-two household missing region field: %#v", nested)
	}

	// Depth three: the chain nests another level.
	rec3 := queryResult(t, a, single+"?expand=event.household.region")
	event3 := embeddedObject(t, rec3, "event")
	household3 := embeddedObject(t, event3, "household")
	region3 := embeddedObject(t, household3, "region")
	if region3["id"] != f.region["id"] {
		t.Fatalf("depth-three region id = %v, want %v", region3["id"], f.region["id"])
	}
	if region3["name"] != "north" {
		t.Fatalf("depth-three region name = %v, want north", region3["name"])
	}
}

func TestExpandDecimalFormatInEmbedded(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	f := buildExpandFixture(t, a)
	single := expandMemberURL(f, f.member["id"].(string))

	asString := embeddedObject(t, queryResult(t, a, single+"?expand=household"), "household")["balance"]
	if asString != "12.34" {
		t.Fatalf("default embedded decimal = %#v, want string 12.34", asString)
	}

	asNumber := embeddedObject(t, queryResult(t, a, single+"?expand=household&decimal_format=number"), "household")["balance"]
	if _, ok := asNumber.(string); ok {
		t.Fatalf("decimal_format=number left embedded decimal as a string: %#v", asNumber)
	}
	if asNumber != float64(12.34) {
		t.Fatalf("embedded numeric decimal = %#v, want JSON number 12.34", asNumber)
	}
}

func TestExpandPageUnchanged(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	buildExpandFixture(t, a)

	plain := queryResult(t, a, expandMembersBase+"?limit=1000")
	expanded := queryResult(t, a, expandMembersBase+"?limit=1000&expand=household")
	if !reflect.DeepEqual(plain["page"], expanded["page"]) {
		t.Fatalf("page changed with expand: without=%#v with=%#v", plain["page"], expanded["page"])
	}
	// The reference field still renders its ID, only an extra key is added.
	row := expanded["data"].([]any)[0].(map[string]any)
	if _, ok := row["expanded"]; !ok {
		t.Fatalf("expanded list row missing expanded object: %#v", row)
	}
}

// TestExpandBudgetExceeded verifies the 1000-record budget boundary: exactly
// 1000 distinct embedded records succeed, and one more returns 422.
func TestExpandBudgetExceeded(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "budget")
	createTestTable(t, a, "budget", map[string]any{"name": "targets", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
	}})
	createTestTable(t, a, "budget", map[string]any{"name": "parents", "fields": []any{
		map[string]any{"name": "left", "type": "reference", "group": "budget", "table": "targets"},
		map[string]any{"name": "right", "type": "reference", "group": "budget", "table": "targets"},
	}})

	const targets = 1001
	ids := make([]any, targets)
	for i := 0; i < targets; i++ {
		rec := createTestRecord(t, a, "budget", "targets", map[string]any{"name": fmt.Sprintf("t%04d", i)})
		ids[i] = rec["id"]
	}
	base := "/api/v1/default/data/groups/budget/tables/parents/records"
	// 500 parents referencing 1000 distinct targets: exactly the budget.
	for i := 0; i < 500; i++ {
		createTestRecord(t, a, "budget", "parents", map[string]any{"left": ids[i], "right": ids[i+500]})
	}
	if w := testRequest(t, a, http.MethodGet, base+"?limit=1000&expand=left,right", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("budget boundary status = %d, want 200: %s", w.Code, w.Body.String())
	}
	// One more distinct target exceeds the budget.
	createTestRecord(t, a, "budget", "parents", map[string]any{"left": ids[1000], "right": ids[0]})
	w := testRequest(t, a, http.MethodGet, base+"?limit=1000&expand=left,right", nil, "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("over-budget expand status = %d, want 422: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	testJSON(t, w, &body)
	if msg := fmt.Sprint(body["message"]); !strings.Contains(msg, "expand exceeds") {
		t.Fatalf("budget error message = %q, want it to mention expand exceeds", msg)
	}
}

// insertRawMember bypasses reference validation to model a missing target.
func insertRawMember(t *testing.T, a *app, id, household string) {
	t.Helper()
	data := fmt.Sprintf(`{"name":"ghost","household":%q,"event":null}`, household)
	if _, err := a.db.Exec(`INSERT INTO records(id,project,group_name,table_name,created_at,updated_at,data) VALUES(?,?,?,?,?,?,?)`,
		id, "default", "estate", "members", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", data); err != nil {
		t.Fatalf("insert raw member: %v", err)
	}
}

func TestExpandMissingTargetOmitted(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	insertRawMember(t, a, id, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	rec := queryResult(t, a, expandMembersBase+"/"+id+"?expand=household")
	if rec["household"] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("missing target id = %#v, want the unchanged id", rec["household"])
	}
	if _, ok := rec["expanded"]; ok {
		t.Fatalf("missing target produced an expanded object: %#v", rec)
	}
}

// TestExpandBudgetIgnoresMissingTargets confirms the budget counts records that
// are actually embedded, not every requested ID.
func TestExpandBudgetIgnoresMissingTargets(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	for i := 0; i < 1001; i++ {
		insertRawMember(t, a, fmt.Sprintf("%032x", i), fmt.Sprintf("%032x", i+1))
	}
	if w := testRequest(t, a, http.MethodGet, expandMembersBase+"?limit=1000&expand=household", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("missing-target expand status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestExpandNonPageQueryRejected(t *testing.T) {
	a := newTestApp(t)
	setupExpandTables(t, a)
	buildExpandFixture(t, a)
	for _, query := range []string{
		"expand=household&distinct=name",
		"expand=household&aggregate=count",
		"expand=household&group_by=name&aggregate=count",
	} {
		assertAPIError(t, testRequest(t, a, http.MethodGet, expandMembersBase+"?"+query, nil, ""), http.StatusUnprocessableEntity)
	}
}

func TestExpandReservedFieldNameRejected(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "resv")
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/resv/tables", map[string]any{
		"name":   "t",
		"fields": []any{map[string]any{"name": "expanded", "type": "string"}},
	}, "application/json")
	assertAPIError(t, w, http.StatusUnprocessableEntity)
}

// --- on_delete on reference fields -------------------------------------------

func setupOnDeleteParents(t *testing.T, a *app) {
	t.Helper()
	createTestGroup(t, a, "od")
	createTestTable(t, a, "od", map[string]any{"name": "parents", "fields": []any{
		map[string]any{"name": "name", "type": "string"},
	}})
}

// createOnDeleteTable creates an "od" table with a string label and one
// reference field named parent, using the supplied reference definition.
func createOnDeleteTable(t *testing.T, a *app, table string, ref map[string]any) {
	t.Helper()
	createTestTable(t, a, "od", map[string]any{"name": table, "fields": []any{
		map[string]any{"name": "label", "type": "string"},
		ref,
	}})
}

func onDeleteReference() map[string]any {
	return map[string]any{"name": "parent", "type": "reference", "group": "od", "table": "parents"}
}

func tableFieldOnDelete(t *testing.T, table map[string]any, name string) string {
	t.Helper()
	fields, ok := table["fields"].([]any)
	if !ok {
		t.Fatalf("table metadata has no fields array: %#v", table)
	}
	for _, raw := range fields {
		f, _ := raw.(map[string]any)
		if f["name"] == name {
			value, _ := f["on_delete"].(string)
			return value
		}
	}
	t.Fatalf("field %q not found in metadata: %#v", name, fields)
	return ""
}

func deleteRecordPath(group, table, id string) string {
	return fmt.Sprintf("/api/v1/default/data/groups/%s/tables/%s/records/%s", group, table, id)
}

func TestOnDeleteSetNull(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	ref := onDeleteReference()
	ref["on_delete"] = "set_null"
	createOnDeleteTable(t, a, "children", ref)

	parent := createTestRecord(t, a, "od", "parents", map[string]any{"name": "p"})
	first := createTestRecord(t, a, "od", "children", map[string]any{"label": "c1", "parent": parent["id"]})
	second := createTestRecord(t, a, "od", "children", map[string]any{"label": "c2", "parent": parent["id"]})

	deleted := testRequest(t, a, http.MethodDelete, deleteRecordPath("od", "parents", parent["id"].(string)), nil, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("set_null parent delete status = %d, want 204: %s", deleted.Code, deleted.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, deleteRecordPath("od", "parents", parent["id"].(string)), nil, ""), http.StatusNotFound)

	for _, child := range []map[string]any{first, second} {
		got := queryResult(t, a, deleteRecordPath("od", "children", child["id"].(string)))
		if value, ok := got["parent"]; !ok || value != nil {
			t.Fatalf("child %v parent = %#v (present=%v), want explicit null", child["label"], value, ok)
		}
		if got["updated_at"] == child["updated_at"] {
			t.Fatalf("child %v updated_at was not refreshed: %v", child["label"], got["updated_at"])
		}
	}
}

func TestOnDeleteRestrictDefault(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	createOnDeleteTable(t, a, "kids", onDeleteReference())

	parent := createTestRecord(t, a, "od", "parents", map[string]any{"name": "p"})
	kid := createTestRecord(t, a, "od", "kids", map[string]any{"label": "k", "parent": parent["id"]})

	blocked := testRequest(t, a, http.MethodDelete, deleteRecordPath("od", "parents", parent["id"].(string)), nil, "")
	assertAPIError(t, blocked, http.StatusConflict)

	// Parent and reference survive the blocked delete.
	if got := queryResult(t, a, deleteRecordPath("od", "parents", parent["id"].(string))); got["id"] != parent["id"] {
		t.Fatalf("parent missing after blocked delete: %#v", got)
	}
	if got := queryResult(t, a, deleteRecordPath("od", "kids", kid["id"].(string))); got["parent"] != parent["id"] {
		t.Fatalf("child reference changed after blocked delete: %#v", got)
	}
}

func TestOnDeleteSetNullOnRequiredRejected(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)

	ref := onDeleteReference()
	ref["on_delete"] = "set_null"
	ref["required"] = true
	w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/od/tables", map[string]any{
		"name":   "bad",
		"fields": []any{ref},
	}, "application/json")
	assertAPIError(t, w, http.StatusUnprocessableEntity)
}

func TestOnDeleteInvalidValueRejected(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)

	for i, value := range []any{"cascade", "SET_NULL", "set null", 5, 2.5, true} {
		ref := onDeleteReference()
		ref["on_delete"] = value
		w := testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/od/tables", map[string]any{
			"name":   fmt.Sprintf("bad%d", i),
			"fields": []any{ref},
		}, "application/json")
		assertAPIError(t, w, http.StatusUnprocessableEntity)
	}
}

func TestOnDeleteMetadataDefaultRestrict(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	createOnDeleteTable(t, a, "refs", onDeleteReference())

	metadata := queryResult(t, a, "/api/v1/default/data/metadata/groups/od/tables/refs")
	if got := tableFieldOnDelete(t, metadata, "parent"); got != "restrict" {
		t.Fatalf("metadata on_delete = %q, want restrict", got)
	}
	// The non-metadata table endpoint returns the same field definitions.
	plain := queryResult(t, a, "/api/v1/default/data/groups/od/tables/refs")
	if got := tableFieldOnDelete(t, plain, "parent"); got != "restrict" {
		t.Fatalf("table endpoint on_delete = %q, want restrict", got)
	}
}

func TestOnDeletePatchPreservesAndUpdates(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	ref := onDeleteReference()
	ref["on_delete"] = "set_null"
	createOnDeleteTable(t, a, "refs", ref)

	tablePath := "/api/v1/default/data/groups/od/tables/refs"
	metadataPath := "/api/v1/default/data/metadata/groups/od/tables/refs"
	if got := tableFieldOnDelete(t, queryResult(t, a, metadataPath), "parent"); got != "set_null" {
		t.Fatalf("initial on_delete = %q, want set_null", got)
	}

	// A partial definition without on_delete preserves the stored value.
	preserve := testRequest(t, a, http.MethodPatch, tablePath, map[string]any{
		"fields": []any{map[string]any{"name": "parent", "type": "reference", "group": "od", "table": "parents"}},
	}, "application/json")
	if preserve.Code != http.StatusOK {
		t.Fatalf("partial patch status = %d, want 200: %s", preserve.Code, preserve.Body.String())
	}
	if got := tableFieldOnDelete(t, queryResult(t, a, metadataPath), "parent"); got != "set_null" {
		t.Fatalf("on_delete after partial patch = %q, want preserved set_null", got)
	}

	// Supplying a new value updates it.
	update := testRequest(t, a, http.MethodPatch, tablePath, map[string]any{
		"fields": []any{map[string]any{"name": "parent", "type": "reference", "group": "od", "table": "parents", "on_delete": "restrict"}},
	}, "application/json")
	if update.Code != http.StatusOK {
		t.Fatalf("update patch status = %d, want 200: %s", update.Code, update.Body.String())
	}
	if got := tableFieldOnDelete(t, queryResult(t, a, metadataPath), "parent"); got != "restrict" {
		t.Fatalf("on_delete after update patch = %q, want restrict", got)
	}

	// An invalid value is still rejected on patch.
	invalid := testRequest(t, a, http.MethodPatch, tablePath, map[string]any{
		"fields": []any{map[string]any{"name": "parent", "type": "reference", "group": "od", "table": "parents", "on_delete": "cascade"}},
	}, "application/json")
	assertAPIError(t, invalid, http.StatusUnprocessableEntity)
}

func TestOnDeleteTableDeleteStillRestricted(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	ref := onDeleteReference()
	ref["on_delete"] = "set_null"
	createOnDeleteTable(t, a, "children", ref)

	// Table deletion ignores set_null and stays restrict-only.
	w := testRequest(t, a, http.MethodDelete, "/api/v1/default/data/groups/od/tables/parents", nil, "")
	assertAPIError(t, w, http.StatusConflict)
}

// TestOnDeleteLegacyDefinitionDefaultsToRestrict covers reference fields stored
// before the on_delete property existed: an absent property means restrict.
func TestOnDeleteLegacyDefinitionDefaultsToRestrict(t *testing.T) {
	a := newTestApp(t)
	setupOnDeleteParents(t, a)
	createOnDeleteTable(t, a, "kids", onDeleteReference())
	parent := createTestRecord(t, a, "od", "parents", map[string]any{"name": "p"})
	createTestRecord(t, a, "od", "kids", map[string]any{"label": "k", "parent": parent["id"]})

	var raw string
	if err := a.db.QueryRow(`SELECT definition FROM fields_meta WHERE project=? AND group_name=? AND table_name=? AND name=?`, "default", "od", "kids", "parent").Scan(&raw); err != nil {
		t.Fatalf("load definition: %v", err)
	}
	def := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	delete(def, "on_delete")
	stripped, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("encode definition: %v", err)
	}
	if _, err := a.db.Exec(`UPDATE fields_meta SET definition=? WHERE project=? AND group_name=? AND table_name=? AND name=?`, string(stripped), "default", "od", "kids", "parent"); err != nil {
		t.Fatalf("update definition: %v", err)
	}

	blocked := testRequest(t, a, http.MethodDelete, deleteRecordPath("od", "parents", parent["id"].(string)), nil, "")
	assertAPIError(t, blocked, http.StatusConflict)
}
