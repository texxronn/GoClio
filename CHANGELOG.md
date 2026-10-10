# Changelog

Notable changes to GoClio are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and product versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Version identifiers are independent; see "Document, product and API versioning"
in [`SPEC.md`](SPEC.md). The API version (`v1`) changes only for incompatible
API changes, and the specification revision tracks the contract document rather
than the product.

## [Unreleased]

### Fixed

- Record list filters on `boolean` fields accept the query values `true` and
  `false` (case-insensitive) for `eq`, `ne` and `in`; previously every boolean
  filter was rejected with HTTP 422 (SPEC section 25.2).

## [2.3.0] - 2026-10-10

### Added

- `CLIO_MAX_UPLOAD_BYTES` configures the maximum individual file upload, with a
  100 MiB default (the fixed 16 MiB cap is gone), so large scanned PDFs,
  high-resolution photos and lossless audio can be stored. Uploads stream to
  disk instead of being buffered in memory; the ZIP per-entry limit is unchanged
  (SPEC sections 7.2 and 64.12).
- `GET /api/v1/health` reports a `files` count, and
  `GET /api/v1/health?deep=true` (and `/health?deep=true`) additionally runs
  `PRAGMA quick_check`; the default health check stays cheap (SPEC section 47).

### Changed

- Content writes are staged and atomic: bytes stream to a temporary file, text
  is extracted before the content lock is taken, and the bytes, catalog row and
  index row are committed together. A failed write leaves the previous bytes
  and catalog unchanged, and a half-finished WebDAV `PUT` stores nothing
  (SPEC section 64.4).
- Startup reconciliation is fast: an entry whose size and modification time
  match the catalog is treated as unchanged, so a normal rescan no longer
  re-hashes or re-extracts every file (including photos, audio and
  scanned/image-only PDFs). `POST .../files/rescan?full=true` re-hashes every
  entry and rebuilds its native index (SPEC sections 64.2, 64.4 and 64.6).
- File and directory copy stream instead of reading whole files into memory
  (SPEC section 64.4).
- The content types for common documents, photos and audio (`.pdf` and the
  audio formats `.flac`, `.mp3`, `.m4a`, `.ogg`, `.oga`, `.opus`, `.wav`) are
  pinned so they do not depend on the base image shipping a `/etc/mime.types`
  file (SPEC section 64.5).
- The built-in `records` indexes now lead with `project`, matching every record
  query; the pre-2.3 group-leading indexes are dropped (SPEC section 52).
- The built-in file explorer's staged upload tray now uses
  `CLIO_MAX_UPLOAD_BYTES` instead of a fixed 16 MiB client-side cap.

### Fixed

- Clio's own temporary files (`.clio-*`) and operating-system metadata
  (`.DS_Store`, `._*`, `Thumbs.db`, `desktop.ini`, `__MACOSX`) are never
  stored, listed, cataloged or indexed: writes to such names return `422`
  (`403` over WebDAV), reads return `404`, ZIP archives skip them instead of
  rejecting the upload, and existing catalog rows are removed by the next
  reconciliation while the files stay on disk (SPEC sections 36.1, 44.1, 64.2
  and 64.10).
- The HTTP server gains an idle timeout and a header-size cap, and JSON-sized
  request bodies are read under a deadline, so a client that declares a small
  body and stalls is cut off. Large uploads keep no server-side deadline for
  slow links (SPEC section 53).

### Security

- Cross-site POSTs to the API are refused. Every state-changing request (POST,
  PUT, PATCH, DELETE) with a foreign `Origin`/`Referer` now returns
  `403 forbidden`, and JSON endpoints reject `text/plain` and form encodings
  with `422` (SPEC section 54.4).
- A successfully verified credential is remembered: after one bcrypt check Clio
  accepts an identical `Authorization` header without re-running bcrypt, so
  authenticated pages and WebDAV clients no longer pay a bcrypt cost per
  request. No password is stored, and restarting clears the memo
  (SPEC section 54.1).

### Upgrade notes

- JSON API requests that send `Content-Type: text/plain` or a form encoding now
  return `422`; send `application/json` (a missing `Content-Type` is still
  accepted for existing scripts).
