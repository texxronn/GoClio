package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// searchURL builds a project search URL with an escaped query.
func searchURL(project, query string) string {
	return "/api/v1/" + project + "/search?q=" + url.QueryEscape(query)
}

type searchResponse struct {
	Data []map[string]any `json:"data"`
	Page map[string]any   `json:"page"`
}

// search runs a query through HTTP and returns the decoded response.
func search(t *testing.T, a *app, project, query string) searchResponse {
	t.Helper()
	return searchPage(t, a, project, query, -1, -1)
}

// searchPage runs a query with an optional limit/offset (negative means omit).
func searchPage(t *testing.T, a *app, project, query string, limit, offset int) searchResponse {
	t.Helper()
	target := searchURL(project, query)
	if limit >= 0 {
		target += "&limit=" + strconv.Itoa(limit)
	}
	if offset >= 0 {
		target += "&offset=" + strconv.Itoa(offset)
	}
	w := testRequest(t, a, http.MethodGet, target, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("search %q status = %d: %s", query, w.Code, w.Body.String())
	}
	var body searchResponse
	testJSON(t, w, &body)
	return body
}

func searchPaths(res searchResponse) []string {
	paths := make([]string, 0, len(res.Data))
	for _, item := range res.Data {
		paths = append(paths, item["path"].(string))
	}
	return paths
}

func TestSearchReturnsRankedResultsAndShape(t *testing.T) {
	a := newTestApp(t)
	// "alpha" appears in the title of the first entry and only in the body of
	// the second; the title boost must rank the first one higher.
	ranked := putFile(t, a, "default", "/rank/alpha.txt", "an unrelated body", "text/plain")
	putFile(t, a, "default", "/rank/other.txt", "one alpha appears among several other words here", "text/plain")

	res := search(t, a, "default", "alpha")
	if len(res.Data) != 2 {
		t.Fatalf("results = %v, want 2", searchPaths(res))
	}
	first := res.Data[0]
	for _, key := range []string{"id", "path", "kind", "content_type", "source", "snippet", "score"} {
		if _, ok := first[key]; !ok {
			t.Errorf("result is missing %q: %#v", key, first)
		}
	}
	if first["id"] != ranked["id"] {
		t.Errorf("first result = %v, want the title match %v", first, ranked["id"])
	}
	if first["path"] != "/rank/alpha.txt" || first["kind"] != "file" || first["content_type"] != "text/plain" || first["source"] != "native" {
		t.Errorf("first result = %#v", first)
	}
	if score, ok := first["score"].(float64); !ok || score <= 0 {
		t.Errorf("score = %v, want a positive number", first["score"])
	}
	if res.Page["total"] != float64(2) || res.Page["count"] != float64(2) {
		t.Errorf("page = %#v", res.Page)
	}
}

func TestSearchSnippetsArePlainTextWithSentinels(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/docs/report.txt", "the amount due 184.20 on 2026-03-15", "text/plain")

	res := search(t, a, "default", "184.20")
	if len(res.Data) != 1 {
		t.Fatalf("results = %v, want 1", searchPaths(res))
	}
	snippet, _ := res.Data[0]["snippet"].(string)
	if !strings.Contains(snippet, snippetStart+"184.20"+snippetEnd) {
		t.Errorf("snippet = %q, want the match wrapped in %q/%q", snippet, snippetStart, snippetEnd)
	}

	// Snippets are plain text; the UI escapes before applying markup.
	putFile(t, a, "default", "/docs/markup.txt", `<script>alert(1)</script> needle in haystack`, "text/plain")
	res = search(t, a, "default", "needle")
	if len(res.Data) != 1 {
		t.Fatalf("markup results = %v, want 1", searchPaths(res))
	}
	raw, _ := res.Data[0]["snippet"].(string)
	if !strings.Contains(raw, "<script>") {
		t.Errorf("API snippet should stay plain text, got %q", raw)
	}
	rendered := renderSnippetMarkup(raw)
	if strings.Contains(rendered, "<script>") || !strings.Contains(rendered, "&lt;script&gt;") {
		t.Errorf("rendered snippet did not escape content markup: %q", rendered)
	}
	if !strings.Contains(rendered, "<mark>needle</mark>") {
		t.Errorf("rendered snippet did not highlight the match: %q", rendered)
	}
	if strings.Contains(rendered, snippetStart) || strings.Contains(rendered, snippetEnd) {
		t.Errorf("rendered snippet kept sentinels: %q", rendered)
	}
}

