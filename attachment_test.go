package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func getTableMetadata(t *testing.T, a *app, group, table string) map[string]any {
	t.Helper()
	w := testRequest(t, a, http.MethodGet, "/api/v1/default/data/metadata/groups/"+group+"/tables/"+table, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get table metadata status = %d: %s", w.Code, w.Body.String())
	}
	var meta map[string]any
	testJSON(t, w, &meta)
	return meta
}

func tableField(t *testing.T, table map[string]any, name string) map[string]any {
	t.Helper()
	fields, _ := table["fields"].([]any)
	for _, item := range fields {
		f, _ := item.(map[string]any)
		if f["name"] == name {
			return f
		}
	}
	t.Fatalf("field %q not found in %#v", name, table["fields"])
	return nil
}

func attachmentRecordsURL(group, table string) string {
	return "/api/v1/default/data/groups/" + group + "/tables/" + table + "/records"
}

// TestAttachmentFieldNormalizationAndMetadata covers the field type and the
// optional `accept` UI hint (section 64.9).
func TestAttachmentFieldNormalizationAndMetadata(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name": "invoices",
		"fields": []any{
			map[string]any{"name": "title", "type": "string"},
			map[string]any{"name": "invoice", "type": "attachment", "accept": []string{".pdf", "application/pdf"}},
		},
	})
	invoice := tableField(t, getTableMetadata(t, a, "billing", "invoices"), "invoice")
	if invoice["type"] != "attachment" {
		t.Fatalf("attachment field type = %v, want attachment", invoice["type"])
	}
	accept, ok := invoice["accept"].([]any)
	if !ok || len(accept) != 2 || accept[0] != ".pdf" || accept[1] != "application/pdf" {
		t.Fatalf("attachment accept = %#v, want [.pdf application/pdf]", invoice["accept"])
	}

	// A lone string is normalized to a one-element list.
	createTestTable(t, a, "billing", map[string]any{
		"name":   "scans",
		"fields": []any{map[string]any{"name": "doc", "type": "attachment", "accept": ".png"}},
	})
	doc := tableField(t, getTableMetadata(t, a, "billing", "scans"), "doc")
	if accept, _ := doc["accept"].([]any); len(accept) != 1 || accept[0] != ".png" {
		t.Fatalf("normalized accept = %#v, want [.png]", doc["accept"])
	}

	// Invalid accept hints are rejected with 422.
	for _, bad := range []any{"", []any{}, []any{""}, []any{7}, map[string]any{"x": "y"}} {
		body := map[string]any{"name": "bad", "fields": []any{map[string]any{"name": "doc", "type": "attachment", "accept": bad}}}
		assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/billing/tables", body, "application/json"), http.StatusUnprocessableEntity)
	}
}