- The first start after upgrade adds the `content_entries.mtime` column and
  rebuilds the built-in record indexes; that first rescan hashes every file
  once. Later starts do not.
- Existing catalog rows for `.DS_Store`, `._*` and the other reserved names are
  removed on the first reconciliation; the files themselves stay on disk.
- `CLIO_MAX_UPLOAD_BYTES` is new with a 100 MiB default (the fixed 16 MiB cap is
  gone). Raise any reverse-proxy body limit (for example nginx
  `client_max_body_size`) to match.

## [2.2.0] - 2026-10-08

### Added

- The Data Browser's **New table** form gains type-specific field editors for
  `enum` fields (a comma-separated list of values, split and trimmed, order
  preserved, duplicates rejected) and `reference` fields (a collection selector
  plus a table selector populated through `GET …/groups/{group}/tables`). Enum
  values and reference targets are captured per field row: they are properties
  of the individual field definition in the individual table, with no shared
  enum or reference registry. `Clio.version` is now `1.6.0`.

## [2.1.1] - 2026-10-08

### Fixed

- Test-only: scoped the `bills` enrichment fingerprint in
  `TestEnrichmentIsProjectScoped` to its own project. The helper resolved paths
  from the unscoped app, so it hashed the `default` project's file and passed
  only when both writes landed in the same `size:mtime` second, making CI flaky.
  No product behaviour changed.

## [2.1.0] - 2026-10-08

### Added

- The file explorer at `/{project}/files` is redesigned as a classic file
  browser. The left pane is a **lazy-loading folder tree** (`role="tree"`, role
  treeitems with `aria-expanded`/`aria-selected`/`aria-level`, chevrons to
  expand and collapse, children fetched on first expand and cached, a "load
  more" node that pages a large folder, ancestors of the current path
  auto-expanded and highlighted, roving-tabindex keyboard navigation, and
  expansion state persisted per project in `sessionStorage`). The right pane is
  a **single uniform list** — directories first, then files, each sorted by
  name, with a folder or document icon, Name/Size and single-click selection and
  double-click to open. Uploads are **staged, not immediate**: choosing files or dropping them
  on the listing fills a staging tray (name, human-readable size, type, resolved
  target path, status) that rejects files over the 16 MiB limit, sanitizes
  path separators, marks an existing name "will replace", asks for confirmation
  before replacing, uploads sequentially with a per-file status and error, and
  refreshes the listing at the end. No API or route changed. There is no icon or
  grid view. `Clio.version` is `1.4.0`.
- The file explorer's right pane has a **right-click context menu**
  (Google-Drive style) instead of inline Rename/Delete buttons: `menu`/`menuitem`
  roles, opened by right-clicking a row at the pointer or by **Shift+F10** / the
  **ContextMenu** key for the selected row. A
  file menu offers **Open in new tab**, Download, Rename and Delete; a directory
  menu offers Open, **Open in new tab**, Rename and Delete. Open navigates a
  directory in place; **Open in new tab** opens the entry's canonical path URL in
  a new tab (so pages render and other files serve without forcing a download);
  Download (files only, omitted when there is no stable ID) uses the stable ID
  URL `/{project}/files/id/{id}`; Rename/Delete reuse the existing prompt and
  confirmation. The menu closes on outside click, Escape, scroll, blur or after
  an action; Up/Down move, Enter activates, and focus moves into the menu and
  returns to the row. The old `.fb-dirs` block and inline action buttons/kebab
  are removed.
- The file explorer's right pane gained a **Type** column (folders show
  "Folder"; common page types show "Markdown"/"HTML"; other files show their
  upper-cased extension). Every column header is a sort toggle (Name, Type, Size,
  Created, Modified) with an ▲/▼ marker and `aria-sort`; folders always stay at
  the top and each group sorts independently.
