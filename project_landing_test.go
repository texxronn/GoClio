package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestProjectLandingRedirectsWhenIndexHTMLExists covers the automatic landing
// rule (section 66.5.1): a regular index.html at the project content root makes
// GET/HEAD /{project}/ redirect to the published file, while the file itself is
// served unchanged at its path URL.
func TestProjectLandingRedirectsWhenIndexHTMLExists(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/index.html", "<!doctype html><h1>Portal</h1>", "text/html")

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := testRequest(t, a, method, "/default/", nil, "")
		if response.Code != http.StatusFound {
			t.Fatalf("%s /default/ status = %d, want 302: %s", method, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Location"); got != "/default/files/index.html" {
			t.Errorf("%s /default/ Location = %q, want /default/files/index.html", method, got)
		}
	}

	// The published file is served at its own path URL, not redirected.
	page := testRequest(t, a, http.MethodGet, "/default/files/index.html", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Portal") {
		t.Errorf("GET /default/files/index.html = %d %q", page.Code, page.Body.String())
	}

	// A non-default project follows the same rule with its own project prefix.
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create bills status = %d: %s", w.Code, w.Body.String())
	}
	putFile(t, a, "bills", "/index.html", "<h1>Bills portal</h1>", "text/html")
	bills := testRequest(t, a, http.MethodGet, "/bills/", nil, "")
	if bills.Code != http.StatusFound || bills.Header().Get("Location") != "/bills/files/index.html" {
		t.Errorf("GET /bills/ = %d %q, want 302 /bills/files/index.html", bills.Code, bills.Header().Get("Location"))
	}
}

// TestProjectOverviewEscapeRoute covers /{project}/overview: it always renders
// the generated overview, whether or not the landing redirect is active, and it
// keeps the overview's method and segment handling.
func TestProjectOverviewEscapeRoute(t *testing.T) {
	a := newTestApp(t)

	// Without index.html both /{project}/ and /{project}/overview are the
	// generated overview.
	for _, path := range []string{"/default/", "/default/overview"} {
		response := testRequest(t, a, http.MethodGet, path, nil, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Open data browser") {
			t.Errorf("GET %s = %d, want the project overview", path, response.Code)
		}
	}

	putFile(t, a, "default", "/index.html", "<h1>Portal</h1>", "text/html")
	if response := testRequest(t, a, http.MethodGet, "/default/", nil, ""); response.Code != http.StatusFound {
		t.Fatalf("GET /default/ with index.html = %d, want 302", response.Code)
	}
	escape := testRequest(t, a, http.MethodGet, "/default/overview", nil, "")
	if escape.Code != http.StatusOK || !strings.Contains(escape.Body.String(), "Open data browser") {
		t.Errorf("GET /default/overview = %d, want the project overview: %s", escape.Code, escape.Body.String())
	}

	// Method restrictions match the overview, and /overview takes no extra
	// segment.
	if w := testRequest(t, a, http.MethodPost, "/default/overview", nil, ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /default/overview = %d, want 405", w.Code)
	}
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/default/overview/extra", nil, ""), http.StatusNotFound)
}

// TestProjectOverviewEscapeRouteUnknownProject confirms an unknown project is
// still a 404 at the escape route.
func TestProjectOverviewEscapeRouteUnknownProject(t *testing.T) {
	a := newTestApp(t)
	assertAPIError(t, testRequest(t, a, http.MethodGet, "/nope/overview", nil, ""), http.StatusNotFound)
}

// TestProjectLandingRestoresOverviewAfterIndexDelete confirms the landing check
// is dynamic: deleting index.html through the API or WebDAV restores the
// overview at /{project}/.
func TestProjectLandingRestoresOverviewAfterIndexDelete(t *testing.T) {
	a := newTestApp(t)

	putFile(t, a, "default", "/index.html", "<h1>Portal</h1>", "text/html")
	if response := testRequest(t, a, http.MethodGet, "/default/", nil, ""); response.Code != http.StatusFound {
		t.Fatalf("GET /default/ with index.html = %d, want 302", response.Code)
	}
	if w := testRequest(t, a, http.MethodDelete, filesURL("default", "/index.html"), nil, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE /index.html status = %d: %s", w.Code, w.Body.String())
	}
	assertOverviewAtRoot(t, a, "after API delete")

	// Recreate it, then delete through WebDAV; the overview returns again.
	putFile(t, a, "default", "/index.html", "<h1>Portal</h1>", "text/html")
	if response := testRequest(t, a, http.MethodGet, "/default/", nil, ""); response.Code != http.StatusFound {
		t.Fatalf("GET /default/ after recreate = %d, want 302", response.Code)
	}
	a.webdavEnabled = true
	if w := davRequest(t, a, http.MethodDelete, "/api/v1/default/files/dav/index.html", "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("WebDAV DELETE index.html status = %d: %s", w.Code, w.Body.String())
	}
	assertOverviewAtRoot(t, a, "after WebDAV delete")
}

