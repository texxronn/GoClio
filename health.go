package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (a *app) health() map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	dbStatus := "ok"
	var check string
	if e := a.db.QueryRow(`PRAGMA quick_check`).Scan(&check); e != nil || !strings.EqualFold(check, "ok") {
		dbStatus = "error"
	}
	groups := a.count("groups_meta")
	tables := a.count("tables_meta")
	records := a.count("records")
	projects := a.count("projects")
	pages, dirs := int64(0), int64(0)
	_ = filepath.WalkDir(a.content, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			dirs++
		} else if strings.HasSuffix(d.Name(), ".md") || strings.HasSuffix(d.Name(), ".html") {
			pages++
		}
		return nil
	})
	status := dbStatus
	return map[string]any{"status": status, "version": version, "uptime_seconds": int64(time.Since(a.started).Seconds()), "memory": map[string]any{"alloc_bytes": mem.Alloc, "sys_bytes": mem.Sys, "heap_alloc_bytes": mem.HeapAlloc, "heap_inuse_bytes": mem.HeapInuse}, "database": map[string]any{"status": dbStatus}, "projects": projects, "groups": groups, "tables": tables, "records": records, "pages": pages, "directories": dirs}
}
func (a *app) count(table string) int64 {
	var n int64
	if e := a.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); e != nil {
		return 0
	}
	return n
}
func (a *app) healthJSON(w http.ResponseWriter) { writeJSON(w, 200, a.health()) }
func (a *app) healthHTML(w http.ResponseWriter) {
	info := a.health()
	database, _ := info["database"].(map[string]any)
	status := fmt.Sprint(info["status"])
	statusClass, statusLabel := "health-badge health-ok", "All systems operational"
	if status != "ok" {
		statusClass, statusLabel = "health-badge health-error", "Health needs attention"
	}
	raw, _ := json.MarshalIndent(info, "", "  ")
	stats := []struct{ label, value string }{
		{"API version", fmt.Sprint(info["version"])},
		{"Uptime", (time.Duration(info["uptime_seconds"].(int64)) * time.Second).Round(time.Second).String()},
		{"Projects", fmt.Sprint(info["projects"])},
		{"Collections", fmt.Sprint(info["groups"])},
		{"Tables", fmt.Sprint(info["tables"])},
		{"Records", fmt.Sprint(info["records"])},
		{"Published pages", fmt.Sprint(info["pages"])},
		{"Folders", fmt.Sprint(info["directories"])},
		{"Database", fmt.Sprint(database["status"])},
	}
	var cards strings.Builder
	for _, stat := range stats {
		cards.WriteString(`<div class="health-stat"><span>` + esc(stat.label) + `</span><strong>` + esc(stat.value) + `</strong></div>`)
	}
	body := workspacePageStyle + `<section class="health-page"><header class="health-heading"><div><p class="workspace-eyebrow">CLIO · SYSTEM STATUS</p><h1>Service health</h1><p>A live snapshot of this Clio instance.</p></div><span class="` + statusClass + `"><i></i>` + statusLabel + `</span></header><div class="health-grid">` + cards.String() + `</div><details class="health-raw"><summary>View raw health report</summary><pre>` + esc(string(raw)) + `</pre></details></section>`
	writeHTML(w, 200, a.pageShell(body, "Health"))
}

func (a *app) helpHTML() string {
	body := workspacePageStyle + `<article class="help-page"><p class="workspace-eyebrow">CLIO · DOCUMENTATION</p><div class="help-document">` + markdownHTML(a.helpText()) + `</div></article>`
	return a.pageShell(body, "Help")
}

