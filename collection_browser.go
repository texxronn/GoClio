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
:root{color-scheme:light;--page:#f6f8fb;--surface:#fff;--surface-raised:#fff;--text:#182230;--muted:#667085;--line:#dfe5ec;--accent:#2563a6;--accent-soft:#e8f1fb;--hover:#f2f5f8;--danger:#a12626}
:root[data-theme="dark"]{color-scheme:dark;--page:#111821;--surface:#18222e;--surface-raised:#202c39;--text:#e6edf5;--muted:#a3b1c2;--line:#344252;--accent:#8bbcff;--accent-soft:#263c55;--hover:#243140;--danger:#ffaaa5}
@media(prefers-color-scheme:dark){:root:not([data-theme]){color-scheme:dark;--page:#111821;--surface:#18222e;--surface-raised:#202c39;--text:#e6edf5;--muted:#a3b1c2;--line:#344252;--accent:#8bbcff;--accent-soft:#263c55;--hover:#243140;--danger:#ffaaa5}}
body{max-width:none;margin:0;padding:0 clamp(.75rem,1.8vw,1.75rem);background:var(--page);color:var(--text);font-size:14px}body>nav{position:sticky;top:0;z-index:2;display:flex;align-items:center;gap:.85rem;margin:0 calc(clamp(.75rem,1.8vw,1.75rem)*-1) .75rem;padding:.65rem clamp(.75rem,1.8vw,1.75rem);background:var(--surface);border-bottom:1px solid var(--line);font-size:.9rem}body>nav a, a{color:var(--accent)}main{max-width:none!important}main>h1{font-size:1.35rem;letter-spacing:-.025em;margin:.55rem 0}.browser-toolbar{display:flex;align-items:center;gap:.65rem;padding:.5rem 0;border-bottom:1px solid var(--line)}.browser-toolbar label{display:flex;align-items:center;gap:.5rem;margin:0;color:var(--muted);font-size:.85rem}.browser-toolbar select{width:auto;min-width:11rem}.browser-toolbar select,.browser-theme-toggle{padding:.4rem .6rem;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--text);font:inherit}.browser-theme-toggle{margin-left:auto;cursor:pointer}.browser-theme-toggle:hover{background:var(--hover)}.browser-layout{display:grid;grid-template-columns:minmax(10rem,14rem) minmax(0,1fr);gap:1rem;padding-top:.75rem;align-items:start}.browser-tables{border:1px solid var(--line);border-radius:8px;background:var(--surface);padding:.6rem}.browser-tables h2{font-size:.75rem;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);margin:.1rem .45rem .45rem}.browser-tables ul{list-style:none;padding:0;margin:0}.browser-tables a{display:block;padding:.42rem .5rem;text-decoration:none;border-radius:5px;font-size:.9rem;color:var(--text)}.browser-tables a:hover{background:var(--hover)}.browser-tables a[aria-current=page]{background:var(--accent-soft);color:var(--accent);font-weight:600}.browser-content{min-width:0}.browser-content h2{font-size:1.05rem;margin:.2rem 0 .25rem;letter-spacing:-.015em}.browser-status{padding:.35rem 0 .55rem;margin:0;color:var(--muted);font-size:.85rem}.browser-grid{overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.browser-grid table{min-width:100%;width:max-content;border-collapse:collapse}.browser-grid th,.browser-grid td{padding:.48rem .65rem;border:0;border-bottom:1px solid var(--line);text-align:left;font-size:.85rem}.browser-grid th{position:sticky;top:0;background:var(--surface-raised);color:var(--muted);font-size:.75rem;font-weight:600;letter-spacing:.035em;white-space:nowrap}.browser-grid tbody tr:last-child td{border-bottom:0}.browser-grid tbody tr:hover{background:var(--hover)}.browser-grid td{max-width:28rem;overflow-wrap:anywhere;vertical-align:top}.browser-pagination{display:flex;align-items:center;justify-content:center;gap:.35rem;margin:.65rem 0;border:0;padding:0}.browser-pagination a,.browser-pagination button{display:inline-block;padding:.32rem .55rem;border:1px solid var(--line);border-radius:5px;background:var(--surface);color:var(--accent);font-size:.85rem;text-decoration:none;cursor:pointer}.browser-pagination a:hover{background:var(--hover)}.browser-pagination [aria-current=page]{background:var(--accent);border-color:var(--accent);color:var(--surface)}.browser-error{color:var(--danger)}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin-left:-.65rem;margin-right:-.65rem;padding-left:.65rem;padding-right:.65rem;gap:.55rem}.browser-layout{grid-template-columns:1fr;gap:.7rem}.browser-tables{padding:.45rem}.browser-tables ul{display:flex;flex-wrap:wrap;gap:.15rem}.browser-tables h2{margin-bottom:.3rem}.browser-toolbar select{min-width:0;max-width:60vw}}
</style>
<style>.browser-table-view{display:inline-flex;align-items:center;padding:.42rem .65rem;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--accent);font-size:.84rem;font-weight:600;text-decoration:none}.browser-table-view:hover{background:var(--hover)}</style>
<script src="/assets/clio.js"></script>
<script>Clio.DataBrowser.mount(document.getElementById("clio-data-browser"));</script>`
	writeHTML(w, http.StatusOK, a.pageShell(body, "Data Browser"))
}
