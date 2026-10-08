# Implementation context (living file)

> **Update this file at the start and end of every work session.** Keep it
> short and factual. It is the handoff that makes context compaction harmless:
> if chat history is lost, read this file and `IMPLEMENTATION-PLAN.md` and
> continue. Never rely on memory of previous sessions.

## Snapshot

- **Last updated:** 2026-10-08
- **Spec:** `SPEC.md` v1.7 (sections 64, 65, 66 are new; earlier URL sections carry supersession notes)
- **Plan:** `IMPLEMENTATION-PLAN.md`
- **Code baseline:** Phase 1 implemented; data tables are project-scoped; routing is still unscoped (Phase 2)
- **Branch:** `master`
- **Last merged commit:** `719f063` (Phase 1, PR #5)
- **Current phase:** Phase 1 complete (this commit)
- **Next action:** Phase 2 — rework `ServeHTTP`/`api` for instance routes and `/api/v1/{project}/data/...`, resolve and validate `{project}` on every scoped request, thread `project` through every SQL query, move the table UI under `/{project}/data/...`, add `/` and `/api/v1` redirects, and mechanically update every test URL through a shared helper.
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

## Open questions (decide before the relevant phase)

- **PDF extractor:** which small pure-Go library (e.g. `rsc.io/pdf` vs `pdfcpu`); confirm it builds under cgo/SQLite constraints. Needed in Phase 6.
- **Project home page** (`/{project}/`): overview page linking `data` and `files`, or redirect to `/{project}/files`. Needed in Phase 11.
- **Human search surface:** search box in the explorer, a `/{project}/search` page, or both. Phase 11.
- **`clio backup` / `restore` command:** ship in Phase 13 or document stop-copy only.
- **Data listing convenience:** whether `/{project}/data` also exposes a flat table list. Phase 11.
- **Project-scoped managed indexes:** Phase 1 introduced project-scoped primary keys but left `managed_indexes` (and the index name/expression helpers in `indexing.go`) unscoped. Phase 2 must add `project` to the managed index names, expressions and `managed_indexes` rows, or a `unique` field could enforce uniqueness across projects.

## Progress

- [x] **Phase 0** — scaffolding and baseline
- [x] **Phase 1** — projects registry and instance routes
- [ ] **Phase 2** — project scope, routing, `data` partition
- [ ] **Phase 3** — content entries and identity
- [ ] **Phase 4** — files REST API
- [ ] **Phase 5** — serving and stable URLs
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
