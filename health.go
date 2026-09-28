package main

import (
	"encoding/json"
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
	return map[string]any{"status": status, "version": version, "uptime_seconds": int64(time.Since(a.started).Seconds()), "memory": map[string]any{"alloc_bytes": mem.Alloc, "sys_bytes": mem.Sys, "heap_alloc_bytes": mem.HeapAlloc, "heap_inuse_bytes": mem.HeapInuse}, "database": map[string]any{"status": dbStatus}, "groups": groups, "tables": tables, "records": records, "pages": pages, "directories": dirs}
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
	b, _ := json.MarshalIndent(a.health(), "", "  ")
	writeHTML(w, 200, a.pageShell("<h1>Clio health</h1><pre>"+esc(string(b))+"</pre>", "Health"))
}

func (a *app) helpText() string {
	authStatus := "disabled (default)"
	if a.auth.enabled {
		authStatus = "enabled"
	}
	return `# Clio API v1

Clio is a self-hosted metadata-driven personal data and publishing service.
Canonical base URL: ` + a.baseURL + `
API root: /api/v1

## Discovery and operations
- GET /api/v1/metadata describes groups, tables, fields and canonical URLs. The detailed metadata routes are /api/v1/metadata/groups, /api/v1/metadata/groups/{group}, /api/v1/metadata/groups/{group}/tables, and /api/v1/metadata/groups/{group}/tables/{table}.
- GET /api/v1/help returns this guide. /help is its HTML version.
- GET /health and GET /api/v1/health report health, version, uptime, memory and resource counts.
- Groups are one-level containers. Tables have kind record or timeseries.
- CLIO_BASE_URL is the canonical public HTTP(S) origin used in returned URLs. CLIO_ADDR is the local host:port bind address.

## Groups, tables, fields, records
Create a group with POST /api/v1/groups and {"name":"pool","label":"Pool"}. Read it at GET /api/v1/groups/pool; list its tables at GET /api/v1/groups/pool/tables.
Create a table with POST /api/v1/groups/pool/tables and name, label, kind, fields, and timestamp_field for timeseries. GET and PATCH /api/v1/groups/pool/tables/{table} read and update metadata; DELETE removes an empty, unreferenced table.
Field types: string, text, integer, decimal, boolean, date, datetime, enum, url, reference.
Fields support required, default, description, order, readonly, hidden, validation min/max, min_length/max_length and pattern.
Records support GET list/item, POST create, PATCH update, DELETE at /api/v1/groups/{group}/tables/{table}/records[/{id}]. PATCH omitted fields remain unchanged; explicit null clears nullable fields. Defaults apply on create; omitted nullable values are returned as null. Required, unknown and readonly fields are validated.
Decimal values are JSON strings; integer values are JSON integers; datetimes are RFC 3339 and normalized to UTC.
References contain target record IDs and prevent deletion of referenced records/tables.
HTML views use /t/{group}/{table}, /new, /{id}, and /{id}/edit. Forms are metadata-driven. The read-only Data Browser is at /collections/{group}/{table}; /collections opens the browser and selects the first available table.

Example: POST /api/v1/groups/pool/tables with {"name":"readings","kind":"timeseries","timestamp_field":"timestamp","fields":[{"name":"timestamp","type":"datetime","required":true},{"name":"temperature","type":"decimal"}]}; then POST /api/v1/groups/pool/tables/readings/records with {"timestamp":"2026-09-27T12:00:00+10:00","temperature":"20.43"}. A record response contains the generated id, created_at, updated_at, and every defined field.

## Querying records
List records with limit and offset. Sort with sort={field}&order=asc|desc. Filters use filter.{field}[.{operator}], for example ?filter.cost.gte=100.
Operators: eq, ne, gt, gte, lt, lte, contains, in, isnull. Repeated in parameters are ORed; different filters are ANDed. Nulls sort last in both directions.
Distinct values: ?distinct=type. Aggregates: ?aggregate=count,cost:sum,cost:avg. Grouping: ?group_by=type&aggregate=count,cost:sum. Functions: count, sum, avg, min, max. Results are pageable where grouped/distinct.
Time-series records additionally accept RFC 3339 from (inclusive) and to (exclusive), and bucket=hour|day|week|month. Time comparisons and buckets use UTC; weeks start Monday; empty buckets are omitted.

## Content tree and publishing
Content paths occupy the root namespace (/); table views use /t. Create/read/delete directories with GET /api/v1/directories?path=/reports, POST /api/v1/directories with {"path":"/reports"}, and DELETE /api/v1/directories?path=/reports.
Create/update a page with POST /api/v1/pages and {"path":"/reports/latest.md","content_type":"text/markdown","content":"# Report"}. Read original source with GET /api/v1/pages?path=/reports/latest.md; delete with DELETE and the path query. Markdown paths end .md; HTML paths end .html. HTML is trusted executable content.
Directory trees upload as application/zip to POST /api/v1/directories?path=/reports. Limits: 32 MiB compressed, 256 MiB expanded, 10,000 entries, 16 MiB per file, depth 32. Overwrite requires overwrite=true.
The asset /assets/clio-markdown.js exposes ClioMarkdown.render(source) for client-side Markdown display.

## Browser JavaScript client
- Load /assets/clio.js for the optional, dependency-free ClioJS v1 client (also available at /assets/clio/v1/clio.js). It uses same-origin fetch() by default and HTTP Basic Authentication supported by the browser.
- Example: const clio = new Clio(); const table = clio.table("pool", "measurements"); const recent = await table.query({limit: 20, sort: "timestamp", order: "desc"});
- ClioJS supports metadata, groups/tables, records, query paging/async iteration, directories and page source. Clio.DataBrowser.mount(element) powers the built-in read-only /collections/{group}/{table} browser. Decimal values remain strings; errors expose status, code, and message.
- Load /assets/clio-markdown.js separately to use Clio.Markdown.render(page.content).

Content paths are canonical, case-sensitive UTF-8 paths. They reject traversal, empty segments, backslashes, control characters and the reserved root names api, health, help, assets, t and collections. The page read API returns source text in JSON.content; it does not return rendered HTML.

## Safety and examples
Group/table identifiers contain lowercase ASCII letters, digits, underscore and hyphen. Content paths reject traversal, backslashes, control characters and reserved root paths including /collections. API errors use {"error":"validation_error","message":"..."}. SQL values are parameterized; arbitrary SQL is unavailable.
One SQLite database runs in WAL mode. Authentication is ` + authStatus + `. Enable it with CLIO_AUTH_ENABLED=true; then every route, including health, help, published content and static assets, requires HTTP Basic Authentication. API clients send standard Basic credentials. Generate a bcrypt password hash with ` + "`clio hash-password`" + ` and set CLIO_AUTH_USER and CLIO_AUTH_PASSWORD_HASH.
When authentication is enabled, HTTPS is required by default except for localhost and explicitly configured CLIO_TRUSTED_HTTP_NETWORKS. CLIO_TRUST_PROXY=true accepts X-Forwarded-Proto only from peers in CLIO_TRUSTED_PROXY_NETWORKS. Direct TLS can be enabled with CLIO_TLS_CERT and CLIO_TLS_KEY. Basic Authentication without TLS exposes credentials to network observers; use HTTPS on untrusted networks. Published HTML is trusted executable content and should only be created by trusted publishers.
Errors use JSON {"error":"validation_error","message":"..."}; invalid input returns 422 and resource conflicts return 409. Stop Clio before backing up or restoring the complete data directory, including the SQLite database and content/ tree.

Query daily averages with GET /api/v1/groups/pool/tables/readings/records?bucket=day&aggregate=temperature:avg.
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