- The Data Browser can now **create collections and tables**. Toolbar **New
  collection** and **New table** buttons open an inline `browser-create` form
  (not a dialog): choose an existing collection or a new one, name the table,
  optionally label it, pick `record` (default) or `timeseries` (which asks for a
  timestamp field), and edit dynamic `name`/`type` field rows (add/remove). The
  simple types `string, text, integer, decimal, boolean, date, datetime, url`
  are offered; `enum` and `reference` need extra configuration and are a
  follow-up. Creation issues `POST /api/v1/{project}/data/groups` first only when
  a new collection name was given, then
  `POST /api/v1/{project}/data/groups/{group}/tables`; on success the browser
  refreshes, selects the new (empty) table and shows a confirmation, while an
  API error (`409`/`422`) is shown in the form and the form stays open. Client
  validation blocks an empty table name, a field-less table and invalid
  timeseries timestamp fields without a request, and all names/values are
  rendered as text. `Clio.version` is `1.5.0`.

### Changed

- The file-explorer folder tree now indents one clear step per level and draws
  classic vertical tree guides, uses larger accent-coloured expand/collapse
  chevrons, and shows a fixed spacer for folders with no subfolders so labels
  align. Presentation only; no behaviour or API change.
- The file-explorer folder tree is more compact (smaller type and a `1rem`
  indent per level). The right pane is one **uniform table** with **Name / Size /
  Created / Modified** columns: directories first (folder icon, en-dash size and
  timestamps), then files (document icon, human size, `created_at`/`updated_at`
  rendered as `YYYY-MM-DD HH:MM`). Clicking a column header sorts by it (folders
  stay first; the active column shows an ▲/▼ marker). The `Kind` column, the
  redundant full path, the explicit "download" link, and the per-row kebab
  column are gone; Rename and Delete for both folders and files live only in the
  right-click context menu.

### Changed

- Only the default project exposes cross-project navigation (section 66.1). The
  projects manager on the overview (`Clio.Projects.mount`), the server-rendered
  project list and the data-browser/file-explorer project switchers are now
  rendered only for `default`. In any other project the single cross-project
  affordance is a **Home** link to bare `/` (which redirects to `/{default}/`),
  present in the nav, the overview and both toolbars; a non-default project no
  longer lists or links to a sibling project. `Clio.Projects.mount` is robust
  when mounted outside `default` and then offers only the Home link. No routes
  changed.

### Fixed

- The Data Browser no longer renders a **duplicate theme toggle**: it relied on
  its own `.browser-theme-toggle` (and wrote `data-theme`) in addition to the
  shared nav toggle in `pageShell`, so the page showed two toggles that could
  disagree. The Data Browser's own toggle, its `Clio.Theme.mount` call and the
  now-dead `.browser-theme-toggle` CSS are removed; the single shared
  `.clio-theme-toggle` and the one `Clio.Theme` controller over the shared
  `clio-theme` key remain.
- The file explorer no longer shows the browser's native "Choose Files" control next to the styled **Upload…** button: the hidden file input is now unconditionally hidden (`.fb-file-input` with `!important`). Only the styled button appears; selecting files still opens the picker and stages them.
- File downloads now send `Content-Disposition: attachment; filename="<name>"`, so the stable ID URL saves under the real file name instead of the opaque ID. An `?inline=1` request on a stable/API content URL previews types that are safe to display (images other than SVG, `application/pdf`, plain text, CSV and Markdown, audio and video) with `Content-Disposition: inline`; active content (HTML, SVG, scripts) and every other type still download. The file explorer's **Open in new tab** uses the inline preview for files and the rendered path URL for pages.

## [2.0.0] - 2026-10-08

### Added

- Projects registry (spec v1.6–v1.7): the implicit `default` project and the
  `GET`/`POST /api/v1/projects` and `GET`/`DELETE /api/v1/projects/{project}`
  API. `POST` rejects reserved names (`api`, `health`, `help`, `projects`,
  `assets`, `favicon.svg`, `default`) with `422` and duplicates with `409`;
  `DELETE` removes only an empty project and never `default`.
- Project-scoped storage: `groups_meta`, `tables_meta`, `fields_meta` and
  `records` gain a `project` column with project-scoped primary and foreign
  keys. Existing databases are rebuilt on open and every row is assigned to
  `default`; no stored data is rewritten. Group and table names are now unique
  per project.
