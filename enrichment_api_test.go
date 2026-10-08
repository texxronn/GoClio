package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractionURL builds the enrichment URL addressed by content path.
func extractionURL(project, path string) string {
	return "/api/v1/" + project + "/files/extraction?path=" + url.QueryEscape(path)
}

// extractionByIDURL builds the enrichment URL addressed by entry ID (section
// 64.8: the enclosing entry may be given by path or by file ID).
func extractionByIDURL(project, id string) string {
	return "/api/v1/" + project + "/files/extraction?id=" + url.QueryEscape(id)
}

// putEnrichment sends a PUT and returns the recorder without asserting status.
func putEnrichment(t *testing.T, a *app, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return testRequest(t, a, http.MethodPut, target, body, "application/json")
}

// diskFingerprint computes the size:mtime fingerprint the same way the handler
// does, so tests can assert the canonical form without hard-coding a timestamp.
func diskFingerprint(t *testing.T, a *app, path string) string {
	t.Helper()
	target := filepath.Join(a.contentRoot(), filepath.FromSlash(strings.TrimPrefix(path, "/")))
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return entryFingerprint(info)
}

func TestEnrichmentRequiresMatchingFingerprint(t *testing.T) {
	a := newTestApp(t)
	// An image-only PDF has no native text, so any searchable text must come
	// from enrichment.
	entry := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	id := entry["id"].(string)

	// The GET returns the entry's current (empty) extraction and its fingerprint.
	got := testRequest(t, a, http.MethodGet, extractionURL("default", "/bills/scan.pdf"), nil, "")
	if got.Code != http.StatusOK {
		t.Fatalf("GET extraction status = %d: %s", got.Code, got.Body.String())
	}
	var current map[string]any
	testJSON(t, got, &current)
	if current["id"] != id || current["path"] != "/bills/scan.pdf" {
		t.Errorf("GET extraction = %#v", current)
	}
	if current["source"] != "" || current["text"] != "" || current["indexed"] != false {
		t.Errorf("empty extraction = %#v, want no source/text and indexed false", current)
	}
	fingerprint, _ := current["fingerprint"].(string)
	if fingerprint != diskFingerprint(t, a, "/bills/scan.pdf") {
		t.Fatalf("fingerprint = %q, want the on-disk size:mtime", fingerprint)
	}
	if !strings.Contains(fingerprint, ":") {
		t.Fatalf("fingerprint = %q, want the size:mtime form", fingerprint)
	}

	// A stale fingerprint is refused.
	stale := putEnrichment(t, a, extractionURL("default", "/bills/scan.pdf"), map[string]any{
		"fingerprint": "1:2000-01-01T00:00:00Z",
		"text":        "stale invoice text",
		"provider":    "ocr:tesseract",
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want 409: %s", stale.Code, stale.Body.String())
	}
	if res := search(t, a, "default", "stale"); len(res.Data) != 0 {
		t.Errorf("stale extraction was stored: %v", searchPaths(res))
	}

	// A matching fingerprint is accepted and becomes searchable.
	ok := putEnrichment(t, a, extractionURL("default", "/bills/scan.pdf"), map[string]any{
		"fingerprint": fingerprint,
		"text":        "Invoice 4421 total due 184.20",
		"provider":    "ocr:tesseract",
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("matching PUT status = %d, want 200: %s", ok.Code, ok.Body.String())
	}
	var stored map[string]any
	testJSON(t, ok, &stored)
	if stored["source"] != "agent:ocr:tesseract" || stored["text"] != "Invoice 4421 total due 184.20" {
		t.Errorf("stored extraction = %#v", stored)
	}
	if stored["title"] != "scan.pdf" {
		t.Errorf("default title = %v, want the entry base name", stored["title"])
	}

	res := search(t, a, "default", "4421")
	if len(res.Data) != 1 {
		t.Fatalf("agent search results = %v, want 1", searchPaths(res))
	}
	if res.Data[0]["source"] != "agent:ocr:tesseract" || res.Data[0]["id"] != id {
		t.Errorf("agent result = %#v", res.Data[0])
	}

	// GET now reports the agent extraction.
	got = testRequest(t, a, http.MethodGet, extractionByIDURL("default", id), nil, "")
	testJSON(t, got, &current)
	if current["source"] != "agent:ocr:tesseract" || current["text"] != "Invoice 4421 total due 184.20" || current["indexed"] != true {
		t.Errorf("GET after PUT = %#v", current)
	}
}

func TestEnrichmentAcceptsSha256Fingerprint(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	sha, _ := entry["sha256"].(string)
	if sha == "" {
		t.Fatal("entry has no sha256")
	}
	w := putEnrichment(t, a, extractionByIDURL("default", entry["id"].(string)), map[string]any{
		"fingerprint": sha,
		"text":        "hash matched text",
		"provider":    "ocr:custom",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("sha256 PUT status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	testJSON(t, w, &body)
	if body["source"] != "agent:ocr:custom" {
		t.Errorf("source = %v, want agent:ocr:custom", body["source"])
	}
}

func TestEnrichmentByIDPathSegment(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	id := entry["id"].(string)
	segment := "/api/v1/default/files/" + id + "/extraction"

	// PUT, GET and DELETE all accept the files-API-style ID path.
	w := putEnrichment(t, a, segment, map[string]any{
		"fingerprint": diskFingerprint(t, a, "/bills/scan.pdf"),
		"text":        "segmentpath marker",
		"provider":    "ocr:tesseract",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT by path segment status = %d: %s", w.Code, w.Body.String())
	}
	got := testRequest(t, a, http.MethodGet, segment, nil, "")
	if got.Code != http.StatusOK {
		t.Fatalf("GET by path segment status = %d: %s", got.Code, got.Body.String())
	}
	var current map[string]any
	testJSON(t, got, &current)
	if current["id"] != id || current["source"] != "agent:ocr:tesseract" {
		t.Errorf("GET by path segment = %#v", current)
	}
	del := testRequest(t, a, http.MethodDelete, segment, nil, "")
	if del.Code != http.StatusNoContent {
		t.Errorf("DELETE by path segment status = %d, want 204: %s", del.Code, del.Body.String())
	}
}

func TestEnrichmentValidatesRequestShape(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/notes.txt", "plain native text", "text/plain")
	target := extractionByIDURL("default", entry["id"].(string))
	fingerprint := diskFingerprint(t, a, "/notes.txt")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing fingerprint", map[string]any{"text": "x", "provider": "ocr:tesseract"}},
		{"missing provider", map[string]any{"fingerprint": fingerprint, "text": "x"}},
		{"invalid provider", map[string]any{"fingerprint": fingerprint, "text": "x", "provider": "bad provider"}},
		{"missing text", map[string]any{"fingerprint": fingerprint, "provider": "ocr:tesseract"}},
		{"blank text", map[string]any{"fingerprint": fingerprint, "text": "   ", "provider": "ocr:tesseract"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := putEnrichment(t, a, target, tc.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422: %s", w.Code, w.Body.String())
			}
		})
	}

	// Exactly one of path or id is required.
	both := testRequest(t, a, http.MethodGet, extractionURL("default", "/notes.txt")+"&id="+entry["id"].(string), nil, "")
	if both.Code != http.StatusUnprocessableEntity {
		t.Errorf("path+id status = %d, want 422: %s", both.Code, both.Body.String())
	}
	neither := testRequest(t, a, http.MethodGet, "/api/v1/default/files/extraction", nil, "")
	if neither.Code != http.StatusUnprocessableEntity {
		t.Errorf("no target status = %d, want 422: %s", neither.Code, neither.Body.String())
	}
	// A missing entry is 404.
	missingEntry := testRequest(t, a, http.MethodGet, extractionURL("default", "/absent.txt"), nil, "")
	if missingEntry.Code != http.StatusNotFound {
		t.Errorf("missing entry status = %d, want 404: %s", missingEntry.Code, missingEntry.Body.String())
	}
}

func TestEnrichmentDeleteFallsBackToNative(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/docs/report.txt", "plaintextbody marker1", "text/plain")
	id := entry["id"].(string)

	// Native text is searchable before enrichment.
	if res := search(t, a, "default", "marker1"); len(res.Data) != 1 || res.Data[0]["source"] != "native" {
		t.Fatalf("native search before enrichment = %v", searchPaths(res))
	}

	w := putEnrichment(t, a, extractionByIDURL("default", id), map[string]any{
		"fingerprint": diskFingerprint(t, a, "/docs/report.txt"),
		"text":        "agentbody marker2",
		"provider":    "ocr:tesseract",
		"title":       "Custom Title",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}
	// The agent row replaces the native row.
	if res := search(t, a, "default", "marker1"); len(res.Data) != 0 {
		t.Errorf("native text still indexed after enrichment: %v", searchPaths(res))
	}
	if res := search(t, a, "default", "marker2"); len(res.Data) != 1 {
		t.Errorf("agent text not searchable: %v", searchPaths(res))
	}

	del := testRequest(t, a, http.MethodDelete, extractionByIDURL("default", id), nil, "")
	if del.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204: %s", del.Code, del.Body.String())
	}
	// The entry falls back to its native text.
	res := search(t, a, "default", "marker1")
	if len(res.Data) != 1 || res.Data[0]["source"] != "native" {
		t.Errorf("native fallback = %v", searchPaths(res))
	}
	if res := search(t, a, "default", "marker2"); len(res.Data) != 0 {
		t.Errorf("agent text survived DELETE: %v", searchPaths(res))
	}
	got := testRequest(t, a, http.MethodGet, extractionByIDURL("default", id), nil, "")
	var current map[string]any
	testJSON(t, got, &current)
	if current["source"] != "native" || !strings.Contains(current["text"].(string), "plaintextbody") {
		t.Errorf("GET after DELETE = %#v", current)
	}

	// DELETE is idempotent when there is no agent row.
	again := testRequest(t, a, http.MethodDelete, extractionByIDURL("default", id), nil, "")
	if again.Code != http.StatusNoContent {
		t.Errorf("second DELETE status = %d, want 204", again.Code)
	}
}

func TestEnrichmentSurvivesMove(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	id := entry["id"].(string)
	w := putEnrichment(t, a, extractionByIDURL("default", id), map[string]any{
		"fingerprint": diskFingerprint(t, a, "/bills/scan.pdf"),
		"text":        "movedmarker invoice",
		"provider":    "ocr:tesseract",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}

	move := testRequest(t, a, http.MethodPost, "/api/v1/default/files/move",
		map[string]any{"from": "/bills/scan.pdf", "to": "/archive/2026/scan.pdf"}, "application/json")
	if move.Code != http.StatusOK {
		t.Fatalf("move status = %d: %s", move.Code, move.Body.String())
	}

	// The extraction stays keyed by ID and its path is re-homed.
	got := testRequest(t, a, http.MethodGet, extractionByIDURL("default", id), nil, "")
	var current map[string]any
	testJSON(t, got, &current)
	if current["id"] != id || current["path"] != "/archive/2026/scan.pdf" {
		t.Errorf("GET after move = %#v", current)
	}
	if current["source"] != "agent:ocr:tesseract" || current["text"] != "movedmarker invoice" {
		t.Errorf("extraction did not survive move: %#v", current)
	}
	res := search(t, a, "default", "movedmarker")
	if len(res.Data) != 1 || res.Data[0]["path"] != "/archive/2026/scan.pdf" || res.Data[0]["source"] != "agent:ocr:tesseract" {
		t.Errorf("search after move = %v", searchPaths(res))
	}
	// Path-addressed GET resolves the new path too.
	byPath := testRequest(t, a, http.MethodGet, extractionURL("default", "/archive/2026/scan.pdf"), nil, "")
	if byPath.Code != http.StatusOK {
		t.Fatalf("GET by moved path status = %d: %s", byPath.Code, byPath.Body.String())
	}
}

func TestEnrichmentIsProjectScoped(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	defaultEntry := putFile(t, a, "default", "/scan.pdf", string(tinyPDF("")), "application/pdf")
	billsEntry := putFile(t, a, "bills", "/scan.pdf", string(tinyPDF("")), "application/pdf")

	if w := putEnrichment(t, a, extractionByIDURL("default", defaultEntry["id"].(string)), map[string]any{
		"fingerprint": diskFingerprint(t, a, "/scan.pdf"),
		"text":        "defaultonly marker",
		"provider":    "ocr:tesseract",
	}); w.Code != http.StatusOK {
		t.Fatalf("default PUT status = %d: %s", w.Code, w.Body.String())
	}

	// The other project sees no extraction for its own entry or by the default ID.
	if res := search(t, a, "bills", "defaultonly"); len(res.Data) != 0 {
		t.Errorf("enrichment leaked across projects: %v", searchPaths(res))
	}
	cross := testRequest(t, a, http.MethodGet, extractionByIDURL("bills", defaultEntry["id"].(string)), nil, "")
	if cross.Code != http.StatusNotFound {
		t.Errorf("cross-project id status = %d, want 404", cross.Code)
	}
	// A PUT in the other project does not disturb the first.
	if w := putEnrichment(t, a, extractionByIDURL("bills", billsEntry["id"].(string)), map[string]any{
		"fingerprint": diskFingerprint(t, a.withProject("bills"), "/scan.pdf"),
		"text":        "billsonly marker",
		"provider":    "ocr:other",
	}); w.Code != http.StatusOK {
		t.Fatalf("bills PUT status = %d: %s", w.Code, w.Body.String())
	}
	if res := search(t, a, "default", "defaultonly"); len(res.Data) != 1 {
		t.Errorf("default enrichment lost: %v", searchPaths(res))
	}
	if res := search(t, a, "default", "billsonly"); len(res.Data) != 0 {
		t.Errorf("bills enrichment visible in default: %v", searchPaths(res))
	}
}

func TestEnrichmentEditedFileInvalidatesAgentText(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/docs/note.txt", "initial body", "text/plain")
	id := entry["id"].(string)
	oldFingerprint := diskFingerprint(t, a, "/docs/note.txt")
	if w := putEnrichment(t, a, extractionByIDURL("default", id), map[string]any{
		"fingerprint": oldFingerprint,
		"text":        "agentmarker stale",
		"provider":    "ocr:tesseract",
	}); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", w.Code, w.Body.String())
	}

	// Rewriting the entry changes its bytes: the agent row is dropped and the
	// native text is rebuilt.
	putFile(t, a, "default", "/docs/note.txt", "changed body with new words", "text/plain")
	if res := search(t, a, "default", "agentmarker"); len(res.Data) != 0 {
		t.Errorf("stale agent text survived a rewrite: %v", searchPaths(res))
	}
	if res := search(t, a, "default", "changed"); len(res.Data) != 1 || res.Data[0]["source"] != "native" {
		t.Errorf("native rebuild after rewrite = %v", searchPaths(res))
	}

	// The fingerprint an agent held before the rewrite no longer matches.
	stale := putEnrichment(t, a, extractionByIDURL("default", id), map[string]any{
		"fingerprint": oldFingerprint,
		"text":        "agentmarker stale",
		"provider":    "ocr:tesseract",
	})
	if stale.Code != http.StatusConflict {
		t.Errorf("post-rewrite PUT status = %d, want 409: %s", stale.Code, stale.Body.String())
	}
}

func TestEnrichmentMethodNotAllowed(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/notes.txt", "body", "text/plain")
	w := testRequest(t, a, http.MethodPost, extractionURL("default", "/notes.txt"), map[string]any{}, "application/json")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405: %s", w.Code, w.Body.String())
	}
}
