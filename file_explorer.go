package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// fileExplorerUI serves the server-rendered shell for /{project}/files
// (section 64.14). It loads ClioJS and mounts Clio.FileBrowser over the files
// and search APIs. The noscript fallback lists the project root so the tree
// stays readable without JavaScript.
func (a *app) fileExplorerUI(w http.ResponseWriter, r *http.Request) {
	d, ae := a.directory("/")
	if ae != nil {
		writeErr(w, ae)
		return
	}
	children, _ := d["children"].([]map[string]any)

	var fallback strings.Builder
	fallback.WriteString(`<noscript><p>The file explorer requires JavaScript. The project root contains:</p>`)
	if len(children) == 0 {
		fallback.WriteString(`<p>No content yet.</p>`)
	} else {
		fallback.WriteString(`<ul>`)
		for _, child := range children {
			name := fmt.Sprint(child["name"])
			kind := fmt.Sprint(child["type"])
			fallback.WriteString(`<li><a href="` + htmlAttr(a.filesPath("/"+name)) + `">` + esc(name) + `</a> <span>` + esc(kind) + `</span></li>`)
		}
		fallback.WriteString(`</ul>`)
	}
	fallback.WriteString(`</noscript>`)

	body := `<h1>File explorer</h1>
<div id="clio-file-browser" aria-live="polite"></div>
` + fallback.String() + `
<style>
body{max-width:none;margin:0;padding:0 clamp(.75rem,1.8vw,1.75rem);background:var(--page);color:var(--text);font-size:14px}body>nav{position:sticky;top:0;z-index:2;display:flex;align-items:center;gap:.85rem;margin:0 calc(clamp(.75rem,1.8vw,1.75rem)*-1) .75rem;padding:.65rem clamp(.75rem,1.8vw,1.75rem);background:var(--surface);border-bottom:1px solid var(--line);font-size:.9rem}body>nav a,a{color:var(--accent)}main{max-width:none!important}main>h1{font-size:1.35rem;letter-spacing:-.025em;margin:.55rem 0}.fb-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:.6rem;padding:.5rem 0;border-bottom:1px solid var(--line)}.fb-toolbar form{display:flex;gap:.4rem;margin:0}.fb-toolbar input{padding:.4rem .55rem;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--text);font:inherit;max-width:22rem}.fb-toolbar button{padding:.4rem .65rem;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--text);font:inherit;cursor:pointer}.fb-toolbar button:hover{background:var(--hover)}.fb-status{padding:.4rem 0;margin:0;color:var(--muted);font-size:.85rem;min-height:1.2em}.fb-status.fb-error{color:var(--danger);font-weight:600}.fb-breadcrumbs{display:flex;flex-wrap:wrap;align-items:center;gap:.35rem;margin:.6rem 0;color:var(--muted);font-size:.85rem}.fb-breadcrumbs a{text-decoration:none}.fb-layout{display:grid;grid-template-columns:minmax(12rem,16rem) minmax(0,1fr);gap:.9rem;align-items:start}.fb-tree-pane{border:1px solid var(--line);border-radius:8px;background:var(--surface);padding:.5rem;min-width:12rem;overflow:auto;max-height:70vh}.fb-tree-heading{font-size:.72rem;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);margin:.1rem .4rem .45rem}.fb-tree{display:flex;flex-direction:column;gap:0;min-width:max-content}.fb-tree-item{display:flex;align-items:center;gap:0;border-radius:5px;padding:.05rem .2rem}.fb-tree-item:hover{background:var(--hover)}.fb-tree-current{background:var(--accent-soft)}.fb-tree-indent{display:inline-flex;flex:none;align-self:stretch}.fb-tree-guide{width:1rem;flex:none;border-left:1px solid var(--line)}.fb-tree-toggle{width:.9rem;min-width:.9rem;height:1.15rem;margin-right:.1rem;padding:0;border:0;border-radius:4px;background:transparent;color:var(--accent);font:inherit;font-size:.72rem;line-height:1;cursor:pointer}.fb-tree-toggle:hover{background:var(--accent-soft)}.fb-tree-loading{color:var(--muted)}.fb-tree-spacer{width:.9rem;min-width:.9rem;margin-right:.1rem;flex:none}.fb-tree-label{flex:1;text-align:left;border:0;background:transparent;color:var(--text);font:inherit;font-size:.8rem;padding:.1rem .25rem;border-radius:4px;cursor:pointer;white-space:nowrap}.fb-tree-label:hover{background:var(--hover)}.fb-tree-more-button{border:0;background:transparent;color:var(--accent);font:inherit;font-size:.78rem;cursor:pointer;padding:.1rem .25rem}.fb-row-icon{font-size:.95rem;line-height:1;flex:none}.fb-row-name{overflow-wrap:anywhere}.fb-sort{border:0;background:transparent;color:inherit;font:inherit;font-weight:600;cursor:pointer;padding:.1rem .2rem;border-radius:4px;white-space:nowrap}.fb-sort:hover{background:var(--hover)}.fb-date-cell{white-space:nowrap;color:var(--muted)}.fb-type-cell{color:var(--muted);white-space:nowrap}.fb-menu{position:fixed;z-index:30;min-width:9rem;padding:.25rem;border:1px solid var(--line);border-radius:8px;background:var(--surface);box-shadow:0 8px 24px rgba(16,24,40,.18)}.fb-menu[hidden]{display:none!important}.fb-menu-item{display:block;width:100%;text-align:left;border:0;background:transparent;color:var(--text);font:inherit;font-size:.85rem;cursor:pointer;padding:.35rem .6rem;border-radius:5px}.fb-menu-item:hover,.fb-menu-item:focus{background:var(--hover);outline:none}.fb-menu-danger{color:var(--danger)}.fb-list{overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.fb-list.fb-drop{outline:2px dashed var(--accent);outline-offset:-4px;background:var(--accent-soft)}.fb-list table{border-collapse:collapse;width:100%}.fb-list th,.fb-list td{padding:.25rem .5rem;border-bottom:1px solid var(--line);text-align:left;font-size:.82rem}.fb-list th{color:var(--muted);font-weight:600}.fb-list tbody tr:hover{background:var(--hover)}.fb-row-selected>td{background:var(--accent-soft)}.fb-list a{text-decoration:none}.fb-list a:hover{text-decoration:underline}.fb-empty{padding:1rem;color:var(--muted)}.fb-search-results{display:grid;gap:.5rem;padding:.6rem 0}.fb-result{padding:.6rem .7rem;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.fb-result a{font-weight:600;text-decoration:none}.fb-result p{margin:.35rem 0 0;color:var(--text);font-size:.88rem;line-height:1.5;overflow-wrap:anywhere}.fb-result mark{background:var(--accent-soft);color:var(--accent)}.fb-result .fb-result-meta{color:var(--muted);font-size:.75rem}.fb-staging{margin:.55rem 0;padding:.6rem .7rem;border:1px solid var(--line);border-radius:8px;background:var(--surface)}.fb-staging-heading{margin:0 0 .4rem;font-size:.75rem;text-transform:uppercase;letter-spacing:.07em;color:var(--muted)}.fb-staging-rows{display:grid;gap:.25rem;margin-bottom:.55rem}.fb-staging-row{display:grid;grid-template-columns:minmax(6rem,1.2fr) 5rem 8rem minmax(8rem,1.5fr) minmax(7rem,1.2fr) 1.6rem;gap:.5rem;align-items:center;font-size:.82rem;padding:.25rem .35rem;border:1px solid var(--line);border-radius:6px}.fb-staging-name{font-weight:600;overflow-wrap:anywhere}.fb-staging-size,.fb-staging-type,.fb-staging-target,.fb-staging-status{color:var(--muted);overflow-wrap:anywhere}.fb-staging-target{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.78rem}.fb-staging-row[data-replaces="true"] .fb-staging-status{color:var(--danger);font-weight:600}.fb-staging-row[data-status="error"] .fb-staging-status{color:var(--danger);font-weight:600}.fb-staging-remove{border:0;background:transparent;color:var(--danger);font-size:1rem;line-height:1;cursor:pointer;padding:0 .2rem}.fb-staging-actions{display:flex;gap:.5rem}.fb-staging-upload{padding:.4rem .7rem;border:1px solid var(--accent);border-radius:6px;background:var(--accent);color:var(--on-accent);font:inherit;font-weight:600;cursor:pointer}.fb-staging-upload:disabled{opacity:.5;cursor:not-allowed}.fb-upload-button{padding:.4rem .65rem;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--text);font:inherit;cursor:pointer}.fb-file-input{display:none!important}
</style>
<script>Clio.FileBrowser.mount(document.getElementById("clio-file-browser"), {uploadLimit: ` + strconv.FormatInt(a.uploadLimit(), 10) + `});</script>`
	writeHTML(w, http.StatusOK, a.pageShell(body, "File explorer"))
}