func TestSearchPagingIsDeterministic(t *testing.T) {
	a := newTestApp(t)
	for _, name := range []string{"a", "b", "c"} {
		putFile(t, a, "default", "/paged/"+name+".txt", "paged document "+name, "text/plain")
	}
	first := searchPage(t, a, "default", "paged", 2, 0)
	if len(first.Data) != 2 || first.Page["total"] != float64(3) || first.Page["count"] != float64(2) || first.Page["limit"] != float64(2) {
		t.Fatalf("first page = %#v data=%v", first.Page, searchPaths(first))
	}
	second := searchPage(t, a, "default", "paged", 2, 2)
	if len(second.Data) != 1 || second.Page["offset"] != float64(2) {
		t.Fatalf("second page = %#v data=%v", second.Page, searchPaths(second))
	}
	seen := map[string]bool{}
	for _, item := range append(append([]map[string]any{}, first.Data...), second.Data...) {
		id := item["id"].(string)
		if seen[id] {
			t.Errorf("entry %s appeared on both pages", id)
		}
		seen[id] = true
	}
	if len(seen) != 3 {
		t.Errorf("paged union has %d entries, want 3", len(seen))
	}
}

func TestSearchRejectsEmptyAndOversizedAndBadPaging(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/doc.txt", "searchable", "text/plain")

	for _, target := range []string{
		"/api/v1/default/search",
		"/api/v1/default/search?q=",
		"/api/v1/default/search?q=%20%20",
	} {
		if w := testRequest(t, a, http.MethodGet, target, nil, ""); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %s status = %d, want 422: %s", target, w.Code, w.Body.String())
		}
	}
	oversized := "/api/v1/default/search?q=" + url.QueryEscape(strings.Repeat("a", maxSearchQueryBytes+1))
	if w := testRequest(t, a, http.MethodGet, oversized, nil, ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("oversized query status = %d, want 422", w.Code)
	}
	for _, target := range []string{
		"/api/v1/default/search?q=searchable&limit=0",
		"/api/v1/default/search?q=searchable&limit=1001",
		"/api/v1/default/search?q=searchable&offset=-1",
	} {
		if w := testRequest(t, a, http.MethodGet, target, nil, ""); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %s status = %d, want 422: %s", target, w.Code, w.Body.String())
		}
	}
}

func TestBuildMatchQueryQuotesAndPrefix(t *testing.T) {
	cases := []struct {
		terms  []string
		prefix bool
		want   string
	}{
		{[]string{"foo"}, false, `({title body} : "foo")`},
		{[]string{"foo", "bar"}, false, `({title body} : "foo") AND ({title body} : "bar")`},
		{[]string{`foo"bar`}, false, `({title body} : "foo""bar")`},
		{[]string{"foo"}, true, `({title body} : "foo"*)`},
	}
	for _, c := range cases {
		if got := buildMatchQuery(c.terms, c.prefix); got != c.want {
			t.Errorf("buildMatchQuery(%v, %v) = %q, want %q", c.terms, c.prefix, got, c.want)
		}
	}

	// The prefix mode (available for as-you-type search) matches a token prefix;
	// the literal mode the API exposes does not.
	a := newTestApp(t)
	putFile(t, a, "default", "/word.txt", "alphabet soup", "text/plain")
	scoped := a.withProject("default")
	if hits, _, err := scoped.searchContent(buildMatchQuery([]string{"alpha"}, false), 10, 0); err != nil || len(hits) != 0 {
		t.Errorf("literal alpha hits=%v err=%v, want none", hits, err)
	}
	if hits, _, err := scoped.searchContent(buildMatchQuery([]string{"alpha"}, true), 10, 0); err != nil || len(hits) != 1 {
		t.Errorf("prefix search hits=%v err=%v, want one", hits, err)
	}
}

func TestSearchTreatsOperatorsAsLiterals(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/lit/alpha.txt", "alpha only", "text/plain")
	putFile(t, a, "default", "/lit/beta.txt", "beta only", "text/plain")
	putFile(t, a, "default", "/lit/both.txt", "alpha or beta together", "text/plain")
	putFile(t, a, "default", "/lit/word.txt", "alphabet soup", "text/plain")

	// "OR" is a literal token, not an operator: all three terms must appear, so
	// only the document that literally contains them matches.
	res := search(t, a, "default", "alpha OR beta")
	if paths := searchPaths(res); len(paths) != 1 || paths[0] != "/lit/both.txt" {
		t.Errorf(`search "alpha OR beta" = %v, want only /lit/both.txt`, paths)
	}

	// A trailing "*" is not a prefix operator: "alpha*" matches the literal
	// token "alpha" but never "alphabet".
	res = search(t, a, "default", "alpha*")
	for _, path := range searchPaths(res) {
		if path == "/lit/word.txt" {
			t.Errorf(`search "alpha*" prefix-matched "alphabet": %v`, searchPaths(res))
		}
	}

	// Operator-looking input never errors and never leaks another project's
	// content. These queries have no literal occurrence and return nothing.
	for _, q := range []string{"NEAR", "AND", "NOT", `"`, "*", "-", "^", ":", "NEAR(alpha beta)", `say "hi"`} {
		w := testRequest(t, a, http.MethodGet, searchURL("default", q), nil, "")
		if w.Code != http.StatusOK {
			t.Errorf("query %q status = %d, want 200: %s", q, w.Code, w.Body.String())
			continue
		}
		var body searchResponse
		testJSON(t, w, &body)
		if len(body.Data) != 0 {
			t.Errorf("query %q unexpectedly matched %v", q, searchPaths(body))
		}
	}
}

