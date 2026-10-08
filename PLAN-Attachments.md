# Plan: a content filesystem (files, attachments, search, WebDAV)

**Status: PLAN — spec revision drafted, implementation not started.** `SPEC.md`
is now v1.5 and takes effect as a new section 64 (content filesystem,
attachments, search, and WebDAV), with targeted edits to sections 3.1, 13, 14.1,
32.3, 34, 36, 41, 46, 47, 49, 52, 55.1, 56, 57, 58 and 62, plus a
pending-coverage section in `SPEC-CONFORMANCE.md`. No application code has
changed. This document remains the design rationale; `SPEC.md` is now the
normative contract.

Clio today treats a **file** as a content-tree resource (`SPEC.md` §36) and
serves files verbatim at stable root URLs, but files can only be published as
part of a ZIP, nothing about them is stored in SQLite, and pages and files are
tracked by different mechanisms. This plan reframes the content tree as a
**filesystem**: one tree of directories, pages, and files; pages are files Clio
renders specially; an index catalogs the tree; records link to files; and the
tree is mountable over WebDAV.

## 1. Decisions

| Area | Decision |
| --- | --- |
| Framing | The content tree is a filesystem, not an attachment directory. |
| File identity | **Stable opaque file IDs** (inode-like), like record IDs. |
| Record links | New **`attachment`** field type storing a file ID. |
| Page links | Markdown renderers accept root-relative `/...` links. |
| Index scope | Metadata catalog **and** full-text search over extracted text. |
| Search engine | SQLite **FTS5**, same database. Requires `-tags sqlite_fts5`. |
| Search surface | Unified `GET /api/v1/search?q=...` over files and pages. |
| Extraction | Text-like formats plus **PDF text** natively. **OCR is not base functionality.** |
| OCR / deeper extraction | Sidecar agents enrich the index through a write-back API. |
| File serving | `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff`. |
| Stable file URL | `/f/{id}` (and `/f/{id}/{name}`), independent of path. |
| REST API | Unified filesystem REST API at `/api/v1/files` (directories, files, bytes, move, copy). |
| WebDAV | Read/write including rename/move, via `golang.org/x/net/webdav`, opt-in. |
| Browsing | ClioJS file explorer at `/files` with full light actions; path directory pages remain. |
| Backup | Database and content directory remain one backup unit; IDs and agent text make the database mandatory. |
| Contract | Deliberate `SPEC.md` revision + full docs/conformance update. |

Guiding principle: Clio provides generic primitives (a filesystem, a searchable
index, links, metadata). A "bill collector" is then an application of those
primitives, not a bundled feature — consistent with `SPEC.md` §63.

## 2. Concepts

- **Content filesystem** — the content directory: directories, pages
  (`.md`/`.html`), and files. Human URLs address it by path.
- **File ID** — a stable opaque identifier for every page/file entry, analogous
  to a record ID. It survives rename and move. The **path** is a mutable
  directory entry; the **ID** is the identity.
- **Metadata index** — a SQLite catalog of content entries keyed by ID
  (path, kind, content type, size, checksum, timestamps).
- **Text index** — an FTS5 table over text extracted from content, either
  natively or supplied by an agent. Rebuildable except for agent-supplied text.
- **Attachment** — a record field whose value is a file ID, exactly analogous to
  how a `reference` field stores a record ID.

## 3. Data model

### 3.1 `content_entries` (identity and metadata catalog)

Replaces `content_page_times` by covering both pages and files.

```sql
CREATE TABLE IF NOT EXISTS content_entries (
  id           TEXT PRIMARY KEY,   -- stable opaque id (same format as record ids)
  path         TEXT NOT NULL UNIQUE,
  kind         TEXT NOT NULL,      -- 'page' | 'file'
  content_type TEXT NOT NULL,
  size         INTEGER NOT NULL,
  sha256       TEXT,               -- computed on write; may be null for reconciled entries
  created_at   TEXT NOT NULL,      -- RFC 3339 UTC
  updated_at   TEXT NOT NULL
);
```