- `/health` and `/api/v1/health` report a `projects` count.
- Project-first routing (spec v1.7 §66): every data and content route now begins
  with the project. Instance routes stay unscoped (`/health`, `/help`,
  `/api/v1/health`, `/api/v1/help`, `/api/v1/projects`, `/assets/...`,
  `/favicon.svg`). Bare `/` and `/api/v1` redirect to `/{default}/` and
  `/api/v1/{default}`; an unknown project returns `404`.
- The data partition moved under `/api/v1/{project}/data/...` (metadata, groups,
  tables, fields, records) and `/{project}/data/...` (table UI, forms, data
  browser at `/{project}/data`). Every data query is scoped by project, and
  managed indexes include the project scope.
- The content tree moved into the per-project `files` partition under
  `/api/v1/{project}/files...` and `/{project}/files/...`, replacing the legacy
  root-namespace directory and page operations (see the files REST API and
  human-UI entries below). Content storage is partitioned per project. ClioJS
  is project-aware (`new Clio({ project })`).
- Content identity (spec v1.5 §64.2): replaced `content_page_times` with
  `content_entries` (`id`, `project`, `path`, `kind`, `content_type`, `size`,
  `sha256`, `created_at`, `updated_at`; unique `(project, path)`). Entry IDs are
  opaque and stable across replacement; creation preserves the ID and
  `created_at` and refreshes the size, hash and `updated_at`. Legacy
  `content_page_times` rows migrate to `default` entries with new IDs.
- Project-scoped content storage: the content root now holds one subtree per
  project under `content/{project}`. Content published before this change is
  moved into `content/default` once on open. Every project's tree is reconciled
  at startup (existing path keeps its ID, a new path gets a new ID, vanished
  paths are removed; symbolic links are never followed). A project with content
  is not empty and cannot be deleted.
- Files REST API (spec v1.7 §64.4/§66.7) under `/api/v1/{project}/files`:
  - `GET /files` lists the paged flat catalog with optional `prefix`,
    `content_type` and `kind` filters; `GET /files?path=...` returns the
    content-entry representation or a directory listing (paged children).
  - `GET /files/{id}` reads a page or file entry by its stable ID.
  - `PUT /files?path=...` creates or replaces raw bytes atomically, honors the
    declared `Content-Type` for files, preserves the ID on replace, and returns
    `201` for a new path and `200` for a replacement. The 16 MiB per-file limit
    (`413`) and path-traversal rules are enforced (spec v1.7 §64.12).
  - `POST /files/directories`, `DELETE /files?path=...` (subtree) and
    `DELETE /files/{id}` manipulate directories and entries, with `409` for
    file/directory type conflicts and a `409` when deleting the root.
  - `POST /files/move` renames or moves a file, page or directory and preserves
    every descendant's ID; `POST /files/copy` copies with new IDs; both reject a
    conflicting destination with `409` and a missing source with `404`.
  - `POST /files/rescan` reconciles the catalog with disk and reports added,
    removed and refreshed entries.
  - The path segment `id` is reserved directly under `files` so it cannot
    shadow the stable ID URL (spec v1.7 §66.5).
- File serving and stable URLs (spec v1.7 §64.3/§64.5/§66.9):
  - `GET /api/v1/{project}/files/{id}/content` and the human
    `/{project}/files/id/{id}` stream an entry's current raw bytes, regardless of
    its path and including pages. Both send `Content-Disposition: attachment` and
    `X-Content-Type-Options: nosniff`, and use `http.ServeContent` so `HEAD` and
    byte ranges work (modification-time conditionals are intentionally omitted;
    see Fixed).
  - Non-page files at their path URL now download with the same disposition and
    nosniff headers instead of being served inline. Markdown pages still render
    (sanitised) and HTML pages remain trusted executable content at their path
    URLs. A missing ID returns `404`.