// TestAttachmentValidationOnCreateUpdateAndDefault covers existence validation
// against the same project on create, update and required/null handling
// (sections 13.3 and 64.9).
func TestAttachmentValidationOnCreateUpdateAndDefault(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name": "invoices",
		"fields": []any{
			map[string]any{"name": "title", "type": "string"},
			map[string]any{"name": "invoice", "type": "attachment"},
		},
	})
	records := attachmentRecordsURL("billing", "invoices")

	// An unknown ID and a non-string value are rejected on create.
	assertAPIError(t, testRequest(t, a, http.MethodPost, records, map[string]any{"title": "a", "invoice": "missing"}, "application/json"), http.StatusUnprocessableEntity)
	assertAPIError(t, testRequest(t, a, http.MethodPost, records, map[string]any{"title": "a", "invoice": 7}, "application/json"), http.StatusUnprocessableEntity)

	// A required attachment may not be omitted or null.
	createTestTable(t, a, "billing", map[string]any{
		"name":   "strict",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment", "required": true}},
	})
	assertAPIError(t, testRequest(t, a, http.MethodPost, attachmentRecordsURL("billing", "strict"), map[string]any{}, "application/json"), http.StatusUnprocessableEntity)

	entry := putFile(t, a, "default", "/docs/invoice.pdf", "PDF", "application/pdf")
	id, _ := entry["id"].(string)
	record := createTestRecord(t, a, "billing", "invoices", map[string]any{"title": "a", "invoice": id})
	if record["invoice"] != id {
		t.Fatalf("attachment representation = %#v, want the ID string %q", record["invoice"], id)
	}

	recordURL := records + "/" + record["id"].(string)
	assertAPIError(t, testRequest(t, a, http.MethodPatch, recordURL, map[string]any{"invoice": "missing"}, "application/json"), http.StatusUnprocessableEntity)

	// A nullable attachment can be cleared and re-set.
	cleared := testRequest(t, a, http.MethodPatch, recordURL, map[string]any{"invoice": nil}, "application/json")
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear attachment status = %d: %s", cleared.Code, cleared.Body.String())
	}
	var clearedRecord map[string]any
	testJSON(t, cleared, &clearedRecord)
	if clearedRecord["invoice"] != nil {
		t.Fatalf("cleared attachment = %#v, want null", clearedRecord["invoice"])
	}
	if w := testRequest(t, a, http.MethodPatch, recordURL, map[string]any{"invoice": id}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("re-set attachment status = %d: %s", w.Code, w.Body.String())
	}
}

// TestAttachmentDefaultValidation covers validation of a field default on table
// create and metadata update (section 13.2).
func TestAttachmentDefaultValidation(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	badDefault := map[string]any{
		"name":   "bad",
		"fields": []any{map[string]any{"name": "doc", "type": "attachment", "default": "missing"}},
	}
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/billing/tables", badDefault, "application/json"), http.StatusUnprocessableEntity)

	entry := putFile(t, a, "default", "/docs/invoice.pdf", "PDF", "application/pdf")
	id, _ := entry["id"].(string)
	createTestTable(t, a, "billing", map[string]any{
		"name": "invoices",
		"fields": []any{
			map[string]any{"name": "title", "type": "string"},
			map[string]any{"name": "invoice", "type": "attachment", "default": id},
		},
	})
	record := createTestRecord(t, a, "billing", "invoices", map[string]any{"title": "a"})
	if record["invoice"] != id {
		t.Fatalf("defaulted attachment = %#v, want %q", record["invoice"], id)
	}

	tableURL := "/api/v1/default/data/groups/billing/tables/invoices"
	assertAPIError(t, testRequest(t, a, http.MethodPatch, tableURL, map[string]any{
		"fields": []any{map[string]any{"name": "extra", "type": "attachment", "default": "missing"}},
	}, "application/json"), http.StatusUnprocessableEntity)

	if w := testRequest(t, a, http.MethodPatch, tableURL, map[string]any{
		"fields": []any{map[string]any{"name": "extra", "type": "attachment", "default": id}},
	}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("add attachment field with valid default status = %d: %s", w.Code, w.Body.String())
	}
}

// TestAttachmentRejectsCrossProjectEntries ensures an attachment can only name
// an entry in the same project (sections 13.4 and 64.9).
func TestAttachmentRejectsCrossProjectEntries(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project status = %d: %s", w.Code, w.Body.String())
	}
	entry := putFile(t, a, "bills", "/secret.pdf", "PDF", "application/pdf")
	id, _ := entry["id"].(string)

	createTestGroup(t, a, "billing")
	assertAPIError(t, testRequest(t, a, http.MethodPost, "/api/v1/default/data/groups/billing/tables", map[string]any{
		"name":   "cross_default",
		"fields": []any{map[string]any{"name": "doc", "type": "attachment", "default": id}},
	}, "application/json"), http.StatusUnprocessableEntity)

	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	assertAPIError(t, testRequest(t, a, http.MethodPost, attachmentRecordsURL("billing", "invoices"), map[string]any{"invoice": id}, "application/json"), http.StatusUnprocessableEntity)
}