- Directories are structural and remain path-addressed; only pages and files
  get IDs (see §5 for directories under move).
- Existing `content_page_times` rows are migrated: each becomes a `page` entry
  with a freshly generated ID and `created_at`/`updated_at` preserved.
- Because IDs cannot be derived from disk, this table is **durable authoritative
  state**, not a pure cache. See §7.

### 3.2 `content_search` (FTS5 text index)

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS content_search USING fts5(
  id      UNINDEXED,
  path    UNINDEXED,
  kind    UNINDEXED,   -- 'page' | 'file'
  source  UNINDEXED,   -- 'native' | 'agent:<name>'
  title,
  body,
  tokenize = 'unicode61'
);
```

- Keyed by ID so rename/move only updates the `path` column.
- One row per indexed resource; a later native/agent extraction replaces the
  row and updates `source`.
- Extracted text is capped (proposed 1 MiB per resource, configurable). Bytes
  always remain on disk.
- FTS5 is a SQLite index, not an application cache, consistent with §4.5.

## 4. API

### 4.1 Filesystem REST API

The content filesystem is exposed as a REST API under `/api/v1/files`,
addressing directories, pages and files. Content paths use the `path` query
parameter (`SPEC.md` §20.1); IDs are simple path parameters, like record IDs.
The Directory and Page APIs become compatibility facades over it.

```
GET    /api/v1/files                       # flat catalog; prefix/content_type/kind filters, paging
GET    /api/v1/files?path=/...             # node: entry metadata or directory listing
GET    /api/v1/files/{id}                  # file/page entry by id
GET    /api/v1/files/{id}/content          # raw bytes; HEAD and ranges
PUT    /api/v1/files?path=/...             # create-or-replace file (raw body), 201 / 200
POST   /api/v1/files/directories           # create a directory
DELETE /api/v1/files?path=/...             # delete node; 409 if referenced (directory subtree)
DELETE /api/v1/files/{id}                  # delete file/page by id
POST   /api/v1/files/move                  # rename/move node; IDs preserved
POST   /api/v1/files/copy                  # copy node; new IDs
POST   /api/v1/files/rescan                # reconcile catalog with disk
```

- Path handling reuses `canonicalContentPath`/`contentPath` and traversal guards.
- `PUT` body is the raw file; media type from `Content-Type`, default
  `application/octet-stream`. Writes are atomic (temp + rename).
- Per-file limit aligns with the ZIP per-file limit (16 MiB); `413` when
  exceeded.
- Creating or replacing over a directory, or creating a directory over an
  existing node, returns `409`; replacing a file preserves its ID.
- `move` preserves IDs for an entry and for all descendants of a moved
  directory; `copy` assigns new IDs and does not copy enrichment.
- A file/page created by path may also be addressed by its ID; creating at an
  existing path keeps the existing ID (content replaced, identity preserved).

Example entry:

```json
{
  "id": "9f2c4e8a1b3d4f5061728394a5b6c7d8",
  "path": "/bills/2026-03.pdf",
  "kind": "file",
  "content_type": "application/pdf",
  "size": 184320,
  "sha256": "9f2c…",
  "created_at": "2026-09-27T12:00:00Z",
  "updated_at": "2026-09-27T12:00:00Z",
  "url": "https://clio.example.com/bills/2026-03.pdf",
  "stable_url": "https://clio.example.com/f/9f2c4e8a1b3d4f5061728394a5b6c7d8",
  "indexed": true
}
```

### 4.2 Search API

```
GET /api/v1/search?q=<terms>&limit=<n>&offset=<n>
```

```json
{
  "data": [
    {
      "id": "9f2c…",
      "path": "/bills/2026-03.pdf",
      "kind": "file",
      "content_type": "application/pdf",
      "source": "native",
      "snippet": "…amount due ⟦184.20⟧ on 2026-03-15…",
      "score": 1.42
    }
  ],
  "page": { "limit": 100, "offset": 0, "count": 1, "total": 1 }
}
```

- Ranked with FTS5 `bm25()`; snippet per hit; results carry `kind`.
- Query construction is **not** raw `MATCH` passthrough: split `q`, quote each
  term as a literal, join with `AND`, optionally add prefix matching.
  Empty/oversized queries return `422`.
- Snippet match delimiters use private sentinel characters (not HTML); clients
  escape, then substitute their own markup.

### 4.3 Agent write-back enrichment

```
GET    /api/v1/files/extraction?path=/...   # current entry + fingerprint
PUT    /api/v1/files/extraction?path=/...   # set/replace searchable text
DELETE /api/v1/files/extraction?path=/...   # drop the agent-provided entry
```

```json
{
  "path": "/bills/scan-001.pdf",
  "fingerprint": "184320:2026-09-27T12:00:00Z",
  "text": "Invoice 4421 … total due 184.20",
  "provider": "ocr:tesseract",
  "title": "Invoice 4421"
}
```

- `PUT` requires the caller's `fingerprint` to match the current file
  fingerprint (size + mtime, or `sha256` when known); mismatch → `409`, so stale
  extractions cannot shadow changed files. Enrichment is keyed by ID, so it
  survives rename/move.
- Stored with `source = "agent:<provider>"`, keeping OCR and future extraction
  outside the executable while still feeding `GET /api/v1/search`.
- Endpoint name (`extraction` vs `index`/`text`) is still open.

### 4.4 `attachment` field type

- Field definition: `{"name": "invoice", "type": "attachment"}`, optionally
  `required` and an `accept` hint for the UI.
- Stored value is a **file ID** string — flat and scalar, so
  `filter`/`sort`/`distinct` work unchanged, and consistent with `reference`
  returning a target ID.
- Validated on create, update, and default application; an ID that does not
  correspond to a `content_entries` row returns `422`.
- Record representation returns the ID. Clients resolve to `path`/`url` through
  the files API or link directly with `/f/{id}`; the UI renders `/f/{id}`.
- **Delete integrity** mirrors references (`SPEC.md` §13.4): deleting a file
  whose ID any record references returns `409` until the values are cleared.
  Deleting a record never deletes files (no cascade). Deleting a directory or
  replacing a file at a path is checked the same way.

## 5. Stable URLs and identity under move

- Every page/file has a path URL (`/bills/2026-03.pdf`) and a stable ID URL
  (`/f/{id}`). `/f/{id}/{name}` is accepted with a cosmetic name segment.
- **Move/rename updates `path` and keeps `id`.** Existing links via `/f/{id}`,
  `attachment` values, and enrichment rows all survive. The path URL changes,
  which is inherent to an explicit move.
- **Directories** remain path-addressed. Moving a directory rewrites the `path`
  of every descendant entry (prefix update) while preserving each descendant's
  ID; the directory's own URL changes. Giving directories IDs is a possible
  future refinement, not part of this revision.
- **Rename outside Clio** (directly on disk) is seen by reconciliation as a
  delete plus a create, so the file gets a new ID. Supported rename/move is via
  the API or WebDAV. An optional enhancement could match orphans by `sha256` to
  recover identity, but that is not committed.

## 6. Storage, consistency, limits

- **Filesystem authoritative for bytes; database authoritative for identity and
  indexes.** `content_entries` IDs cannot be rebuilt from disk; native metadata
  and text can.
- **Reconciliation.** On startup and on `POST /api/v1/files/rescan`: walk the
  content directory; keep the ID for any existing path, generate an ID for new
  paths, drop rows whose path vanished, refresh size/mtime, and (re)extract
  native text where absent. Removed entries lose their FTS row.
- **Staleness.** Fingerprint is `size:mtime` plus `sha256` when available.
  Native extraction refreshes when it changes; agent extractions are rejected on
  mismatch.
- **Limits.** Upload capped at 16 MiB (aligned with ZIP); indexed text capped
  per resource. Both proposed constants, not required config.
- **Build.** `-tags sqlite_fts5` added to `Makefile`, `Dockerfile`, and CI.

## 7. Backup and restore

Keeping everything in one data directory keeps backup easy, and this plan
preserves that: file bytes live in the content directory; identity, metadata,
and enrichment live in `clio.db`.

- **One unit.** A consistent backup captures `clio.db` (with `-wal`/`-shm`) and
  the content directory together. The content tree alone is not a complete
  backup.
- **Why the database is mandatory.** It now holds three things disk cannot
  reproduce: **file IDs** (so attachment links and `/f/{id}` URLs stay valid),
  page/entry timestamps, and **agent-supplied extraction text** (OCR, heavier
  PDF parsing). A rescan rebuilds the catalog and native text, but not IDs or
  agent enrichment.
- **Always-correct procedure.** Stop Clio, copy the entire data directory,
  start Clio. This remains the documented baseline (`SPEC.md` §57).
- **Hot backup.** SQLite `VACUUM INTO` yields a clean database snapshot without
  WAL sidecars; copying the content tree while running is safe because file
  writes are atomic temp-and-rename. For a strict point-in-time snapshot,
  quiesce or stop.
- **Proposed convenience command.** `clio backup <dest>` — quiesce content
  writes, `VACUUM INTO`, copy the content tree, write a manifest (product
  version, timestamp, counts). Restore is stop-and-replace plus a rescan.
  Optional; the stop-copy path suffices.
- **Restore.** Replace database and content directory, start Clio (or rescan).
  IDs, timestamps, and agent enrichment are restored; native extraction is
  refreshed. Confirm through `/health`.

## 8. WebDAV filesystem

- **Implementation.** `golang.org/x/net/webdav` (RFC 4918), the boring choice
  over a hand-rolled DAV subset. GET streams via `http.ServeContent` for range
  support; the handler uses `webdav.NewMemLS()` for locking.
- **Mount and enablement.** Mount at a reserved root `/dav`, mapping
  `/dav/<path>` to content `<path>`. Gated by `CLIO_WEBDAV_ENABLED` (default
  off), behind the §54 auth gate and HTTPS policy.
- **Shared write path.** A wrapper `webdav.FileSystem` over the content
  directory centralizes canonical-path validation, traversal/no-symlink
  enforcement, limits (per-file, depth, entry count), index and timestamp
  updates, and reference-integrity checks, so the API, ZIP upload, and DAV all
  mutate the tree through one code path.
- **Method mapping.**
  - `PUT` writes and gets an ID (new) or keeps the existing ID (replace).
  - `MOVE` renames on disk and updates `path` in `content_entries` and
    `content_search`, preserving IDs; directory moves update descendant path
    prefixes.
  - `COPY` creates a new entry with a new ID.
  - `DELETE` enforces the same `409` when a record references the file, and
    recursively removes descendants for directories.
  - `MKCOL`/`PROPFIND` map to directory create/list.
  - `.md`/`.html` written over DAV become pages with timestamps and are rendered
    at their path URLs; the raw bytes remain available via `/f/{id}`.
- **Client caveats.** macOS Finder and Windows Explorer have DAV quirks and
  often expect locking; in-memory locks do not survive restart. rclone, curl,
  and davfs2 are the primary supported clients.
- **Security.** DAV exposes and can mutate the entire content tree, so it stays
  opt-in and behind auth; `/f/{id}` and `/dav/...` are added to reserved roots so
  content cannot shadow them.

## 9. Security

- **Serving.** Non-page files are served with `Content-Disposition: attachment`
  and `X-Content-Type-Options: nosniff`; `/f/{id}` does the same. HTML stays
  executable only through the trusted pages API (`SPEC.md` §55.1).
- **Extraction.** Reading files for text never leaves the content root; the
  existing `contentPath`/`within` guards are reused.
- **Search input.** No raw FTS5 `MATCH`; terms are quoted as literals. Snippets
  are untrusted text.
- **Enrichment and DAV.** Protected by the same auth gate; fingerprint checks
  prevent stale writes.
- **Logging.** File bodies, extracted text, and query strings are never logged.

## 10. UI

### 10.1 Forms and record views

- Generic forms gain an `attachment` control that links or uploads a file
  (multipart or a two-step flow, since current forms are urlencoded).
- Record and table views render attachment values via `/f/{id}`.

### 10.2 File-system browsing

A basic browser already exists: content paths render a breadcrumbed child list
with directories, pages and files and a "New folder" form, and `/collections`
is a ClioJS data browser for tables. This plan upgrades it into a proper
filesystem browser:

- Show each entry's kind, size, content type and modified time from the
  metadata catalog.
- Link files to `/f/{id}` (stable) and to their path URL; downloads use the
  section 64.5 disposition.
- Provide light actions: new folder, upload (multipart), rename/move, delete,
  with `409` feedback when an entry is referenced.
- Provide a search entry point over `GET /api/v1/search` with escaped snippets.
- Optionally show which records reference a file (reverse lookup).

Decided approach: add a ClioJS explorer at the reserved `/files` route, in the
style of `/collections`, exposing the full light-action set (create folder,
upload, rename/move, delete) plus search. The path-based directory pages remain
as the no-JavaScript browsing surface. WebDAV remains the tool for bulk
operations.

Caveats: the explorer's mutating actions widen the CSRF surface when
authentication is enabled (Basic Auth is sent automatically by browsers); the
path pages and record forms share this limitation, and CSRF protection is not
part of v1. The explorer uses the reserved `/files` route defined in SPEC
§64.14.

## 11. Health, metadata, help

- `/health` and `/api/v1/health` gain a `files` count (and optionally `indexed`).
- `/help` and `/api/v1/help` document files, stable IDs, `/f/{id}`, search,
  attachments, the enrichment API, WebDAV, and backup/restore.
- Metadata responses already describe fields; the new type appears there.

## 12. Specification changes

`SPEC.md` is frozen at v1.4; this is a deliberate revision (proposed v1.5),
additive under `/api/v1/`. Sections to update:

- §3.1 — replace "no renaming or moving" with explicit move semantics backed by
  stable file IDs.
- §13 / §13.3 / §14.1 — add the `attachment` field type storing a file ID.
- §32.3 — reserve `/f` and `/dav`.
- §36 / §37 / §45 — files as first-class, ID-addressed resources; path vs stable
  URLs; move/rename.
- §41 / §49 — API summary gains files, search, enrichment, and WebDAV.
- §46 — help content.
- §50 — reuse existing error codes; confirm `409` for referenced-file deletion.
- §51 / §52 — the metadata catalog and durable IDs; the bounded FTS5 index.
- §57 — database is mandatory (IDs, timestamps, agent text).
- §58 / §59 — test and acceptance additions, including a WebDAV section.
- `SPEC-CONFORMANCE.md` — new rows for files, IDs, search, attachments,
  extraction, WebDAV, and backup/restore.
- Product version bump; README and examples updated.

## 13. Testing

- Identity: IDs stable across `POST` replace and across move/rename; orphaned
  on-disk renames produce new IDs (documented behavior).
- Files API: create/replace/delete by path and ID, atomicity, traversal
  rejection, size limit, conflict against directories/pages.
- Reconciliation: drop/rename files on disk, rescan, catalog correctness.
- Search: ranking/snippets, literal-term safety, paging, empty/oversized `422`,
  unified file+page results, enrichment survives move.
- Attachments: validation, representation, delete-integrity `409`, directory
  delete and path-replace integrity.
- WebDAV: `OPTIONS`, `PROPFIND`, `PUT`, `GET` (range), `MKCOL`, `DELETE`,
  `MOVE`, `COPY`, `LOCK`/`UNLOCK`; method rejects when disabled; index and ID
  behavior through DAV; auth enforcement.
- Serving: attachment disposition and `nosniff` on `/` and `/f/{id}`.
- Rendering: Markdown root-relative links in both renderers; `javascript:`/
  `data:` rejection; snippet escaping.
- Backup/restore: restored database plus content tree reproduces IDs, page
  timestamps, and agent enrichment; rescan rebuilds native state.
- Health/help/metadata shape updates.

## 14. Phasing

0. **Spec text.** Write the v1.5 revision and conformance rows from this plan.
1. **Identity + catalog.** `content_entries` with IDs; migrate
   `content_page_times`; reconciliation; files read/delete by path and ID.
   Implement the existing Page API on this core so there is one storage path.
2. **Uploads and serving.** Single-file create/replace, limits, atomic write,
   download/nosniff, `/f/{id}`, streaming/range.
3. **Backup/restore.** Document stop-copy and rescan-on-restore; optionally add
   `clio backup` / `clio restore`.
4. **Native extraction.** Text-like formats plus PDF text; FTS5 and build tag.
5. **Search API.** `GET /api/v1/search` with safe parsing, ranking, snippets.
6. **Enrichment API.** Fingerprint-checked agent write-back.
7. **Attachments.** ID-based field type, validation, delete integrity, UI.
8. **Page links.** Root-relative Markdown links in server and client renderers.
9. **WebDAV.** Wrapper `FileSystem`, `/dav` mount, method handling including
   MOVE/COPY, opt-in flag.
10. **File explorer.** `/files` shell, `Clio.FileBrowser`, breadcrumbs and URL
    state, light actions, and search.
11. **Docs/conformance/examples/acceptance**, then `go test ./...`,
    `go vet ./...`, `go build`.

## 15. Future: the filesystem can subsume the Page API

If the content filesystem lands cleanly, user-published Markdown/HTML pages
become just entries with `kind = 'page'`, and the separate Page API becomes a
thin compatibility facade rather than a distinct subsystem.

What is genuinely subsumed:

- Page create/read/replace/delete by path — the files API already provides the
  same upsert and atomic-write semantics.
- Extension/content-type rules move to the filesystem: `.md`/`.html` classify
  as pages and the media type follows the extension.
- Page timestamps are already folded into `content_entries`.

What must remain (this is the actual value, not CRUD):

- Server-side rendering of the Markdown subset and the client Markdown asset
  (`ClioMarkdown.render`). Rendering is a presentation layer keyed by `kind`,
  not a resource type.
- Markdown safety and the HTML trust boundary (`SPEC.md` §55), which becomes
  more important because WebDAV lets a publisher drop an `.html` file in.
- A compatibility surface: keep `/api/v1/pages` as an alias so existing clients
  and ClioJS `clio.page(path)` keep working. Removing it is a breaking API
  change and belongs in a future `/api/v2`.

Design implication: build the unified content filesystem core in phase 1 and
implement the existing Page API on top of it, rather than maintaining two
storage paths. Then superseding pages is facade cleanup, not a migration.

Security nuance: any `.html` in the tree renders as trusted executable content
at its path URL; `/f/{id}` always serves raw bytes with a download disposition.
The trusted-publisher stance in §55.1 covers this, but the widened WebDAV write
surface makes documenting it essential.

Recommendation: unify the core now, keep the Page API as a compatibility facade,
and defer removal to a future `/api/v2` rather than breaking v1.

## 16. Explicitly out of scope

- OCR or any external binary dependency in the executable (agents handle it).
- External search services (Elasticsearch/Meilisearch/etc.), separate search DBs.
- Ranking tuning, synonyms, stemming configuration, query language, facets,
  highlighting UI, relevance analytics.
- Recovering identity for renames done outside Clio (via sha256 matching).
- Directory IDs / ID-stable directory URLs.
- File versioning, per-file sharing/permissions beyond §54, resumable/chunked
  uploads, virus scanning.
- Multi-file `attachment` fields (single ID per field in this revision).
- An online backup API beyond the optional `clio backup` convenience command.

## 17. Open items

- Endpoint name for enrichment (`/files/extraction` proposed).
- Indexed-text cap and per-file upload limit: constants vs optional settings.
- Whether `clio backup` / `clio restore` ships in this revision or the
  documented stop-copy path is enough for now.
- Whether pages join `content_search` from day one (currently yes).
- Supported WebDAV client matrix to document and test.