- Native extraction and the full-text index (spec v1.7 §64.6): text-like content
  (`text/*` as well as `.md`, `.txt`, `.csv`, `.json`, `.html` and similar
  formats) is indexed natively — HTML with markup removed — and PDF text layers
  are extracted with the small pure-Go `github.com/ledongthuc/pdf` library.
  Image-only or malformed PDFs yield no native text; OCR remains out of scope
  and is handled by sidecar agents (section 64.8). Extracted text is capped at
  1 MiB per entry and stored in the project-scoped `content_search` FTS5 table
  (`id`, `project`, `path`, `kind`, `source`, `title`, `body`). The index is
  populated on create/replace (files API, pages, ZIP uploads and copy), updated
  on move, rebuilt by rescan and dropped on delete. This adds a
  `github.com/ledongthuc/pdf` module dependency.
- Search API (spec v1.7 §64.7/§66.7): `GET /api/v1/{project}/search?q=...`
  searches the extracted text of pages and files in one project-scoped result
  set. Query terms are treated literally (quoted, AND-combined; FTS operators
  such as `NEAR`, `OR` and `*` cannot inject syntax or cause an error), an empty
  or over-long query returns `422`, and results are paged (`limit`, `offset`,
  default 100, maximum 1000). Each result is `id`, `path`, `kind`,
  `content_type`, `source`, a plain-text `snippet` with matched terms delimited
  by the private sentinels `⟦`/`⟧`, and a relevance `score`; results are ordered
  by relevance (bm25 with a title boost) and then deterministically by path and
  ID. `html.EscapeString` is applied before the sentinels become highlight
  markup, so indexed content cannot inject HTML.
- The content-entry representation now reports the real `indexed` value
  (whether the entry has native or agent text) instead of a hardcoded `false`.
- Agent enrichment API (spec v1.7 §64.8/§66.7): `GET`/`PUT`/`DELETE
  /api/v1/{project}/files/extraction`, addressing an entry by `?path=`, `?id=`
  or the files-style `/api/v1/{project}/files/{id}/extraction`. `PUT` stores
  agent-supplied text (`{"fingerprint", "text", "provider", "title"}`) with
  `source = agent:<provider>` so sidecar agents (for example OCR) make their
  text searchable through `GET /api/v1/{project}/search`; a missing or invalid
  field returns `422`. The fingerprint must match the current on-disk entry —
  `size:mtime` or the entry's `sha256` when known — or the write is refused with
  `409`, so a stale extraction cannot shadow changed content. Enrichment is keyed
  by entry ID and survives rename and move. `GET` returns the current extraction
  (agent or native) with its `source` and the current fingerprint; `DELETE`
  removes the agent row and rebuilds the native text so the entry falls back to
  native. Rewriting or rescan-refreshing an entry drops its stale agent text.
- Builds and tests require `-tags sqlite_fts5` so the cgo SQLite driver compiles
  in FTS5. The `Makefile`, `Dockerfile` and CI apply it; a binary built without
  the tag fails at startup when it creates `content_search`.
- `attachment` field type (spec v1.5 §64.9/§13.3): a field may declare
  `{"type": "attachment"}` with an optional `accept` UI hint (a string or a list
  of extensions/media types, normalized to a list and surfaced in field
  metadata). The stored value is a content-entry ID and is returned in the
  record representation as a JSON string; filtering, sorting and `distinct`
  operate on the ID string. On create, update and default application Clio
  validates that the ID names an entry in the **same project**, otherwise the
  request fails with `422`. A record that references an entry prevents deletion
  of that entry — or of a directory containing it — with `409 Conflict` through
  the files API, until the referencing values are cleared; there is no cascade
  deletion. The generated record form
  offers a choose control listing the project's files, and the record view links
  the attachment through `/{project}/files/id/{id}`. The link survives rename
  and move because it stores the stable ID.
- Human UI (spec v1.7 §35/§64.14/§66.5): the read-only collection data browser
  moved from `/{project}/collections/...` to `/{project}/data`, carrying the
  selected group, table and page in the URL query string, while the
  server-rendered table UI and forms stay at `/{project}/data/{group}/{table}`.
  A server-rendered file explorer shell at `/{project}/files` mounts
  `Clio.FileBrowser.mount(element)` over the files and search APIs, with a
  directory gutter, breadcrumbs, URL state and light actions (new folder,
  upload, rename/move, delete); `409 Conflict` responses from referenced
  entries are surfaced. `/{project}/` is now a project overview that links the
  data browser, the file explorer and search instead of redirecting to
  `/{project}/data`. A human search page at `/{project}/search` renders escaped
  result snippets over the search API. Names, paths and snippets are rendered
  as text; the search snippet is escaped before its private sentinels become
  highlight markup. `Clio.version` is now `1.1.0`; the file helpers gain
  `files`, `search`, `getFile`, `moveFile`, `copyFile` and `putFile`.