func TestSearchReportsIndexSourceAndContentType(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/bills/scan.pdf", string(tinyPDF("")), "application/pdf")
	// A synthetic agent row stands in for Phase 8 enrichment.
	if _, err := a.db.Exec(
		`INSERT INTO content_search(id,project,path,kind,source,title,body) VALUES(?,?,?,?,?,?,?)`,
		entry["id"], "default", "/bills/scan.pdf", "file", "agent:tesseract", "scan.pdf", "agent supplied total due",
	); err != nil {
		t.Fatal(err)
	}
	res := search(t, a, "default", "supplied")
	if len(res.Data) != 1 {
		t.Fatalf("results = %v, want 1", searchPaths(res))
	}
	got := res.Data[0]
	if got["id"] != entry["id"] || got["source"] != "agent:tesseract" || got["content_type"] != "application/pdf" || got["kind"] != "file" {
		t.Errorf("result = %#v", got)
	}
	// The entry's `indexed` flag now reflects the agent row too.
	fetched := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+entry["id"].(string), nil, "")
	var body map[string]any
	testJSON(t, fetched, &body)
	if body["indexed"] != true {
		t.Errorf("indexed = %v, want true after enrichment", body["indexed"])
	}
}

func TestSearchIsProjectScoped(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	defaultEntry := putFile(t, a, "default", "/shared.txt", "zebrafish default marker", "text/plain")
	billsEntry := putFile(t, a, "bills", "/shared.txt", "zebrafish bills marker", "text/plain")

	res := search(t, a, "default", "zebrafish")
	if len(res.Data) != 1 || res.Data[0]["id"] != defaultEntry["id"] {
		t.Errorf("default search = %v, want the default entry", searchPaths(res))
	}
	res = search(t, a, "bills", "zebrafish")
	if len(res.Data) != 1 || res.Data[0]["id"] != billsEntry["id"] {
		t.Errorf("bills search = %v, want the bills entry", searchPaths(res))
	}
	if res := search(t, a, "default", "bills"); len(res.Data) != 0 {
		t.Errorf("bills content leaked into default: %v", searchPaths(res))
	}

	// An unknown project is 404, and search is only accessible under a project.
	if w := testRequest(t, a, http.MethodGet, searchURL("ghost", "zebrafish"), nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown project search status = %d, want 404", w.Code)
	}
	if w := testRequest(t, a, http.MethodPost, searchURL("default", "zebrafish"), nil, ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("search POST status = %d, want 405", w.Code)
	}
}

func TestFilesRepresentationReportsIndexedState(t *testing.T) {
	a := newTestApp(t)
	text := putFile(t, a, "default", "/docs/note.txt", "indexed body", "text/plain")
	if text["indexed"] != true {
		t.Errorf("text entry indexed = %v, want true", text["indexed"])
	}
	binary := putFile(t, a, "default", "/docs/blob.bin", "\x00\x01\x02", "application/octet-stream")
	if binary["indexed"] != false {
		t.Errorf("binary entry indexed = %v, want false", binary["indexed"])
	}

	byID := testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+text["id"].(string), nil, "")
	var fetched map[string]any
	testJSON(t, byID, &fetched)
	if fetched["indexed"] != true {
		t.Errorf("GET by id indexed = %v, want true", fetched["indexed"])
	}

	w := testRequest(t, a, http.MethodGet, "/api/v1/default/files", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list files status = %d: %s", w.Code, w.Body.String())
	}
	var listing struct {
		Data []map[string]any `json:"data"`
	}
	testJSON(t, w, &listing)
	for _, item := range listing.Data {
		switch item["path"] {
		case "/docs/note.txt":
			if item["indexed"] != true {
				t.Errorf("listed text entry indexed = %v, want true", item["indexed"])
			}
		case "/docs/blob.bin":
			if item["indexed"] != false {
				t.Errorf("listed binary entry indexed = %v, want false", item["indexed"])
			}
		}
	}
}