// TestAttachmentDeleteIntegrity covers the delete-integrity rule for a single
// entry, including clearing the value (section 64.9).
func TestAttachmentDeleteIntegrity(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	file := putFile(t, a, "default", "/docs/invoice.pdf", "PDF", "application/pdf")
	id, _ := file["id"].(string)
	record := createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": id})

	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+id, nil, ""), http.StatusConflict)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files?path=%2Fdocs%2Finvoice.pdf", nil, ""), http.StatusConflict)
	// The legacy page facade enforces the same rule.
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/pages?path=%2Fdocs%2Finvoice.pdf", nil, ""), http.StatusConflict)
	if w := testRequest(t, a, http.MethodGet, "/default/files/id/"+id, nil, ""); w.Code != http.StatusOK || w.Body.String() != "PDF" {
		t.Fatalf("referenced file is not still served: status=%d body=%q", w.Code, w.Body.String())
	}

	// Clearing the value unblocks deletion.
	if w := testRequest(t, a, http.MethodPatch, attachmentRecordsURL("billing", "invoices")+"/"+record["id"].(string), map[string]any{"invoice": nil}, "application/json"); w.Code != http.StatusOK {
		t.Fatalf("clear attachment status = %d: %s", w.Code, w.Body.String())
	}
	if w := testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+id, nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete unreferenced file status = %d: %s", w.Code, w.Body.String())
	}

	// A referenced page is blocked the same way.
	page := putFile(t, a, "default", "/docs/note.md", "# note", "")
	createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": page["id"].(string)})
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+page["id"].(string), nil, ""), http.StatusConflict)
}

// TestAttachmentDeleteIntegrityDirectorySubtree covers the directory rule: a
// directory containing a referenced entry cannot be deleted (section 64.9).
func TestAttachmentDeleteIntegrityDirectorySubtree(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	child := putFile(t, a, "default", "/archive/invoice.pdf", "PDF", "application/pdf")
	putFile(t, a, "default", "/archive/notes.txt", "notes", "text/plain")
	createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": child["id"].(string)})

	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files?path=%2Farchive", nil, ""), http.StatusConflict)
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/directories?path=%2Farchive", nil, ""), http.StatusConflict)

	// An unreferenced sibling is still deletable while the directory is blocked.
	if w := testRequest(t, a, http.MethodDelete, "/api/v1/default/files?path=%2Farchive%2Fnotes.txt", nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete unreferenced sibling status = %d: %s", w.Code, w.Body.String())
	}
}

// TestAttachmentSurvivesRenameAndMove verifies the stored ID is stable across a
// move, so the record link keeps resolving (sections 64.2 and 64.9).
func TestAttachmentSurvivesRenameAndMove(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	file := putFile(t, a, "default", "/docs/invoice.pdf", "PDF", "application/pdf")
	id, _ := file["id"].(string)
	record := createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": id})

	move := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move", map[string]any{"from": "/docs/invoice.pdf", "to": "/2026/invoice.pdf"}, "application/json")
	if move.Code != http.StatusOK {
		t.Fatalf("move status = %d: %s", move.Code, move.Body.String())
	}
	byID := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id, nil, "")
	var moved map[string]any
	testJSON(t, byID, &moved)
	if moved["id"] != id || moved["path"] != "/2026/invoice.pdf" {
		t.Fatalf("moved entry = %#v", moved)
	}
	reread := testRequest(t, a, http.MethodGet, attachmentRecordsURL("billing", "invoices")+"/"+record["id"].(string), nil, "")
	var rereadRecord map[string]any
	testJSON(t, reread, &rereadRecord)
	if rereadRecord["invoice"] != id {
		t.Fatalf("attachment after move = %#v, want %q", rereadRecord["invoice"], id)
	}
	if w := testRequest(t, a, http.MethodGet, "/default/files/id/"+id, nil, ""); w.Code != http.StatusOK || w.Body.String() != "PDF" {
		t.Fatalf("stable URL after move status = %d body = %q", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+id, nil, ""), http.StatusConflict)
}

