package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func requestWithHeaders(t *testing.T, a *app, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func TestFileContentDownloadHeadersAndRanges(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/docs/notes.txt", "0123456789", "text/plain")
	url := "/api/v1/default/files/" + entry["id"].(string) + "/content"

	full := requestWithHeaders(t, a, http.MethodGet, url, nil)
	if full.Code != http.StatusOK {
		t.Fatalf("GET content status = %d: %s", full.Code, full.Body.String())
	}
	if got := full.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
	if got := full.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := full.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
	if got := full.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := full.Body.String(); got != "0123456789" {
		t.Errorf("body = %q", got)
	}

	partial := requestWithHeaders(t, a, http.MethodGet, url, map[string]string{"Range": "bytes=2-5"})
	if partial.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206: %s", partial.Code, partial.Body.String())
	}
	if got := partial.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Errorf("Content-Range = %q, want bytes 2-5/10", got)
	}
	if got := partial.Body.String(); got != "2345" {
		t.Errorf("range body = %q, want 2345", got)
	}

	unsatisfiable := requestWithHeaders(t, a, http.MethodGet, url, map[string]string{"Range": "bytes=50-60"})
	if unsatisfiable.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("unsatisfiable range status = %d, want 416", unsatisfiable.Code)
	}

	head := requestWithHeaders(t, a, http.MethodHead, url, nil)
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d: %s", head.Code, head.Body.String())
	}
	if got := head.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("HEAD Content-Disposition = %q", got)
	}
	if got := head.Header().Get("Content-Length"); got != "10" {
		t.Errorf("HEAD Content-Length = %q, want 10", got)
	}
	if head.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", head.Body.String())
	}

	if w := testRequest(t, a, http.MethodPost, url, "x", "text/plain"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST content status = %d, want 405", w.Code)
	}
}

func TestFileContentDownloadOmitsModificationConditionals(t *testing.T) {
	a := newTestApp(t)
	entry := putFile(t, a, "default", "/docs/notes.txt", "0123456789", "text/plain")
	url := "/api/v1/default/files/" + entry["id"].(string) + "/content"

	full := requestWithHeaders(t, a, http.MethodGet, url, nil)
	if full.Code != http.StatusOK {
		t.Fatalf("GET content status = %d: %s", full.Code, full.Body.String())
	}
	if got := full.Header().Get("Last-Modified"); got != "" {
		t.Errorf("Last-Modified = %q, want empty (section 3.1)", got)
	}
	if got := full.Header().Get("ETag"); got != "" {
		t.Errorf("ETag = %q, want empty", got)
	}
	if got := full.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
	if got := full.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	// A modification-time conditional must not short-circuit with 304.
	conditional := requestWithHeaders(t, a, http.MethodGet, url, map[string]string{"If-Modified-Since": "Wed, 21 Oct 2015 07:28:00 GMT"})
	if conditional.Code != http.StatusOK {
		t.Fatalf("If-Modified-Since status = %d, want 200", conditional.Code)
	}
	if conditional.Body.String() != "0123456789" {
		t.Errorf("conditional body = %q, want the full body", conditional.Body.String())
	}

	// Ranges still work without a modification time.
	partial := requestWithHeaders(t, a, http.MethodGet, url, map[string]string{"Range": "bytes=2-5"})
	if partial.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", partial.Code)
	}
	if got := partial.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Errorf("Content-Range = %q, want bytes 2-5/10", got)
	}
	if partial.Body.String() != "2345" {
		t.Errorf("range body = %q, want 2345", partial.Body.String())
	}
}

func TestFileContentMissingIDReturnsNotFound(t *testing.T) {
	a := newTestApp(t)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/missing/content", nil, ""), http.StatusNotFound)

	// A deleted entry's ID returns 404 even if the file vanished from disk.
	entry := putFile(t, a, "default", "/gone.txt", "x", "text/plain")
	id := entry["id"].(string)
	if w := testRequest(t, a, http.MethodDelete, "/api/v1/default/files/"+id, nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", w.Code, w.Body.String())
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id+"/content", nil, ""), http.StatusNotFound)
}

func TestFileContentIsProjectScoped(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	bills := putFile(t, a, "bills", "/invoice.txt", "invoice", "text/plain")
	id := bills["id"].(string)

	assertAPIError(t, testRequest(t, a, http.MethodGet, "/api/v1/default/files/"+id+"/content", nil, ""), http.StatusNotFound)
	if w := testRequest(t, a, http.MethodGet, "/api/v1/bills/files/"+id+"/content", nil, ""); w.Code != http.StatusOK {
		t.Errorf("own-project content status = %d: %s", w.Code, w.Body.String())
	}
}

