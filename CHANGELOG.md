# Changelog

Notable changes to GoClio are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and product versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Version identifiers are independent; see "Document, product and API versioning"
in [`SPEC.md`](SPEC.md). The API version (`v1`) changes only for incompatible
API changes, and the specification revision tracks the contract document rather
than the product.

## [Unreleased]

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
  browser at `/{project}/collections/...`). Every data query is scoped by
  project, and managed indexes include the project scope.
- The content tree moved under `/api/v1/{project}/files/{directories,pages}`
  and `/{project}/files/...`. These are the legacy directory and page operations
  under the files partition until the unified files API lands; storage is not yet
  partitioned per project. ClioJS is project-aware (`new Clio({ project })`).
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
    `X-Content-Type-Options: nosniff`, and use `http.ServeContent` so `HEAD`,
    byte ranges and conditional requests work.
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

### Changed

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
- `SPEC-CONFORMANCE.md` records the v1.5–v1.7 areas as pending coverage.

This is a specification change only; none of sections 64–66 is implemented yet.

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
