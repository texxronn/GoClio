package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestHumanStateChangingRequestsRequireSameOrigin covers the Origin/Referer
// gate in ServeHTTP: a cross-site human or API POST is rejected before it
// mutates anything, while a same-origin or header-less request is allowed
// (sections 54 and 64.14).
func TestHumanStateChangingRequestsRequireSameOrigin(t *testing.T) {
	a := newTestApp(t)
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})

	recordsURL := "/api/v1/default/data/groups/pool/tables/items/records"
	recordCount := func() int {
		var result struct {
			Page map[string]any `json:"page"`
		}
		testJSON(t, testRequest(t, a, http.MethodGet, recordsURL, nil, ""), &result)
		return int(result.Page["total"].(float64))
	}

	humanPost := func(path, origin, referer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("label=attempt"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if referer != "" {
			r.Header.Set("Referer", referer)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}

	recordPath := "/default/data/pool/items/new"

	// A mismatched Origin is rejected and leaves no record behind.
	if w := humanPost(recordPath, "http://evil.test", ""); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site Origin status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if got := recordCount(); got != 0 {
		t.Fatalf("cross-site Origin created %d record(s), want 0", got)
	}

	// A mismatched Referer is rejected too.
	if w := humanPost(recordPath, "", "http://evil.test/some/form"); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site Referer status = %d, want 403", w.Code)
	}
	if got := recordCount(); got != 0 {
		t.Fatalf("cross-site Referer created %d record(s), want 0", got)
	}

	// A matching Origin succeeds (a human form post redirects to the record).
	if w := humanPost(recordPath, "http://example.com", ""); w.Code != http.StatusSeeOther {
		t.Fatalf("same-origin status = %d, want 303: %s", w.Code, w.Body.String())
	}
	if got := recordCount(); got != 1 {
		t.Fatalf("same-origin request created %d record(s), want 1", got)
	}

	// An absent header keeps working, as non-browser clients and tests rely on.
	if w := humanPost(recordPath, "", ""); w.Code != http.StatusSeeOther {
		t.Fatalf("header-less status = %d, want 303", w.Code)
	}
	if got := recordCount(); got != 2 {
		t.Fatalf("header-less request created %d record(s), want 2", got)
	}

	// Folder creation is another human POST and is protected as well.
	if w := humanPost("/default/files/blocked", "http://evil.test", ""); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site folder status = %d, want 403", w.Code)
	}
	if w := testRequest(t, a, http.MethodGet, "/api/v1/default/files?path=%2Fblocked", nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("cross-site folder exists (status %d), want 404", w.Code)
	}

	// The JSON API is covered by the same gate, so a cross-site API POST is
	// refused before the handler runs (section 54).
	r := httptest.NewRequest(http.MethodPost, "/api/v1/default/data/groups", strings.NewReader(`{"name":"api-cross"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.test")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("JSON API cross-site status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// TestAuthFailureLogOmitsPath proves an authentication failure does not log the
// request path or query string (sections 54.4 and 56).
func TestAuthFailureLogOmitsPath(t *testing.T) {
	a := newTestApp(t)
	a.auth = authConfig{enabled: true, username: "admin", passwordHash: testPasswordHash(t), requireHTTPS: true}
	logs := captureLogOutput(t)

	response := authRequest(t, a, "/default/files/secret-page.md?token=never-log-this", "127.0.0.1:1234", false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	logged := logs.String()
	if !strings.Contains(logged, "authentication failure:") || !strings.Contains(logged, "reason=invalid_credentials") {
		t.Fatalf("authentication failure was not logged: %q", logged)
	}
	for _, leaked := range []string{"secret-page", "token", "never-log-this"} {
		if strings.Contains(logged, leaked) {
			t.Fatalf("authentication log leaked %q: %q", leaked, logged)
		}
	}
}

// TestInternalErrorLogOmitsPath proves errAPI logs a redacted description of a
// path-bearing error (sections 54.4 and 56).
func TestInternalErrorLogOmitsPath(t *testing.T) {
	logs := captureLogOutput(t)

	_ = errAPI(&os.PathError{Op: "open", Path: "/var/lib/clio/content/default/secret/private.txt", Err: syscall.ENOENT})
	_ = errAPI(&os.LinkError{Op: "rename", Old: "/srv/private.txt", New: "/srv/other.txt", Err: syscall.EPERM})

	logged := logs.String()
	if !strings.Contains(logged, "internal storage or filesystem error:") {
		t.Fatalf("internal error was not logged: %q", logged)
	}
	if !strings.Contains(logged, `path operation "open"`) || !strings.Contains(logged, "not_exist") {
		t.Fatalf("redacted error lost its operation/cause: %q", logged)
	}
	for _, leaked := range []string{"private.txt", "/var/lib", "/srv", "secret"} {
		if strings.Contains(logged, leaked) {
			t.Fatalf("internal error log leaked %q: %q", leaked, logged)
		}
	}
}