func TestStableHumanURLDownloadsRawPageBytes(t *testing.T) {
	a := newTestApp(t)
	md := putFile(t, a, "default", "/reports/week.md", "# Week\n\nBody", "")
	if md["kind"] != "page" {
		t.Fatalf("markdown kind = %v", md["kind"])
	}
	html := putFile(t, a, "default", "/reports/report.html", "<h1>Report</h1>", "")

	// A Markdown page renders (sanitised) at its path URL.
	rendered := testRequest(t, a, http.MethodGet, "/default/files/reports/week.md", nil, "")
	if rendered.Code != http.StatusOK || !strings.HasPrefix(rendered.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("rendered markdown = %d %q", rendered.Code, rendered.Header().Get("Content-Type"))
	}
	if !strings.Contains(rendered.Body.String(), "<h1>Week</h1>") {
		t.Errorf("rendered markdown body = %q", rendered.Body.String())
	}

	// An HTML page stays trusted executable content at its path URL.
	trusted := testRequest(t, a, http.MethodGet, "/default/files/reports/report.html", nil, "")
	if trusted.Code != http.StatusOK || trusted.Body.String() != "<h1>Report</h1>" {
		t.Fatalf("html path URL = %d %q", trusted.Code, trusted.Body.String())
	}
	if trusted.Header().Get("Content-Disposition") != "" {
		t.Errorf("HTML page should not download at its path URL: %q", trusted.Header().Get("Content-Disposition"))
	}

	// Every entry, including a page, downloads its raw stored bytes at the
	// stable ID URL, both the human and API forms.
	for name, item := range map[string]map[string]any{"markdown": md, "html": html} {
		id := item["id"].(string)
		path := item["path"].(string)
		want := diskFile(t, a, "default", strings.TrimPrefix(path, "/"))
		for label, url := range map[string]string{
			"human": "/default/files/id/" + id,
			"api":   "/api/v1/default/files/" + id + "/content",
		} {
			w := testRequest(t, a, http.MethodGet, url, nil, "")
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d: %s", name, label, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
				t.Errorf("%s %s Content-Disposition = %q, want attachment", name, label, got)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("%s %s X-Content-Type-Options = %q, want nosniff", name, label, got)
			}
			if w.Body.String() != want {
				t.Errorf("%s %s served %q, want raw bytes %q", name, label, w.Body.String(), want)
			}
		}
	}

	// The Markdown stable URL must not render: it returns the source.
	stable := testRequest(t, a, http.MethodGet, "/default/files/id/"+md["id"].(string), nil, "")
	if stable.Body.String() != "# Week\n\nBody" {
		t.Errorf("stable markdown URL = %q, want raw source", stable.Body.String())
	}
}

func TestPathURLNonPageDownloads(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/docs/data.csv", "a,b\n1,2\n", "text/csv")

	download := testRequest(t, a, http.MethodGet, "/default/files/docs/data.csv", nil, "")
	if download.Code != http.StatusOK {
		t.Fatalf("file path URL status = %d: %s", download.Code, download.Body.String())
	}
	if got := download.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
	if got := download.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := download.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("Content-Type = %q", got)
	}
	if download.Body.String() != "a,b\n1,2\n" {
		t.Errorf("body = %q", download.Body.String())
	}

	// Byte ranges work at the path URL too.
	partial := requestWithHeaders(t, a, http.MethodGet, "/default/files/docs/data.csv", map[string]string{"Range": "bytes=0-2"})
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "a,b" {
		t.Errorf("path range = %d %q", partial.Code, partial.Body.String())
	}
}

func TestStableHumanURLMissingAndMethodRestrictions(t *testing.T) {
	a := newTestApp(t)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/files/id/missing", nil, ""), http.StatusNotFound)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/files/id", nil, ""), http.StatusNotFound)
	if w := testRequest(t, a, http.MethodPost, "/default/files/id/anything", nil, ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST stable URL status = %d, want 405", w.Code)
	}
}

func TestFileDownloadFilenameAndInlinePreview(t *testing.T) {
	a := newTestApp(t)
	pdf := putFile(t, a, "default", "/docs/Electric_Bill.pdf", "%PDF-1.4 fake", "application/pdf")
	id := pdf["id"].(string)

	// The download disposition carries the real file name at both URLs.
	for label, url := range map[string]string{
		"path": "/default/files/docs/Electric_Bill.pdf",
		"id":   "/default/files/id/" + id,
	} {
		w := testRequest(t, a, http.MethodGet, url, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", label, w.Code, w.Body.String())
		}
		disp := w.Header().Get("Content-Disposition")
		if !strings.HasPrefix(disp, "attachment") || !strings.Contains(disp, "Electric_Bill.pdf") {
			t.Errorf("%s Content-Disposition = %q, want attachment with the file name", label, disp)
		}
	}

	// An inline request previews a safe type and still names the file.
	inline := testRequest(t, a, http.MethodGet, "/default/files/id/"+id+"?inline=1", nil, "")
	if got := inline.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") || !strings.Contains(got, "Electric_Bill.pdf") {
		t.Errorf("inline Content-Disposition = %q, want inline with the file name", got)
	}
	if got := inline.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("inline X-Content-Type-Options = %q, want nosniff", got)
	}

	// Plain text previews inline; HTML is active content and always downloads.
	txt := putFile(t, a, "default", "/docs/notes.txt", "hello", "text/plain")
	txtInline := testRequest(t, a, http.MethodGet, "/default/files/id/"+txt["id"].(string)+"?inline=1", nil, "")
	if got := txtInline.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("text inline Content-Disposition = %q, want inline", got)
	}
	html := putFile(t, a, "default", "/docs/page.html", "<h1>x</h1>", "text/html")
	forced := testRequest(t, a, http.MethodGet, "/default/files/id/"+html["id"].(string)+"?inline=1", nil, "")
	if got := forced.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("html inline Content-Disposition = %q, want attachment", got)
	}
}