// TestProjectLandingIgnoresSubdirectoryIndex confirms the landing rule applies
// only to the exact content-root /index.html, never to a nested one.
func TestProjectLandingIgnoresSubdirectoryIndex(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/default/files/directories", map[string]any{"path": "/portal"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create /portal status = %d: %s", w.Code, w.Body.String())
	}
	putFile(t, a, "default", "/portal/index.html", "<h1>Nested</h1>", "text/html")

	assertOverviewAtRoot(t, a, "with only a nested index.html")

	// The nested file still serves at its own path.
	nested := testRequest(t, a, http.MethodGet, "/default/files/portal/index.html", nil, "")
	if nested.Code != http.StatusOK || !strings.Contains(nested.Body.String(), "Nested") {
		t.Errorf("GET /default/files/portal/index.html = %d %q", nested.Code, nested.Body.String())
	}
}

// TestDefaultProjectLandingViaBareRoot confirms a default project with
// index.html becomes the instance landing page through the bare "/" redirect.
func TestDefaultProjectLandingViaBareRoot(t *testing.T) {
	a := newTestApp(t)

	root := testRequest(t, a, http.MethodGet, "/", nil, "")
	if root.Code != http.StatusFound || root.Header().Get("Location") != "/default/" {
		t.Fatalf("GET / = %d %q, want 302 /default/", root.Code, root.Header().Get("Location"))
	}
	// Without index.html the default overview renders at the redirect target.
	if response := testRequest(t, a, http.MethodGet, "/default/", nil, ""); response.Code != http.StatusOK {
		t.Fatalf("GET /default/ = %d, want 200", response.Code)
	}

	putFile(t, a, "default", "/index.html", "<h1>Instance portal</h1>", "text/html")
	landing := testRequest(t, a, http.MethodGet, "/default/", nil, "")
	if landing.Code != http.StatusFound || landing.Header().Get("Location") != "/default/files/index.html" {
		t.Errorf("GET /default/ with index.html = %d %q, want 302 /default/files/index.html", landing.Code, landing.Header().Get("Location"))
	}
}

// TestPortalSubresourceContentTypes confirms a portal's CSS, JS and image
// subresources are served from /{project}/files/... with a correct Content-Type
// alongside nosniff and the attachment disposition (section 64.5), so a browser
// accepts them for <link>, <script> and <img>.
func TestPortalSubresourceContentTypes(t *testing.T) {
	a := newTestApp(t)
	putFile(t, a, "default", "/portal/site.css", "body{color:red}", "text/css")
	putFile(t, a, "default", "/portal/site.js", "console.log(1)", "text/javascript")
	putFile(t, a, "default", "/portal/app.mjs", "export default 1", "text/javascript")
	putFile(t, a, "default", "/portal/logo.png", "PNG", "image/png")

	for _, tc := range []struct{ path, want string }{
		{"/default/files/portal/site.css", "text/css"},
		{"/default/files/portal/site.js", "text/javascript"},
		{"/default/files/portal/app.mjs", "text/javascript"},
		{"/default/files/portal/logo.png", "image/png"},
	} {
		w := testRequest(t, a, http.MethodGet, tc.path, nil, "")
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d: %s", tc.path, w.Code, w.Body.String())
			continue
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.want) {
			t.Errorf("GET %s Content-Type = %q, want %q", tc.path, got, tc.want)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", tc.path, got)
		}
		if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
			t.Errorf("GET %s Content-Disposition = %q, want attachment", tc.path, got)
		}
	}
}

// TestGeneratedOverviewLinksPointToOverview confirms Clio-generated
// project-overview links use the /{project}/overview escape route rather than
// /{project}/, which a portal's index.html would capture.
func TestGeneratedOverviewLinksPointToOverview(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create bills status = %d: %s", w.Code, w.Body.String())
	}

	overview := testRequest(t, a, http.MethodGet, "/default/overview", nil, "")
	if overview.Code != http.StatusOK {
		t.Fatalf("GET /default/overview = %d: %s", overview.Code, overview.Body.String())
	}
	for _, want := range []string{`href="/default/overview"`, `href="/bills/overview"`} {
		if !strings.Contains(overview.Body.String(), want) {
			t.Errorf("default overview missing the escape-route link %q", want)
		}
	}

	// The shared page shell's brand link on any overview-adjacent page points
	// at the escape route.
	page := testRequest(t, a, http.MethodGet, "/default/data", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `href='/default/overview'>Clio</a>`) {
		t.Errorf("GET /default/data brand link = %d, want /default/overview: %s", page.Code, page.Body.String())
	}

	// A non-default overview keeps its single Home link to bare "/".
	nonDefault := testRequest(t, a, http.MethodGet, "/bills/overview", nil, "")
	if nonDefault.Code != http.StatusOK || !strings.Contains(nonDefault.Body.String(), `href="/">`) {
		t.Errorf("GET /bills/overview = %d, want the Home link to /: %s", nonDefault.Code, nonDefault.Body.String())
	}
}

// assertOverviewAtRoot fails unless GET /{project}/ renders the generated
// overview (not the landing redirect).
func assertOverviewAtRoot(t *testing.T, a *app, when string) {
	t.Helper()
	response := testRequest(t, a, http.MethodGet, "/default/", nil, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Open data browser") {
		t.Fatalf("GET /default/ %s = %d, want the project overview: %s", when, response.Code, response.Body.String())
	}
}
