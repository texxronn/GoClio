package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAPIStateChangingRequestsRequireSameOrigin verifies the same-origin gate
// and the JSON content-type rule on the API (sections 50 and 54).
func TestAPIStateChangingRequestsRequireSameOrigin(t *testing.T) {
	a := newTestApp(t) // baseURL is http://clio.test; httptest Host is example.com
	createTestGroup(t, a, "pool")
	createTestTable(t, a, "pool", map[string]any{"name": "items", "fields": []any{
		map[string]any{"name": "label", "type": "string"},
	}})
	recordsURL := "/api/v1/default/data/groups/pool/tables/items/records"
	post := func(target, origin, contentType, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}

	// Cross-site Origin is refused before any handler runs.
	w := post(recordsURL, "http://evil.test", "application/json", `{"label":"x"}`)
	assertAPIError(t, w, http.StatusForbidden)
	if !strings.Contains(w.Body.String(), `"forbidden"`) {
		t.Errorf("cross-site body = %s, want error code forbidden", w.Body.String())
	}
	// Opaque origins ("null", sandboxed frames) are refused too.
	assertAPIError(t, post(recordsURL, "null", "application/json", `{"label":"x"}`), http.StatusForbidden)
	// The instance-level projects API is covered as well.
	assertAPIError(t, post("/api/v1/projects", "http://evil.test", "application/json", `{"name":"evil"}`), http.StatusForbidden)

	// Same host as the request is accepted.
	if w := post(recordsURL, "http://example.com", "application/json", `{"label":"a"}`); w.Code != http.StatusCreated {
		t.Fatalf("same-host Origin status = %d: %s", w.Code, w.Body.String())
	}
	// The CLIO_BASE_URL host is accepted even when a proxy rewrote Host.
	if w := post(recordsURL, "http://clio.test", "application/json", `{"label":"b"}`); w.Code != http.StatusCreated {
		t.Fatalf("base-URL Origin status = %d: %s", w.Code, w.Body.String())
	}
	// Non-browser clients send no Origin and keep working, with or without a
	// Content-Type header.
	if w := post(recordsURL, "", "", `{"label":"c"}`); w.Code != http.StatusCreated {
		t.Fatalf("no Origin, no Content-Type status = %d: %s", w.Code, w.Body.String())
	}

	// Form encodings never reach a JSON endpoint, whatever the origin.
	for _, contentType := range []string{"text/plain", "text/plain; charset=utf-8", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		assertAPIError(t, post(recordsURL, "", contentType, `{"label":"d"}`), http.StatusUnprocessableEntity)
	}
}