const workspacePageStyle = `<style>
body{max-width:none;margin:0 auto;padding:0 1.4rem;background:var(--page);color:var(--text)}body>nav{display:flex;align-items:center;gap:.9rem;margin:0 calc(1.4rem * -1) 1.25rem;padding:.8rem 1.4rem;background:var(--surface);border-bottom:1px solid var(--line);font-size:.9rem}body>nav a{color:var(--muted)}body>nav a:first-child{font-weight:750;color:var(--text)}main{max-width:none!important}.workspace-eyebrow{margin:0 0 .4rem;color:var(--muted);font-size:.68rem;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.health-page,.help-page{max-width:1080px;margin:1.4rem auto 4rem}.health-heading{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1.4rem 1.5rem;border:1px solid var(--line);border-radius:12px;background:var(--surface)}.health-heading h1{margin:0;color:var(--text);font-size:1.7rem;letter-spacing:-.04em}.health-heading p:last-child{margin:.4rem 0 0;color:var(--muted);font-size:.88rem}.health-badge{display:inline-flex;align-items:center;gap:.5rem;padding:.55rem .75rem;border:1px solid #cbe8d8;border-radius:999px;background:#f0faf4;color:#26704d;font-size:.8rem;font-weight:650;white-space:nowrap}.health-badge i{width:.5rem;height:.5rem;border-radius:50%;background:#37a56c}.health-error{border-color:#f0caca;background:var(--surface)4f3;color:var(--danger)}.health-error i{background:#c7443e}.health-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(12rem,1fr));gap:.7rem;margin-top:.8rem}.health-stat{display:flex;flex-direction:column;gap:.65rem;padding:1rem 1.1rem;border:1px solid var(--line);border-radius:9px;background:var(--surface)}.health-stat span{color:var(--muted);font-size:.8rem}.health-stat strong{color:var(--text);font-size:1.25rem;letter-spacing:-.025em;overflow-wrap:anywhere}.health-raw{margin-top:.8rem;padding:1rem 1.1rem;border:1px solid var(--line);border-radius:9px;background:var(--surface)}.health-raw summary{color:var(--accent);font-size:.85rem;font-weight:650;cursor:pointer}.health-raw pre{margin:.9rem 0 0;padding:1rem;border:1px solid var(--line);border-radius:7px;background:var(--surface-raised);overflow:auto}.help-page{max-width:900px;padding:clamp(1.2rem,4vw,2.5rem);border:1px solid var(--line);border-radius:14px;background:var(--surface);box-shadow:0 5px 22px rgba(24,47,75,.04)}.help-document{color:var(--text);line-height:1.72}.help-document h1,.help-document h2,.help-document h3{color:var(--text);line-height:1.25;letter-spacing:-.03em}.help-document h1{margin:.15rem 0 1.25rem;font-size:clamp(1.8rem,4vw,2.35rem)}.help-document h2{margin:2rem 0 .6rem;padding-bottom:.4rem;border-bottom:1px solid var(--line);font-size:1.35rem}.help-document h3{margin:1.5rem 0 .5rem;font-size:1.05rem}.help-document p{margin:.7rem 0}.help-document li{margin:.35rem 0}.help-document a{color:var(--accent)}.help-document code{padding:.12em .3em;border-radius:4px;background:var(--surface-raised);color:var(--accent)}.help-document pre{padding:1rem;border:1px solid var(--line);border-radius:8px;background:var(--surface-raised);overflow:auto}.help-document pre code{padding:0;background:transparent}@media(max-width:700px){body{padding:0 .65rem}body>nav{margin:0 -.65rem .8rem;padding:.7rem .65rem;gap:.55rem;font-size:.82rem}.health-page,.help-page{margin:.65rem auto 2rem}.health-heading{align-items:flex-start;flex-direction:column;padding:1rem}.health-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.health-stat{padding:.8rem}.health-stat strong{font-size:1.05rem}.help-page{padding:1.1rem}}
</style>`