// TestAttachmentFilterSortAndDistinct covers filtering, sorting and distinct
// over the stored ID string (section 64.9).
func TestAttachmentFilterSortAndDistinct(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name":   "invoices",
		"fields": []any{map[string]any{"name": "invoice", "type": "attachment"}},
	})
	first := putFile(t, a, "default", "/docs/a.pdf", "A", "application/pdf")
	second := putFile(t, a, "default", "/docs/b.pdf", "B", "application/pdf")
	idA, idB := first["id"].(string), second["id"].(string)
	createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": idA})
	createTestRecord(t, a, "billing", "invoices", map[string]any{"invoice": idB})

	filtered := testRequest(t, a, http.MethodGet, attachmentRecordsURL("billing", "invoices")+"?filter.invoice="+idA, nil, "")
	var filteredPage map[string]any
	testJSON(t, filtered, &filteredPage)
	if len(filteredPage["data"].([]any)) != 1 {
		t.Fatalf("filter.invoice returned %d records, want 1", len(filteredPage["data"].([]any)))
	}

	sorted := testRequest(t, a, http.MethodGet, attachmentRecordsURL("billing", "invoices")+"?sort=invoice&order=asc", nil, "")
	var sortedPage map[string]any
	testJSON(t, sorted, &sortedPage)
	rows := sortedPage["data"].([]any)
	low, high := idA, idB
	if high < low {
		low, high = high, low
	}
	if rows[0].(map[string]any)["invoice"] != low || rows[1].(map[string]any)["invoice"] != high {
		t.Fatalf("sorted attachment values = %#v, want %q then %q", rows, low, high)
	}

	distinct := testRequest(t, a, http.MethodGet, attachmentRecordsURL("billing", "invoices")+"?distinct=invoice", nil, "")
	var distinctPage map[string]any
	testJSON(t, distinct, &distinctPage)
	if len(distinctPage["values"].([]any)) != 2 {
		t.Fatalf("distinct attachment values = %#v, want 2", distinctPage["values"])
	}
}

// TestAttachmentFormAndRecordRendering covers the choose control, the recorded
// link and correct escaping (section 64.9).
func TestAttachmentFormAndRecordRendering(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "billing")
	createTestTable(t, a, "billing", map[string]any{
		"name": "invoices",
		"fields": []any{
			map[string]any{"name": "title", "type": "string"},
			map[string]any{"name": "invoice", "type": "attachment", "accept": ".pdf"},
		},
	})
	entry := putFile(t, a, "default", "/docs/Invoice & <b>.pdf", "PDF", "application/pdf")
	id, _ := entry["id"].(string)

	form := testRequest(t, a, http.MethodGet, "/default/data/billing/invoices/new", nil, "")
	body := form.Body.String()
	if form.Code != http.StatusOK || !strings.Contains(body, `name="invoice"`) || !strings.Contains(body, `value="`+id+`"`) {
		t.Fatalf("attachment form missing the choose control: status=%d", form.Code)
	}
	if !strings.Contains(body, "Invoice &amp; &lt;b&gt;.pdf") {
		t.Error("attachment form did not escape the file path label")
	}
	if !strings.Contains(body, "Accepted: .pdf") {
		t.Error("attachment form omitted the accept hint")
	}

	created := testRequest(t, a, http.MethodPost, "/default/data/billing/invoices/new", url.Values{"title": {"From form"}, "invoice": {id}}.Encode(), "application/x-www-form-urlencoded")
	if created.Code != http.StatusSeeOther {
		t.Fatalf("attachment form submission status = %d: %s", created.Code, created.Body.String())
	}
	location := created.Header().Get("Location")
	detail := testRequest(t, a, http.MethodGet, location, nil, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `href="/default/files/id/`+id+`"`) {
		t.Fatalf("record view did not link the attachment: status=%d", detail.Code)
	}
}