- Projects UI (spec v1.7 §65.5/§66.3/§66.5): the project overview at
  `/{project}/` now manages projects. It lists each project with a
  project-scoped link to `/{name}/`, highlights the current project, creates
  projects from `name`, optional `label`, `description` and `order`, and deletes
  empty projects, never offering delete for `default`. It is driven by the
  public `GET`/`POST /api/v1/projects` and `DELETE /api/v1/projects/{project}`
  API and surfaces API errors (`409` non-empty, `422` reserved/default) without
  crashing. A `<noscript>` fallback server-renders the project list with links,
  so the overview stays useful without JavaScript. No human `/projects` route is
  added (section 66.3): the manager lives inside the existing project-scoped
  pages and every URL it generates is `/{name}/`. `Clio.Projects.mount(element)`
  implements the manager, and the data browser and file explorer toolbars gain a
  compact project switcher that navigates the same sub-path (and query string)
  under the chosen project. `Clio.version` is now `1.2.0`.
- WebDAV (spec v1.7 §64.10/§66.8), opt-in with `CLIO_WEBDAV_ENABLED=true`
  (default off). When enabled, a project's content tree is mounted read/write at
  `/api/v1/{project}/files/dav/...` and `/{project}/files/dav/...` with the
  standard method set (`OPTIONS`, `PROPFIND`, `PROPPATCH`, `GET`, `HEAD`, `PUT`,
  `DELETE`, `MKCOL`, `COPY`, `MOVE`, `LOCK`, `UNLOCK`). Every mutation flows
  through the files API logic, so `PUT` preserves the entry ID on replace,
  `MOVE` preserves entry IDs (including descendants), `COPY` assigns new IDs,
  writes refresh timestamps and the text index, and `DELETE` returns
  `409 Conflict` when an attachment field still references the entry or its
  subtree. Locks are in-memory and do not survive a restart. The mount is
  behind the same authentication and HTTPS policy as every route, and when
  disabled it does not exist (returns `404`).
- Backup and restore (spec §57/§64.11/§65.8): `clio backup <dest>` snapshots the
  SQLite database with `VACUUM INTO` (one consistent, compact file that includes
  the FTS5 index) and copies the content tree, writing a `manifest.json`
  (`format`, `format_version`, `product_version`, `created_at`) last so an
  interrupted backup is incomplete. It refuses a destination that is not empty
  or a missing database. `clio restore <src>` validates the manifest, replaces
  the database and content directory, then reconciles the catalog with the
  restored files (rescan-on-restore) so content-entry IDs, entry timestamps and
  agent-supplied text are preserved, raw files are adopted and vanished files
  are dropped; it refuses a non-empty target data directory unless `--force` is
  given. The documented stop-copy procedure remains
  (`README.md`, `/help`, `CLIO_DATA_DIR`/`CLIO_DB`).
- Human state-changing requests are protected by a lightweight same-origin
  check: a `POST`/`PUT`/`PATCH`/`DELETE` to a human route whose present `Origin`
  or `Referer` host does not match the request host returns `403` before any
  mutation, while a request with no such header is unaffected. This closes the
  cross-site forged-form path when Basic credentials are cached by the browser.
  The JSON API is deliberately unchanged (section 54 treats API clients as
  non-browser).

### Changed

- **Route re-scope:** the collection data browser is now at `/{project}/data`
  with query-string state rather than `/{project}/collections/...`, and
  `/{project}/` serves the project overview rather than redirecting to
  `/{project}/data`. `t` and `collections` are no longer reserved project
  names, and content paths no longer reject former root names such as `api`,
  `health`, `help`, `assets`, `t`, `collections` or `favicon.svg`: content lives
  under `/{project}/files/`, where only the `id` and `dav` segments are
  reserved (spec v1.7 §66.4–§66.5).