func (a *app) helpText() string {
	authStatus := "disabled (default)"
	if a.auth.enabled {
		authStatus = "enabled"
	}
	return `# Clio API v1

Clio is a self-hosted metadata-driven personal data and publishing service.
Canonical base URL: ` + a.baseURL + `
API root: /api/v1

Every application route is project-scoped (section 66). The project is the first
path segment after /api/v1/ or after the site root. Bare / redirects to
/{default}/ and bare /api/v1 redirects to /api/v1/{default}. The implicit project
` + "`default`" + ` always exists. The only instance-level routes are /health, /help,
/api/v1/health, /api/v1/help, /api/v1/projects and /assets/...; /api,
/api/v1/{unknown} and /{unknown} return 404.

## Projects and partitions
- GET/POST /api/v1/projects and GET/DELETE /api/v1/projects/{project}. A project
  has name, label, description and order. ` + "`default`" + ` is reserved and cannot be
  created, renamed or deleted; DELETE removes only an empty project (204) and a
  non-empty project returns 409. Reserved names: api, health, help, projects,
  assets, favicon.svg. Only the default project's overview at /default/ manages
  projects in the browser through this API; without JavaScript it server-renders
  the project list with links. Every other project offers only a Home link to /
  (the default project), so cross-project navigation stays on the default
  project. No human /projects route exists (section 66.3).
- A project is divided into two partitions. The ` + "`data`" + ` partition holds groups,
  tables, fields, records and metadata under /api/v1/{project}/data/... and
  /{project}/data/.... The ` + "`files`" + ` partition holds the unified content tree
  (directories, pages and files, with search, extraction, enrichment and WebDAV)
  under /api/v1/{project}/files... and /{project}/files/....

## Discovery and operations
- GET /api/v1/{project}/data/metadata describes groups, tables, fields and canonical URLs. The detailed metadata routes are /api/v1/{project}/data/metadata/groups, .../metadata/groups/{group}, .../metadata/groups/{group}/tables, and .../metadata/groups/{group}/tables/{table}.
- GET /api/v1/help returns this guide. /help is its HTML version.
- GET /health and GET /api/v1/health report health, version, uptime, memory, project count and resource counts.
- Groups are one-level containers. Tables have kind record or timeseries.
- CLIO_BASE_URL is the canonical public HTTP(S) origin used in returned URLs. CLIO_ADDR is the local host:port bind address.

## Groups, tables, fields, records
Create a group with POST /api/v1/{project}/data/groups and {"name":"pool","label":"Pool"}. Read it at GET /api/v1/{project}/data/groups/pool; list its tables at GET /api/v1/{project}/data/groups/pool/tables.
Create a table with POST /api/v1/{project}/data/groups/pool/tables and name, label, kind, fields, and timestamp_field for timeseries. GET and PATCH /api/v1/{project}/data/groups/pool/tables/{table} read and update metadata; DELETE removes an empty, unreferenced table. A metadata PATCH merges fields by name: a partial fields list adds or updates only the named fields and never drops the others. Remove fields explicitly with {"remove_fields":["name"]} when they have no stored values.
Field types: string, text, integer, decimal, boolean, date, datetime, enum, url, reference, attachment.
Fields support required, default, description, order, readonly, hidden, validation min/max, min_length/max_length and pattern.
Records support GET list/item, POST create, PATCH update, DELETE at /api/v1/{project}/data/groups/{group}/tables/{table}/records[/{id}]. PATCH omitted fields remain unchanged; explicit null clears nullable fields. Defaults apply on create; omitted nullable values are returned as null. Required, unknown and readonly fields are validated.
Decimal values are JSON strings by default; integer values are JSON integers; datetimes are RFC 3339 and normalized to UTC. Record reads accept decimal_format=number to return decimal fields as JSON numbers (canonical decimal text preserved) for consumers that require numeric JSON; decimal_format=string is the default.
References contain target record IDs and prevent deletion of referenced records/tables.
Attachments contain content-entry IDs from the same project and prevent deletion of a referenced file (or a directory containing one) until the value is cleared.
HTML views use /{project}/data/{group}/{table}, /new, /{id}, and /{id}/edit. Forms are metadata-driven. The Data Browser is at /{project}/data and carries group, table and page in the query string; its table view links to /{project}/data/{group}/{table}. The browser can also create a collection (POST /api/v1/{project}/data/groups) and a table (POST /api/v1/{project}/data/groups/{group}/tables) with an inline New collection / New table form; the field editor offers the scalar types plus enum (a comma-separated list of values) and reference (a target group and table in the same project), and a timeseries table declares a timestamp field. Enum values and reference targets are properties of the individual field definition in the individual table; there is no shared enum or reference registry. /{project}/health and /{project}/help mirror the instance pages for one project.

Example: POST /api/v1/{project}/data/groups/pool/tables with {"name":"readings","kind":"timeseries","timestamp_field":"timestamp","fields":[{"name":"timestamp","type":"datetime","required":true},{"name":"temperature","type":"decimal"}]}; then POST /api/v1/{project}/data/groups/pool/tables/readings/records with {"timestamp":"2026-09-27T12:00:00+10:00","temperature":"20.43"}. A record response contains the generated id, created_at, updated_at, and every defined field.

## Querying records
List records with limit and offset. Sort with sort={field}&order=asc|desc. Filters use filter.{field}[.{operator}], for example ?filter.cost.gte=100.
Operators: eq, ne, gt, gte, lt, lte, contains, in, isnull. Repeated in parameters are ORed; different filters are ANDed. Nulls sort last in both directions.
Distinct values: ?distinct=type. Aggregates: ?aggregate=count,cost:sum,cost:avg. Grouping: ?group_by=type&aggregate=count,cost:sum. Functions: count, sum, avg, min, max. Results are pageable where grouped/distinct.
Time-series records additionally accept RFC 3339 from (inclusive) and to (exclusive), and bucket=hour|day|week|month. Time comparisons and buckets use UTC; weeks start Monday; empty buckets are omitted.

## Content filesystem (files partition)
Pages and files are content entries with stable opaque IDs under /api/v1/{project}/files. List the paged catalog (prefix, content_type and kind filters) with GET /api/v1/{project}/files, read an entry or directory listing with GET /api/v1/{project}/files?path=/reports, and read a single entry by ID with GET /api/v1/{project}/files/{id}.
Create a directory with POST /api/v1/{project}/files/directories and {"path":"/reports"}; upload a directory tree as application/zip to the same route with ?path=/reports. Create or replace a page or file with PUT /api/v1/{project}/files?path=/reports/latest.md and the raw bytes as the request body (16 MiB limit). Rename or move with POST /api/v1/{project}/files/move (IDs preserved), copy with /files/copy (new IDs), and reconcile with /files/rescan. Delete with DELETE /api/v1/{project}/files?path=... (subtree) or DELETE /api/v1/{project}/files/{id}.
Markdown paths end .md and render as sanitised HTML when viewed; HTML paths end .html and are trusted executable content. Every other file downloads. Download an entry's stored bytes from /{project}/files/id/{id} or GET /api/v1/{project}/files/{id}/content; both send Content-Disposition: attachment and X-Content-Type-Options: nosniff and support HEAD and byte ranges. The human file explorer is at /{project}/files: a lazy-loading folder tree on the left, a compact detailed list on the right, and a staged upload tray. Choosing or dropping files stages them with a size and target-path check (16 MiB limit); nothing is uploaded until Upload is chosen, and a staged name that already exists is confirmed before it is replaced.
ZIP upload limits: 32 MiB compressed, 256 MiB expanded, 10,000 entries, 16 MiB per file, depth 32. Overwrite requires overwrite=true.
The asset /assets/clio-markdown.js exposes ClioMarkdown.render(source) for client-side Markdown display.

## Search, extraction and enrichment
- GET /api/v1/{project}/search?q=... searches the extracted text of pages and files across one project. Terms are literal (quoted and AND-combined; FTS5 operators cannot inject syntax), results are paged (limit default 100, max 1000), and each result carries id, path, kind, content_type, source, a plain-text snippet and a relevance score. A human search page is at /{project}/search.
- Native text-like formats and PDF text layers are indexed automatically, capped at 1 MiB per entry. OCR is not built in; a sidecar agent submits text with PUT /api/v1/{project}/files/extraction ({"path" or "id", "fingerprint", "text", "provider"}) so it becomes searchable with source agent:{provider}. GET reads the current extraction and DELETE falls back to native text. A fingerprint mismatch returns 409.

## WebDAV
When CLIO_WEBDAV_ENABLED=true (default false), a project's content tree is mounted read/write at /api/v1/{project}/files/dav/... and /{project}/files/dav/... with the standard WebDAV method set. When disabled the mount does not exist and returns 404. Locks are in-memory and do not survive a restart.

## Browser JavaScript client
- Load /assets/clio.js for the optional, dependency-free ClioJS v1 client (library version 1.6.0; also available at /assets/clio/v1/clio.js). It uses same-origin fetch() by default and HTTP Basic Authentication supported by the browser.
- Example: const clio = new Clio(); (scope another project with new Clio({project: "bills"})) const table = clio.table("pool", "measurements"); const recent = await table.query({limit: 20, sort: "timestamp", order: "desc"});
- ClioJS supports metadata, groups/tables, records, query paging/async iteration, files and directory listing, search, and the projects API. Clio.DataBrowser.mount(element) powers the built-in data browser at /{project}/data, which reads collections/tables and can create a collection and a table (New collection / New table) through the public API, including enum and reference fields; Clio.FileBrowser.mount(element) powers the file explorer at /{project}/files: a lazy, sessionStorage-persisted folder tree with keyboard navigation, a single uniform list with a right-click/kebab context menu (Open, Download for files, Rename, Delete), and a staged upload tray that only sends files when Upload is chosen. Only the default project exposes cross-project navigation: in the default project both toolbars carry a compact project switcher that navigates the same sub-path under another project, while any other project shows a single Home link to / and no switcher. Clio.Projects.mount(element) powers the project manager on the default project's /default/ overview and lists, creates and deletes projects (never deleting default); mounted elsewhere it shows only the Home link. Decimal values remain strings; errors expose status, code, and message.
- Load /assets/clio-markdown.js separately to use Clio.Markdown.render(page.content).

Content paths are canonical, case-sensitive UTF-8 paths relative to the project root. They reject traversal, empty segments, backslashes, control characters and the reserved segments id and dav directly under the project files tree. The files endpoints return entry metadata; the stored source is served as raw bytes from the stable ID URL, and pages render at their path URL.

## Safety, builds and examples
Group/table identifiers contain lowercase ASCII letters, digits, underscore and hyphen. Content paths reject traversal, backslashes, control characters and the reserved segments id and dav under the files tree. API errors use {"error":"validation_error","message":"..."}. SQL values are parameterized; arbitrary SQL is unavailable.
One SQLite database runs in WAL mode. Authentication is ` + authStatus + `. Enable it with CLIO_AUTH_ENABLED=true; then every route, including health, help, published content and static assets, requires HTTP Basic Authentication. API clients send standard Basic credentials. Generate a bcrypt password hash with ` + "`clio hash-password`" + ` and set CLIO_AUTH_USER and CLIO_AUTH_PASSWORD_HASH.
When authentication is enabled, HTTPS is required by default except for localhost and explicitly configured CLIO_TRUSTED_HTTP_NETWORKS. CLIO_TRUST_PROXY=true accepts X-Forwarded-Proto only from peers in CLIO_TRUSTED_PROXY_NETWORKS. Direct TLS can be enabled with CLIO_TLS_CERT and CLIO_TLS_KEY. Basic Authentication without TLS exposes credentials to network observers; use HTTPS on untrusted networks. Published HTML is trusted executable content and should only be created by trusted publishers.
Errors use JSON {"error":"validation_error","message":"..."}; invalid input returns 422 and resource conflicts return 409. Build and test with the mandatory ` + "`-tags sqlite_fts5`" + ` tag: the SQLite driver only compiles in FTS5 under that tag, and a binary built without it fails at startup when it creates the content_search table.
Back up by stopping Clio and copying the complete CLIO_DATA_DIR (clio.db, any -wal/-shm files, and content/). The commands ` + "`clio backup <dest>`" + ` and ` + "`clio restore <src> [--force]`" + ` snapshot and restore the database (VACUUM INTO) and content tree with a manifest, then reconcile the catalog, preserving content-entry IDs, timestamps and agent text. Like the stop-copy procedure, they must be run with Clio stopped: VACUUM INTO makes the database file consistent, but the content copy is not coordinated with live writers.

Query daily averages with GET /api/v1/{project}/data/groups/pool/tables/readings/records?bucket=day&aggregate=temperature:avg.
`
}