// searchUI serves the human search surface at /{project}/search (section
// 64.14): a server-rendered form and result list over the search API (section
// 64.7). Snippets are escaped before the private sentinels become <mark>, and
// paths and kinds are escaped text, so indexed content can never inject
// markup.
func (a *app) searchUI(w http.ResponseWriter, r *http.Request, s []string) {
	if len(s) != 0 {
		writeAPIError(w, missing("Page"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, methodNotAllowed())
		return
	}
	q := queryValues(r)
	query := strings.TrimSpace(first(q, "q"))

	var b strings.Builder
	b.WriteString(`<style>.search-page{max-width:820px;margin:1.2rem auto 4rem}.search-form{display:flex;flex-wrap:wrap;gap:.6rem;align-items:flex-end;margin:1rem 0;padding:1rem;border:1px solid var(--line);border-radius:12px;background:var(--surface)}.search-form label{display:flex;flex-direction:column;gap:.35rem;margin:0;color:var(--muted);font-size:.8rem;font-weight:600}.search-form input{box-sizing:border-box;width:min(30rem,80vw);padding:.55rem .65rem;border:1px solid var(--line);border-radius:7px}.search-form button{padding:.6rem .9rem;border:0;border-radius:7px;background:var(--accent);color:var(--on-accent);font:inherit;font-weight:650;cursor:pointer}.search-count{color:var(--muted);font-size:.9rem}.search-results{display:grid;gap:.6rem}.search-result{padding:.8rem .9rem;border:1px solid var(--line);border-radius:10px;background:var(--surface)}.search-result>a{font-weight:650;color:var(--accent);text-decoration:none;overflow-wrap:anywhere}.search-result>a:hover{text-decoration:underline}.search-meta{margin:.25rem 0 0;color:var(--muted);font-size:.75rem;text-transform:uppercase;letter-spacing:.05em}.search-snippet{margin:.45rem 0 0;color:var(--text);line-height:1.6;overflow-wrap:anywhere}.search-snippet mark{background:var(--accent-soft);color:var(--accent)}.search-error{color:var(--danger);font-weight:600}.search-empty{color:var(--muted)}</style><section class="search-page"><h1>Search</h1><form class="search-form" method="get" action="` + htmlAttr(a.projectPath("search")) + `"><label>Search pages and files<input type="search" name="q" value="` + esc(query) + `" placeholder="Search terms" autofocus></label><button type="submit">Search</button></form>`)
	if query != "" {
		limit, ae := intParam(q, "limit", 50, 1, 100)
		if ae != nil {
			a.writeSearchError(w, b.String(), ae)
			return
		}
		offset, ae := intParam(q, "offset", 0, 0, 1<<31-1)
		if ae != nil {
			a.writeSearchError(w, b.String(), ae)
			return
		}
		if len(query) > maxSearchQueryBytes {
			b.WriteString(`<p class="search-error">The search query is too long.</p></section>`)
			writeHTML(w, http.StatusUnprocessableEntity, a.pageShell(b.String(), "Search"))
			return
		}
		match := buildMatchQuery(strings.Fields(query), false)
		results, total, err := a.searchContent(match, limit, offset)
		if err != nil {
			writeErr(w, errAPI(err))
			return
		}
		b.WriteString(`<p class="search-count">` + fmt.Sprint(total) + ` result(s) for “` + esc(query) + `”.</p>`)
		if len(results) == 0 {
			b.WriteString(`<p class="search-empty">No pages or files matched.</p>`)
		}
		b.WriteString(`<div class="search-results">`)
		for _, item := range results {
			path := fmt.Sprint(item["path"])
			kind := fmt.Sprint(item["kind"])
			contentType := fmt.Sprint(item["content_type"])
			source := fmt.Sprint(item["source"])
			b.WriteString(`<article class="search-result"><a href="` + htmlAttr(a.filesPath(path)) + `">` + esc(path) + `</a><p class="search-meta">` + esc(kind) + ` · ` + esc(contentType) + ` · ` + esc(source) + `</p><p class="search-snippet">` + renderSnippetMarkup(fmt.Sprint(item["snippet"])) + `</p></article>`)
		}
		b.WriteString(`</div>`)
		if offset > 0 {
			b.WriteString(`<p><a href="` + htmlAttr(queryURL(a.projectPath("search"), q, map[string]string{"offset": strconv.Itoa(max(0, offset-limit))})) + `">← Previous</a></p>`)
		}
		if offset+len(results) < total {
			b.WriteString(`<p><a href="` + htmlAttr(queryURL(a.projectPath("search"), q, map[string]string{"offset": strconv.Itoa(offset + limit)})) + `">Next →</a></p>`)
		}
	}
	b.WriteString(`</section>`)
	writeHTML(w, http.StatusOK, a.pageShell(b.String(), "Search"))
}

func (a *app) writeSearchError(w http.ResponseWriter, prefix string, ae *apiError) {
	writeHTML(w, ae.Status, a.pageShell(prefix+`<p class="search-error">`+esc(ae.Message)+`</p></section>`, "Search"))
}