- **Breaking route change:** the Page API and the directory facade are removed
  (spec v1.7 §66.2/§66.10). `POST /api/v1/{project}/files/pages`,
  `GET`/`DELETE /api/v1/{project}/files/pages`, and
  `GET`/`DELETE /api/v1/{project}/files/directories` no longer exist and return
  `404`; pages are `.md`/`.html` entries of the files partition. Replacements:
  create/replace a page with `PUT /api/v1/{project}/files?path=...` (raw bytes);
  read the stored source from `GET /api/v1/{project}/files/{id}/content` or
  `/{project}/files/id/{id}`; delete with `DELETE /api/v1/{project}/files?path=...`
  or `DELETE /api/v1/{project}/files/{id}`; list a directory with
  `GET /api/v1/{project}/files?path=...`. `POST
  /api/v1/{project}/files/directories` remains for JSON directory creation and
  for `application/zip` directory-tree uploads (spec §44), now served by the
  files API rather than the legacy directory handler. Pages render at their path
  URL (Markdown subset, sanitised) and HTML remains trusted executable content.
  ClioJS file helpers (`clio.page`, `clio.directory`, `clio.publishPage`,
  `clio.deletePage`, `clio.deleteDirectory`) now target the files partition and
  `Clio.Markdown` is unchanged.

### Fixed

- WebDAV `PUT` now bounds the request body before the handler buffers it: a body
  over the 16 MiB limit returns `413 body_too_large` for both a declared
  `Content-Length` and a chunked/unknown-length body, instead of buffering
  unbounded or failing at commit with `500` (spec §64.10/§64.12).
- Rescan detects change from the actual bytes, not the stored content type. A
  file uploaded with a custom declared `Content-Type` no longer looks changed,
  so a reconcile preserves its stored type and its agent enrichment; entries
  without a hash gain one, and a same-size external edit is detected by hash,
  refreshing the index and invalidating stale agent text (spec §64.2/§64.6/§64.8).
- Project names are validated as safe identifiers by project routing and by
  restore; the project content root is verified to stay inside the content
  directory. A database whose project name is `..` (or otherwise unsafe) is
  rejected and cannot escape the content root (spec §57/§65.2).
- A symlinked content directory or project content subtree is rejected for reads
  and writes rather than followed (spec §64.2).
- Moving or renaming an entry refreshes the search index row's `kind` and
  `title` from the new path and re-extracts native text when the extension
  changes, so search no longer reports a page as a file or matches the old name;
  a same-extension rename keeps agent enrichment (spec §64.6/§64.8).
- `clio backup` rejects a destination inside the content tree, which would
  otherwise copy its own output recursively (spec §57).
- `clio restore` requires the manifest's `database`/`content` to be the expected
  names and file types, stages the restored database and content in a temporary
  directory, opens, validates and reconciles the staged database, and only then
  swaps it into the target with rollback on failure. A malformed manifest can no
  longer destroy an existing target with `--force` (spec §57).
- Directory listings read and sort names first and stat/represent only the
  requested page window, so a large directory no longer builds every child to
  return one page (spec §23/§64.4).
- WebDAV `GET`/`HEAD` now send `X-Content-Type-Options: nosniff` for every entry
  and `Content-Disposition: attachment` for non-page content, matching the path
  URL protections without changing page responses (spec §64.5).
- File downloads no longer expose modification-time conditionals: `serveDownload`
  passes a zero modification time to `http.ServeContent`, so a download has no
  `Last-Modified`, an `If-Modified-Since` request returns `200` rather than
  `304`, and `HEAD` and byte ranges still work (spec §3.1/§64.5).
- A panic after a response is committed no longer appends a JSON `500` error to
  the already-sent body. The recovery writes the generic `500` only when nothing
  has been committed and always logs the panic (spec §56).
- Authentication failures no longer log the request path, and internal
  storage/filesystem errors are logged through a redacted description that keeps
  the operation and error class but drops the path-bearing message; request
  bodies, credentials, query strings and content are never logged
  (spec §54.4/§56).

### Documentation

