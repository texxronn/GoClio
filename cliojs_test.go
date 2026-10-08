package main

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

func TestClioJSAssetsAndHelp(t *testing.T) {
	a := newTestApp(t)
	for _, route := range []string{"/assets/clio.js", "/assets/clio/v1/clio.js"} {
		response := testRequest(t, a, http.MethodGet, route, nil, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d: %s", route, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
			t.Errorf("GET %s content type=%q", route, got)
		}
		for _, want := range []string{"root.Clio = Clio", "async function* iterateRecords", "ClioError", "DataBrowser", "FileBrowser", "apiVersion = \"v1\""} {
			if !strings.Contains(response.Body.String(), want) {
				t.Errorf("GET %s missing %q", route, want)
			}
		}
	}
	if response := testRequest(t, a, http.MethodPost, "/assets/clio.js", nil, ""); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /assets/clio.js status=%d, want 405", response.Code)
	}
	help := testRequest(t, a, http.MethodGet, "/api/v1/help", nil, "")
	for _, want := range []string{"/assets/clio.js", "/assets/clio/v1/clio.js", "new Clio()", "Clio.Markdown.render", "Clio.DataBrowser.mount", "Clio.FileBrowser.mount"} {
		if !strings.Contains(help.Body.String(), want) {
			t.Errorf("API help missing %q", want)
		}
	}
}

func TestClioJSBehaviorWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cmd := exec.Command(node, "testdata/cliojs_test.cjs", "assets/clio.js")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ClioJS behavior test failed: %v\n%s", err, output)
	}
}
