package main

import (
	"net/http"
	"strings"
	"testing"
)

// noscriptSection returns the markup between the overview's <noscript> and
// </noscript> tags, where the project list is server-rendered.
func noscriptSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<noscript>")
	end := strings.Index(body, "</noscript>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("overview has no noscript fallback: %s", body)
	}
	return body[start:end]
}

func TestProjectOverviewManagesProjects(t *testing.T) {
	a := newTestApp(t)

	// Create a non-default project with a label that needs escaping.
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills", "label": "Bills & <Invoices>", "description": "Household"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}

	response := testRequest(t, a, http.MethodGet, "/default/", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /default/ status=%d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()

	// The mount point and the ClioJS manager are present.
	for _, want := range []string{`id="clio-projects"`, "Clio.Projects.mount", `<script src="/assets/clio.js">`, `<noscript>`} {
		if !strings.Contains(body, want) {
			t.Errorf("project overview missing %q", want)
		}
	}

	// The server-rendered fallback lists default and the new project with
	// project-scoped links.
	noscript := noscriptSection(t, body)
	for _, want := range []string{`href="/default/"`, "Default", `href="/bills/"`, "Bills &amp; &lt;Invoices&gt;"} {
		if !strings.Contains(noscript, want) {
			t.Errorf("noscript fallback missing %q: %s", want, noscript)
		}
	}

	// The default project is present but not deletable in the fallback: the
	// fallback carries links only and no delete control at all.
	if strings.Contains(noscript, "<button") || strings.Contains(strings.ToLower(noscript), "delete") {
		t.Errorf("noscript fallback must not offer deletion: %s", noscript)
	}

	// Labels are escaped, never rendered as HTML.
	if strings.Contains(body, "<Invoices>") {
		t.Errorf("project label rendered as raw HTML: %s", body)
	}

	// Every generated URL is project-scoped; the instance-level projects route
	// is never linked from the human UI (section 66.3).
	for _, forbidden := range []string{`href="/projects"`, `action="/projects"`, `href="/api/v1/projects`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("overview contains a non-project-scoped URL %q", forbidden)
		}
	}
}

func TestProjectOverviewWithOnlyDefaultProject(t *testing.T) {
	a := newTestApp(t)
	response := testRequest(t, a, http.MethodGet, "/default/", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /default/ status=%d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`id="clio-projects"`, "Clio.Projects.mount", `href="/default/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("single-project overview missing %q", want)
		}
	}
}

func TestProjectOverviewPanelOnNonDefaultProject(t *testing.T) {
	a := newTestApp(t)
	if w := testRequest(t, a, http.MethodPost, "/api/v1/projects", map[string]any{"name": "bills", "label": "Bills"}, "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	response := testRequest(t, a, http.MethodGet, "/bills/", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /bills/ status=%d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	noscript := noscriptSection(t, body)
	// Both projects are listed and each links to its own project-scoped URL.
	for _, want := range []string{`href="/default/"`, `href="/bills/"`} {
		if !strings.Contains(noscript, want) {
			t.Errorf("non-default overview fallback missing %q: %s", want, noscript)
		}
	}
}