- `SPEC.md` revised to v1.7.
  - v1.5 (section 64) added the content filesystem: stable content-entry IDs, a
    filesystem REST API with `/f/{id}` stable URLs, the `attachment` field type,
    native text extraction with a bounded SQLite full-text index, the search and
    enrichment APIs, optional WebDAV at `/dav`, and a ClioJS file explorer at
    `/files`. Sections 3.1, 13, 14.1, 32.3, 34, 35, 36, 41, 43, 46, 47, 49, 52,
    55.1, 56, 57, 58 and 62 were updated accordingly.
  - v1.6 (section 65) added namespaces as logical projects: project-scoped
    groups/tables and content, the projects API and project-scoped route
    prefixes, project-relative paths, reserved `/p`, and the `default` alias for
    existing routes. Sections 8, 9, 13.4, 17, 32.2, 32.3, 36, 47, 49, 58, 62 and
    64 were updated accordingly.
  - v1.7 (section 66) made project scoping mandatory and organized each project
    into `data` and `files` partitions: every application route begins with the
    project, `default` is explicit, bare `/` and `/api/v1` redirect to it, and
    the Directory and Page APIs are folded into the files partition. Sections
    32, 35, 38, 41, 49, 64 and 65 carry supersession notes where their URLs
    changed.
- `SPEC-CONFORMANCE.md` maps each v1.5–v1.7 area to its automated tests; nothing
  is left under *Pending*.
- `/help` and `/api/v1/help` now document the project-scoped routes, the
  `data`/`files` partitions, search, enrichment, WebDAV, the mandatory
  `-tags sqlite_fts5` build tag, `CLIO_WEBDAV_ENABLED`, and `clio backup` /
  `restore`. `README.md` documents the same feature set, routes, configuration
  and backup procedure, and `examples/` uses the project-scoped routes.
- `SPEC.md` sections 10 and 49 no longer describe the removed
  `/{project}/collections` browser; the data browser is `/{project}/data`
  (section 66.5). The `/collections` content-root reservation (section 32.3) is
  unchanged.
- Added the section 59 acceptance walkthrough test
  (`TestSection59AcceptanceWalkthrough`), which runs the end-to-end walkthrough
  against the project-scoped routes.
- `SPEC.md` section 66.10 now states explicitly that the removal of the Page API
  (section 41) and Directory API (section 38) is an intentional supersession
  under the existing `/api/v1/` contract and is not by itself an incompatible
  change requiring `/api/v2/`; where section 64.13's versioning rule conflicts
  with section 66, section 66 governs.
- `IMPLEMENTATION-CONTEXT.md` records the round-2 decisions and limitations:
  the supported filesystem is case-sensitive and normalization-stable (case-/
  normalization-insensitive hosts are unsupported), directory listings are paged
  with no hard per-directory cap beyond the ZIP limits, backup archives are
  trusted administrator input with no manifest checksum/signature, the WebDAV
  non-page download policy is intentional, and the same-origin and log-redaction
  choices above.

All of sections 64–66 are implemented; this release brings the product to the
`SPEC.md` v1.7 contract.

## [1.0.1] - 2026-09-29

### Fixed

- Table metadata `PATCH` now merges `fields` by name instead of replacing the
  complete field list. Adding one field no longer removes the other field
  definitions, their stored record values, or their table columns. Empty and
  partial `fields` lists are non-destructive.

### Added

- Explicit `remove_fields` for table metadata `PATCH`, still returning
  `409 Conflict` when a removed field has stored values.
- Build-time version injection (`-ldflags "-X main.version=..."`) with a
  `Makefile` and a Docker build argument, so a binary reports the release tag it
  was built from through `/api/v1/health`.

### Documentation

- `SPEC.md` sections 19 and 19.1, `SPEC-CONFORMANCE.md`, and `/help` now
  describe the additive field contract.

## [1.0.0] - 2026-09-29

### Added

- Initial product release of the Clio v1 contract: metadata-driven groups,
  tables, fields and records; time-series tables; record querying, filtering,
  sorting, paging, grouping and aggregation; the published content tree; the
  Markdown subset and renderer; ClioJS and the read-only data browser; optional
  authentication and transport security.
