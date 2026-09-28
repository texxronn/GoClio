package main

import (
	"net/http"
	"strings"
)

// collectionBrowserUI serves the client-side collection data browser shell.
// All data and navigation are loaded by ClioJS through the public API.
func (a *app) collectionBrowserUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, methodNotAllowed())
		return
	}
	path := ""
	if r.URL.Path != "/collections" {
		path = strings.TrimPrefix(r.URL.Path, "/collections/")
	}
	parts := splitPath(path)
	if r.URL.Path != "/collections" && len(parts) > 2 {
		writeAPIError(w, missing("Page"))
		return
	}
	if len(parts) > 0 {
		if _, err := validIdentifier(parts[0], "group"); err != nil {
			writeErr(w, err)
			return
		}
	}
	if len(parts) > 1 {
		if _, err := validIdentifier(parts[1], "table"); err != nil {
			writeErr(w, err)
			return
		}
	}
	body := `<h1>Data Browser</h1>
<div id="clio-data-browser" aria-live="polite"></div>
<noscript>The Clio Data Browser requires JavaScript.</noscript>
<style>
main{max-width:1200px!important}.browser-toolbar{display:flex;align-items:center;gap:.75rem;padding:.75rem 0;border-bottom:1px solid #ddd}.browser-toolbar label{display:flex;align-items:center;gap:.5rem;margin:0}.browser-toolbar select{width:auto}.browser-layout{display:grid;grid-template-columns:minmax(12rem,18rem) minmax(0,1fr);gap:1.5rem;padding-top:1rem}.browser-tables{border-right:1px solid #ddd;padding-right:1rem}.browser-tables h2{font-size:1rem;margin:.5rem 0}.browser-tables ul{list-style:none;padding:0;margin:0}.browser-tables a{display:block;padding:.45rem .55rem;text-decoration:none;border-radius:3px}.browser-tables a[aria-current=page]{background:#e8f1fa;font-weight:600}.browser-grid{overflow:auto}.browser-grid table{min-width:100%;width:max-content;border-collapse:collapse}.browser-grid th{background:#f5f5f5;white-space:nowrap}.browser-grid td{max-width:28rem;overflow-wrap:anywhere;vertical-align:top}.browser-pagination{display:flex;align-items:center;justify-content:center;gap:.5rem;margin:1rem 0}.browser-pagination a,.browser-pagination button{display:inline-block;padding:.4rem .65rem;border:1px solid #ccc;border-radius:3px;background:white;color:#165d9c;text-decoration:none;cursor:pointer}.browser-pagination [aria-current=page]{background:#165d9c;color:white}.browser-status{padding:1rem 0}.browser-error{color:#8b1d1d}@media(max-width:700px){.browser-layout{grid-template-columns:1fr}.browser-tables{border-right:0;border-bottom:1px solid #ddd;padding:0 0 1rem}.browser-tables ul{display:flex;flex-wrap:wrap;gap:.25rem}}
</style>
<script src="/assets/clio.js"></script>
<script>Clio.DataBrowser.mount(document.getElementById("clio-data-browser"));</script>`
	writeHTML(w, http.StatusOK, a.pageShell(body, "Data Browser"))
}