const markdownAsset = `(function(){
  const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  function inline(s){return esc(s).replace(/\x60([^\x60]+)\x60/g,'<code>$1</code>').replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>').replace(/\*([^*]+)\*/g,'<em>$1</em>').replace(/\[([^\]]+)\]\((https?:\/\/[^ )]+)\)/g,'<a href="$2">$1</a>');}
  function render(src){
    const out=[];let list=false,code=false,lines=[];
    const closeList=()=>{if(list){out.push('</ul>');list=false;}};
    for(const line of String(src).replace(/\r\n/g,'\n').split('\n')){
      if(line.startsWith(String.fromCharCode(96).repeat(3))){
        closeList();
        if(code){out.push('<pre><code>'+lines.map(esc).join('\n')+(lines.length?'\n':'')+'</code></pre>');lines=[];code=false;}
        else code=true;
        continue;
      }
      if(code){lines.push(line);continue;}
      const h=line.match(/^(#{1,6}) (.*)$/);
      if(h){closeList();const n=h[1].length;out.push('<h'+n+'>'+inline(h[2])+'</h'+n+'>');}
      else if(/^[-*] /.test(line)){if(!list){out.push('<ul>');list=true;}out.push('<li>'+inline(line.slice(2))+'</li>');}
      else{closeList();if(line.trim())out.push('<p>'+inline(line)+'</p>');}
    }
    closeList();
    if(code)out.push('<pre><code>'+lines.map(esc).join('\n')+(lines.length?'\n':'')+'</code></pre>');
    return out.join('');
  }
  window.ClioMarkdown={render};
})();`
