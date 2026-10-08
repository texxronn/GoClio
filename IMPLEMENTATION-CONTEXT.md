# Implementation context (living file)

> **Update this file at the start and end of every work session.** Keep it
> short and factual. It is the handoff that makes context compaction harmless:
> if chat history is lost, read this file and `IMPLEMENTATION-PLAN.md` and
> continue. Never rely on memory of previous sessions.

## Snapshot

- **Last updated:** 2026-10-08
- **Spec:** `SPEC.md` v1.7 (sections 64, 65, 66 are new; earlier URL sections carry supersession notes)
- **Plan:** `IMPLEMENTATION-PLAN.md`
- **Code baseline:** Phase 5 implemented; the filesystem is fully manipulable through `/api/v1/{project}/files` and content is served with stable URLs. Non-page content always downloads (`Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`) via `http.ServeContent` (HEAD, byte ranges); `/{project}/files/id/{id}` and `/api/v1/{project}/files/{id}/content` stream an entry's raw bytes (even a page), while Markdown/HTML pages still render at their path URLs. The legacy directory/page operations remain at `/files/{directories,pages}` until Phase 10.
- **Branch:** `phase-5-serving`
- **Last merged commit:** `a8da7b7` (Phase 4, PR #8)
- **Current phase:** Phase 5 complete (this commit)
- **Next action:** Phase 6 — native extraction and FTS5: add `-tags sqlite_fts5` to `Makefile`, `Dockerfile` and CI; add a `content_search` FTS5 table; native extraction for text-like formats and PDF text (cap 1 MiB); populate on write and rescan; drop on delete. Tests: extraction per type, cap, rebuild by rescan, FTS5 build.
- **Blockers:** none

## Decision log (locked — do not relitigate)

Design decisions already fixed by `SPEC.md` v1.5–v1.7:

- **Projects are logical scopes, not security boundaries.** One process, one SQLite database, one content root. `default` always exists.
- **Project scoping is mandatory in every route** (section 66). `/{project}` is the first segment after `/api/v1/` or the site root. Bare `/` → `/{default}/`; bare `/api/v1` → `/api/v1/{default}`.
- **Two partitions per project:** `data` (groups/tables/records/metadata) and `files` (unified tree: directories, pages, files, search, enrichment, WebDAV).
- **No separate pages partition or Page API; no Directory API.** Pages are `.md`/`.html` entries in the files tree, rendered by extension. Replaced by the files partition (section 66.2).
- **File identity is a stable opaque ID**, like record IDs; path is mutable; move preserves ID; copy gets a new ID. IDs are unique within a project; record IDs remain globally unique.
- **References and attachments stay within a project**; cross-project references are out of scope.
- **Attachment field stores a file ID**, not a path.
- **Non-page content is served as a download** with `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff`.
- **Full-text index uses SQLite FTS5**, native extraction for text-like formats and PDF text; **OCR is not in the executable** — sidecar agents enrich via a fingerprint-checked write-back API.
- **WebDAV is opt-in** (`CLIO_WEBDAV_ENABLED`, default off), exposed at both `/api/v1/{project}/files/dav/...` and `/{project}/files/dav/...`, read/write including rename/move.
- **Reserved project names:** `api`, `health`, `help`, `projects`, `assets`, `favicon.svg`; `default` is reserved.
- **Project representation URLs** follow section 66.5, not the superseded 65.4: `url` is `/{project}/` (the overview) and `api_url` is `/api/v1/projects/{project}`.
- **Delete status codes:** a non-empty project delete returns `409`; deleting the reserved `default` project returns `422`.
- **Project scope is a storage-key change, not a column add.** Phase 1 rebuilds `groups_meta`, `tables_meta`, `fields_meta` and `records` with `(project, …)` primary keys and project-scoped foreign keys, so names are unique per project. A legacy database migrates on open with every row assigned to `default`; no stored data is rewritten.
- **Backup must capture the database and content root together;** the database is mandatory (IDs, timestamps, agent text).
- **No migration/compatibility layer** for the old unscoped routes; update tests instead.
- **Routing (Phase 2):** `ServeHTTP` resolves the project before dispatch. Instance routes are `/health`, `/help`, `/api/v1/health`, `/api/v1/help`, `/api/v1/projects[/{project}]`, `/assets/...` and `/favicon.svg`. Bare `/` and `/api/v1` return `302` to `/{default}/` and `/api/v1/{default}`; `/{project}` returns `302` to `/{project}/data`. The data partition is `/api/v1/{project}/data/...` and `/{project}/data/...`; the collection browser is `/{project}/collections/...`.
- **Per-request scope is a shallow `app` copy.** `app.contentMu` is a pointer so `withProject` can copy the handler safely; data functions read `a.project` instead of taking a project argument. Every group/table/field/record query and every managed index includes `project`.
- **Temporary files-partition placement (until Phase 4/10):** the legacy directory and page operations are served at `/api/v1/{project}/files/directories` and `.../files/pages`, and the content UI at `/{project}/files/...`. ClioJS is project-aware and defaults to `default`.
- **Content storage layout:** the content root holds one subtree per project at `content/{project}`. Legacy content at the root is moved once into `content/default` on open (`migrateContentLayout`), detected by the absence of `content/default` and of any top-level directory named after a project. The layout is an implementation detail (section 65.7).
- **Content entries:** `content_entries` holds `id`, `project`, `path`, `kind`, `content_type`, `size`, `sha256`, `created_at`, `updated_at` with unique `(project, path)`. IDs are opaque (`newID`). Replacement preserves `id` and `created_at` and refreshes `size`, `sha256` and `updated_at`; reconciliation assigns new IDs to new paths, keeps existing IDs, drops vanished paths, and never follows symbolic links. `sha256` is populated on API writes and left empty on reconciled entries (section 64.2). A project is not empty — and cannot be deleted — when it has groups, tables or content entries, or a non-empty content subtree.
- **Help, README and `examples/README.md` still describe the pre-Phase-2 routes.** Bringing them current is Phase 14; do not treat them as the routing contract in the meantime.
- **Files REST API (Phase 4):** `files_api.go` implements section 64.4 under `/api/v1/{project}/files`. `GET /files` returns `{"data":[...],"page":{...}}` (paged, `prefix`/`content_type`/`kind` filters); `GET /files?path=` returns a content-entry representation or a directory representation (`path`,`kind`,`url`,`children`,`page`). Directory listing is filesystem-authoritative and self-heals: a raw file found without an entry is adopted into `content_entries` with a new ID. `POST /files/directories` and the `/files/{directories,pages}` legacy facades are unchanged. `PUT` is atomic, caps at 16 MiB (`413`), forces page media types from the extension and honors the request `Content-Type` for files; replacing preserves the ID and `created_at` and returns `200`, a new path `201`. `DELETE` by path removes a subtree; deleting the root is `409`. `move` preserves IDs (including descendants), `copy` assigns new IDs (`201`); both reject a conflicting destination `409` and a missing source `404`. `rescan` returns `{"added","removed","refreshed"}`; a refresh updates `size`/`kind`/`content_type` and clears `sha256` when the size changed. `stable_url` is `{base}/{project}/files/id/{id}` (section 66.9) rather than the superseded `/f/{id}`; serving that URL and `/files/{id}/content` is implemented in Phase 5.
- **Reserved content segment `id`:** `canonicalContentPath` rejects a content path whose first segment is `id`, so it cannot shadow the human stable URL `/{project}/files/id/{id}` (section 66.5). The API shape `/api/v1/{project}/files/{id}` addresses an opaque entry ID. Attachment-reference delete integrity (`409`) waits for the attachment field type in Phase 9.
- **Serving and stable URLs (Phase 5):** content is streamed through `serveDownload`, which sets `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` before calling `http.ServeContent`, so `HEAD`, byte ranges and conditional requests work. The entry's stored content type is sent (extension detection is the fallback). `/{project}/files/id/{id}` and `/api/v1/{project}/files/{id}/content` serve an entry's raw bytes for any entry, including a page; pages render only at their path URL. Non-page files at a path URL now download instead of being served with `writeText`. `contentUI` intercepts a first segment of `id` before content-path canonicalisation; a missing ID is `404`.

## Open questions (decide before the relevant phase)

- **PDF extractor:** which small pure-Go library (e.g. `rsc.io/pdf` vs `pdfcpu`); confirm it builds under cgo/SQLite constraints. Needed in Phase 6.
- **Project home page** (`/{project}/`): overview page linking `data` and `files`, or redirect to `/{project}/files`. Needed in Phase 11.
- **Human search surface:** search box in the explorer, a `/{project}/search` page, or both. Phase 11.
- **`clio backup` / `restore` command:** ship in Phase 13 or document stop-copy only.
- **Data listing convenience:** whether `/{project}/data` also exposes a flat table list. Phase 11.

## Progress

- [x] **Phase 0** — scaffolding and baseline
- [x] **Phase 1** — projects registry and instance routes
- [x] **Phase 2** — project scope, routing, `data` partition
- [x] **Phase 3** — content entries and identity
- [x] **Phase 4** — files REST API
- [x] **Phase 5** — serving and stable URLs
- [ ] **Phase 6** — native extraction and FTS5
- [ ] **Phase 7** — search API
- [ ] **Phase 8** — enrichment API
- [ ] **Phase 9** — `attachment` field type
- [ ] **Phase 10** — pages/directories folded into files
- [ ] **Phase 11** — human UI: data browser and file explorer
- [ ] **Phase 12** — WebDAV
- [ ] **Phase 13** — backup and restore
- [ ] **Phase 14** — docs, conformance, acceptance

(Detailed tasks per phase are in `IMPLEMENTATION-PLAN.md`.)

## Environment and verification

- Go 1.25; cgo build needs a C toolchain for the SQLite driver.
- After Phase 6, builds and tests need `-tags sqlite_fts5`.
- Verification:
  ```sh
  go test ./...
  go vet ./...
  go build -buildvcs=false -o /tmp/gocl-clio-check .
  ```
- Tests use temporary SQLite databases and content directories; no external services.

## Test inventory (baseline, v1.4 code)

- 15 test files, 79 `Test*` functions; statement coverage 67.1%.
- Heaviest areas: records CRUD/validation (`records_test.go`, 15), queries and
  aggregates (`query_contract_test.go`, 10; `value_contract_test.go`, 10),
  content/directories/pages/ZIP (`content_contract_test.go`, 9;
  `content_test.go`, 7), API contracts/regression (`api_regression_test.go`, 8;
  `api_contract_test.go`, 5), indexing (3), auth (4), plus lifecycle, ClioJS,
  collection browser, examples and decimal-format tests.
- All tests exercise behaviour through HTTP with temp databases/content dirs.
- Expect **Phase 2** to touch every test file that hardcodes a URL; introduce a
  shared test URL helper rather than editing each path ad hoc.

## Session log

- **2026-10-08** — Spec v1.5 (content filesystem), v1.6 (namespaces/projects) and v1.7 (project-scoped URL scheme and partitions) drafted, merged to `master` via PRs #1–#3. No application code changed. Created `IMPLEMENTATION-PLAN.md` and this context file. Next: Phase 0.
- **2026-10-08** — Phase 0: baseline verified green (`go test -count=1 ./...` 1.7s, `go vet ./...`, `go build -buildvcs=false`). 79 tests, 67.1% coverage. Plan and context files committed. Next: Phase 1.
- **2026-10-08** — Phase 1: added the `projects` table and seeded `default`; rebuilt `groups_meta`, `tables_meta`, `fields_meta` and `records` with project-scoped primary/foreign keys and migrated legacy databases on open (all rows assigned to `default`); added the projects API (`GET`/`POST /api/v1/projects`, `GET`/`DELETE /api/v1/projects/{project}`), reserved project names, and a `projects` health count. New tests: `projects_test.go` (5). All checks green. Next: Phase 2.
- **2026-10-08** — Phase 2: made routing project-first (`ServeHTTP`/`api` resolve `{project}`; instance routes stay unscoped); bare `/` and `/api/v1` redirect to `default`; `/api/v1/{project}/data/...` and `/{project}/data/...` for metadata/groups/tables/records; the legacy directory/page operations re-homed under `/api/v1/{project}/files/{directories,pages}` and the content UI under `/{project}/files/...`; collection browser at `/{project}/collections/...`. Threaded project through every data query and managed index (added `managed_indexes.project`), and made ClioJS project-aware. Introduced per-request app scoping via `withProject` (pointer content mutex). Updated every test URL. New tests: `TestBareRoutesRedirectToDefaultProject`, `TestUnknownProjectReturnsNotFound`, `TestProjectDataIsolation`, `TestProjectScopedHumanAndAPIRoutes`. All checks green. Next: Phase 3.
- **2026-10-08** — Phase 3: replaced `content_page_times` with `content_entries` (stable opaque IDs, `kind`/`content_type`/`size`/`sha256`/timestamps, unique `(project, path)`); migrated legacy rows to `default` with new IDs; partitioned the content root into `content/{project}` with a one-time move of legacy root content into `content/default`; added reconciliation (startup and a reusable method for rescan) that keeps existing IDs, adds new ones, removes vanished paths and skips symlinks; updated the page/directory/zip write paths to record entries; a project with content can no longer be deleted. New tests: `TestContentEntryIdentityStableAcrossReplace`, `TestContentReconciliationAddsAndRemoves`, `TestContentIsolationBetweenProjects`, `TestContentEntriesMigrationFromPageTimes`, `TestContentLayoutMigrationMovesLegacyRoot`. All checks green. Next: Phase 4.
- **2026-10-08** — Phase 4: added the files REST API in `files_api.go` under `/api/v1/{project}/files` — paged flat catalog with `prefix`/`content_type`/`kind` filters and `?path=` node/directory listing, `GET /files/{id}`, atomic `PUT /files?path=` (16 MiB, ID-preserving replace, declared content type for files), `POST /files/directories`, `DELETE /files?path=` and `/files/{id}`, `POST /files/move` (descendant IDs preserved) and `/files/copy` (new IDs) and `/files/rescan` with an added/removed/refreshed summary. Reserved the `id` content-path segment; adopted raw on-disk files into entries during listing; kept the legacy `/files/{directories,pages}` facades and all existing content tests green. New tests: `TestFilesCRUDByPathAndID`, `TestFilesDirectoryListingAndPageKind`, `TestFilesListFiltersAndPaging`, `TestFilesMoveAndCopy`, `TestFilesConflictsAndReservedSegment`, `TestFilesDeleteByPathRemovesSubtree`, `TestFilesUploadLimitAndTraversal`, `TestFilesRescanSummary`, `TestFilesProjectIsolation`. All checks green. Next: Phase 5.
- **2026-10-08** — Phase 5: served content with stable URLs and safe downloads. Added `serveFileContent` and `serveDownload` (files_api.go): `GET`/`HEAD /api/v1/{project}/files/{id}/content` and the human `/{project}/files/id/{id}` stream an entry's raw bytes with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` via `http.ServeContent` (byte ranges, conditional requests, missing ID `404`). `contentUI` intercepts the reserved `id` segment before content-path canonicalisation and permits `HEAD`; non-page files at a path URL now download instead of `writeText`, while Markdown still renders (sanitised) and HTML stays trusted executable content at path URLs. New tests: `TestFileContentDownloadHeadersAndRanges`, `TestFileContentMissingIDReturnsNotFound`, `TestFileContentIsProjectScoped`, `TestStableHumanURLDownloadsRawPageBytes`, `TestPathURLNonPageDownloads`, `TestStableHumanURLMissingAndMethodRestrictions`. All checks green. Next: Phase 6.
