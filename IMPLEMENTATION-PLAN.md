# GoClio v1.7 implementation plan

This is the runnable, phase-by-phase plan to bring the Go implementation from
the **v1.4** contract it currently satisfies up to **`SPEC.md` v1.7**
(sections 64, 65 and 66). It is designed for interrupted work: each phase is
small, independently verifiable, and leaves a resumable state.

The companion living file is [`IMPLEMENTATION-CONTEXT.md`](IMPLEMENTATION-CONTEXT.md).
Keep it current; it is what makes context compaction harmless.

## How to use this plan

1. Read `IMPLEMENTATION-CONTEXT.md` first — it holds current state and the next action.
2. Pick the first unchecked phase/task in that file (or in the checklist below).
3. Implement the task, add tests, run verification.
4. Update `IMPLEMENTATION-CONTEXT.md` (progress, decisions, exact next action).
5. Update `SPEC-CONFORMANCE.md` (move any matching row out of *Pending*) and `CHANGELOG.md`.
6. Commit on a short branch, open a PR, merge, pull. One phase per PR where practical.

If the context window is compacted at any point, the two files plus `master`
carry everything needed to continue. Never rely on chat history.

## Ground rules

- **The spec is the contract.** `SPEC.md` v1.7. Section 66 governs every URL,
  partition and reserved name; sections 64–65 govern the filesystem and projects;
  sections 1–63 govern behaviour.
- **No migration, no compatibility layer.** Project scoping is mandatory and the
  routes change (section 66). Update the tests; do not add unscoped aliases.
- **Verification after every change:**
  ```sh
  go test ./...
  go vet ./...
  go build -buildvcs=false -o /tmp/gocl-clio-check .
  ```
  (`make check` runs the same sequence with the git-derived version.)
- **Tests exercise behaviour through HTTP** with temporary SQLite databases and
  content directories; no external services.
- **Never log** request bodies, credentials, query strings, or published content.
- **Keep it small.** No web framework, ORM, DI framework, application cache, or
  extra database. Prefer the standard library.
- **Update as you go:** `SPEC-CONFORMANCE.md` (tests ↔ spec areas) and
  `CHANGELOG.md` (user-visible changes). `IMPLEMENTATION-CONTEXT.md` every session.

## Baseline

- **Spec:** v1.7. Sections 64 (content filesystem, attachments, search, WebDAV),
  65 (namespaces/projects) and 66 (project-scoped URL scheme and partitions) are
  new; several earlier sections carry *Superseded by section 66* notes.
- **Code:** implements the v1.4 contract only. None of v1.5–v1.7 is implemented.
- **Stack:** Go 1.25, cgo SQLite (`mattn/go-sqlite3`), one SQLite database in WAL
  mode, one content directory, single process.

## Reuse map (what already exists)

| Area | Files | Action |
| --- | --- | --- |
| Tables, fields, records, refs | `app.go`, `records.go`, `indexing.go` | keep; add project scope |
| Query engine (filter/sort/page/group/agg) | `query_sql.go` | keep; add project scope |
| Content paths, atomic writes, ZIP upload, traversal guards | `content.go` | keep; extend |
| Content UI (directory browsing) | `content.go`, `ui.go` | becomes the file explorer |
| Table UI and forms | `ui.go` | keep; re-scope to `/{project}/data/...` |
| Markdown subset + client asset | `content.go`, `health.go` | keep; render `.md`/`.html` in files |
| Help / health / auth / ClioJS | `health.go`, `auth.go`, `assets/clio.js` | keep; re-scope |
| DB schema + open/migrate | `main.go` | extend for projects and content entries |

## Phase summary

| # | Phase | Spec | Delivers |
| --- | --- | --- | --- |
| 0 | Scaffolding and baseline | — | plan + context files; green baseline |
| 1 | Projects registry and instance routes | §65.2, §65.5, §66.3–66.4 | `projects` table, projects API, `default` |
| 2 | Project scope + routing + `data` partition | §66.1–66.6 | `/{project}/data/...` for all existing data APIs and UI |
| 3 | Content entries and identity | §64.2, §66.9 | `content_entries` with IDs, reconciliation, migration |
| 4 | Files REST API | §64.4, §66.7 | list/dir/put/delete/move/copy/rescan |
| 5 | Serving and stable URLs | §64.3, §64.5, §66.5 | download/nosniff, `/{project}/files/id/{id}`, ranges |
| 6 | Native extraction and FTS5 | §64.6 | text-like + PDF text, `content_search`, build tag |
| 7 | Search API | §64.7 | `GET /api/v1/{project}/search` |
| 8 | Enrichment API | §64.8 | fingerprint-checked agent write-back |
| 9 | `attachment` field type | §64.9 | field type, validation, delete integrity, form control |
| 10 | Pages/directories folded into files | §66.2, §64.14 | rendered pages, client renderer, Page/Directory APIs removed |
| 11 | Human UI: data browser + explorer | §35, §64.14, §66.5 | re-scoped `/collections`, `/files` with gutter |
| 12 | WebDAV | §64.10, §66.8 | `/api/v1/{project}/files/dav` + `/{project}/files/dav` |
| 13 | Backup and restore | §57, §64.11 | docs; optional `clio backup`/`restore` |
| 14 | Docs, conformance, acceptance | §46, §58–60, §62 | README/help/examples; conformance moved out of Pending |

---

## Phase 0 — Scaffolding and baseline

**Goal:** a green baseline and the continuity files.

- [ ] Confirm `go test ./...`, `go vet ./...`, `go build -buildvcs=false -o /tmp/gocl-clio-check .` pass on `master`.
- [ ] Commit `IMPLEMENTATION-PLAN.md` and `IMPLEMENTATION-CONTEXT.md`.
- [ ] Record the existing test inventory in the context file.

**Done when:** baseline is green and both files are on `master`.

## Phase 1 — Projects registry and instance routes

**Spec:** §65.2, §65.5, §66.3–66.4.

- [ ] Add a `projects` table (`name`, `label`, `description`, `sort_order`, `created_at`) and seed `default`.
- [ ] Add a `project` column (default `default`) to `groups_meta`, `tables_meta`, `fields_meta`, `records`; migrate existing rows on open.
- [ ] Implement `GET/POST /api/v1/projects`, `GET/DELETE /api/v1/projects/{project}` (delete only when empty; `default` cannot be created/renamed/deleted).
- [ ] Reserve project names `api`, `health`, `help`, `projects`, `assets`, `favicon.svg`.
- [ ] Add a `projects` count to `/health` and `/api/v1/health`.
- [ ] Tests: create/list/read/delete-when-empty, duplicate, reserved, non-empty `409`.

**Done when:** projects exist and are addressable; health reports the count.

## Phase 2 — Project scope, routing and the `data` partition

**Spec:** §66.1–66.6.

- [ ] Rework `ServeHTTP`/`api`: instance routes (`/health`, `/help`, `/api/v1/health`, `/api/v1/help`, `/api/v1/projects`, `/assets`, `/favicon.svg`); `/api/v1/{project}/data/...`; `/api/v1/{project}/files/...`.
- [ ] Resolve and validate `{project}` on every scoped request (`404` for unknown).
- [ ] Move metadata, groups and records handling under `.../data/...`; thread project scope through every SQL query (add `project` to the WHERE of records/groups/fields/tables).
- [ ] Human table UI under `/{project}/data/{group}/{table}[...]`; keep forms.
- [ ] `/` → `/{default}/` and `/api/v1` → `/api/v1/{default}` redirects.
- [ ] Mechanical test update: introduce a test URL helper; update every existing test to the new paths.
- [ ] Tests: project isolation (same group name in two projects), unknown project `404`, redirects.

**Done when:** all existing data API and UI tests pass against the project-scoped routes, and two projects cannot see each other's data.

## Phase 3 — Content entries and identity

**Spec:** §64.2, §66.9.

- [ ] Replace `content_page_times` with `content_entries` (`id`, `project`, `path`, `kind`, `content_type`, `size`, `sha256`, `created_at`, `updated_at`; unique `(project, path)`).
- [ ] Migrate existing `content_page_times` rows to `default` entries with new IDs.
- [ ] Generate opaque IDs (reuse the record ID generator); keep filesystem authoritative for bytes.
- [ ] Reconciliation walk at startup and on rescan: keep ID for an existing path, assign an ID to a new path, drop vanished rows.
- [ ] Tests: ID stability across replace, reconciliation add/remove, migration.

**Done when:** every page/file in the content root has a stable entry with a stable ID.

## Phase 4 — Files REST API

**Spec:** §64.4, §66.7.

- [ ] `GET /api/v1/{project}/files` (paged, filters) and `?path=` (entry or directory listing).
- [ ] `GET /api/v1/{project}/files/{id}`.
- [ ] `PUT /api/v1/{project}/files?path=...` (create/replace, atomic, 16 MiB limit, ID preserved).
- [ ] `POST .../files/directories`, `DELETE .../files?path=` and `.../files/{id}` (409 when referenced).
- [ ] `POST .../files/move`, `.../files/copy` (IDs preserved on move; new IDs on copy), `.../files/rescan`.
- [ ] Enforce `409` for file/dir vs page/dir type conflicts; reserve segment `id`.
- [ ] Tests: CRUD by path and ID, move/copy, limits, traversal, conflict rules, rescan.

**Done when:** the filesystem is fully manipulable through the API.

## Phase 5 — Serving and stable URLs

**Spec:** §64.3, §64.5, §66.5.

- [ ] `GET /api/v1/{project}/files/{id}/content` and `/{project}/files/id/{id}` stream bytes with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`, via `http.ServeContent` (ranges, `HEAD`).
- [ ] Path URLs: directory listing, rendered page (`.md`/`.html`), or download.
- [ ] Tests: disposition/nosniff, ranges, missing ID `404`, page vs file.

**Done when:** non-page content always downloads and never executes in-origin.

## Phase 6 — Native extraction and FTS5

**Spec:** §64.6.

- [ ] Add `-tags sqlite_fts5` to `Makefile`, `Dockerfile` and CI; confirm FTS5 is available.
- [ ] Add `content_search` (FTS5: `id`, `project`, `path`, `kind`, `source`, `title`, `body`).
- [ ] Native extraction for text-like formats and PDF text (choose one small pure-Go extractor; see context decisions). Cap indexed text (1 MiB).
- [ ] Populate on write and during rescan; drop on delete.
- [ ] Tests: extraction per type, cap, rebuild by rescan, FTS5 build.

**Done when:** native text is searchable and rebuildable.

## Phase 7 — Search API

**Spec:** §64.7.

- [ ] `GET /api/v1/{project}/search?q=...` with paging; scoped to the project.
- [ ] Build the query by quoting terms (no raw `MATCH`), AND-combine, optional prefix.
- [ ] Results: `id`, `path`, `kind`, `content_type`, `source`, escaped `snippet` (private sentinels), `score`.
- [ ] Tests: ranking, snippets, paging, empty/oversized `422`, literal-safety, project isolation.

**Done when:** unified project search works with safe snippets.

## Phase 8 — Enrichment API

**Spec:** §64.8.

- [ ] `GET/PUT/DELETE /api/v1/{project}/files/extraction?path=...` (or by ID).
- [ ] Require a matching fingerprint (`size:mtime` or `sha256`); `409` on mismatch.
- [ ] Store with `source = agent:<provider>`; delete falls back to native.
- [ ] Tests: match/mismatch, survives move, delete fallback.

**Done when:** sidecar agents can make extractions searchable.

## Phase 9 — `attachment` field type

**Spec:** §64.9.

- [ ] Accept `type: "attachment"` in field normalisation/metadata, with optional `accept`.
- [ ] Validate the file ID exists **in the same project** on create/update/default; `422` otherwise.
- [ ] Delete integrity: deleting a referenced entry (or a directory containing one) returns `409`.
- [ ] Generic form control (upload/choose) and record display via `/{project}/files/id/{id}`.
- [ ] Tests: validation, representation, delete integrity, cross-project rejection.

**Done when:** records can link files and the link survives rename.

## Phase 10 — Pages/directories folded into files

**Spec:** §66.2, §64.14.

- [ ] Render `.md` (Markdown subset, sanitised) and `.html` at path URLs under `/{project}/files/...`.
- [ ] Keep the client Markdown asset and `Clio.Markdown`; point the explorer at files.
- [ ] Remove the Page API and Directory API code paths; ensure their behaviour is covered by the files partition.
- [ ] Tests: markdown subset, HTML trust boundary, create/replace/delete via files.

**Done when:** there is no separate pages/directories API.

## Phase 11 — Human UI: data browser and file explorer

**Spec:** §35, §64.14, §66.5.

- [ ] Re-scope the collection data browser to `/{project}/data/...`.
- [ ] Build the file explorer at `/{project}/files` with a **directory-browsing gutter** and breadcrumbs; URL reflects the current directory.
- [ ] Add `Clio.FileBrowser.mount(element)` to the ClioJS asset; full light actions (new folder, upload, rename/move, delete) and search.
- [ ] Escape names/snippets; never render record values as HTML.
- [ ] Tests: routes, gutter/navigation, actions, `409` surfacing, search workflow.

**Done when:** both browsers work at the new URLs.

## Phase 12 — WebDAV

**Spec:** §64.10, §66.8.

- [ ] Add `golang.org/x/net/webdav`; mount at `/api/v1/{project}/files/dav/...` and `/{project}/files/dav/...`.
- [ ] Wrapper `webdav.FileSystem` sharing validation, limits, index/timestamp updates, and reference integrity with the API.
- [ ] `PUT`/`GET`/`HEAD`/`DELETE`/`MKCOL`/`MOVE`/`COPY`/`PROPFIND`/`LOCK`/`UNLOCK`; in-memory lock system.
- [ ] `CLIO_WEBDAV_ENABLED` (default off), behind auth/HTTPS.
- [ ] Tests: method set, ID preservation on MOVE, `409` on referenced delete, disabled by default, auth.

**Done when:** a client can mount a project and rename/move files.

## Phase 13 — Backup and restore

**Spec:** §57, §64.11.

- [ ] Document the stop-copy procedure and that the database is mandatory (IDs, timestamps, agent text).
- [ ] Rescan-on-restore support.
- [ ] Optional: `clio backup <dest>` / `clio restore <src>` (VACUUM INTO + content copy + manifest).
- [ ] Tests: restore reproduces IDs, timestamps and enrichment.

**Done when:** backup/restore is documented and (optionally) a one-command path exists.

## Phase 14 — Docs, conformance, acceptance

**Spec:** §46, §58–60, §62.

- [ ] Update `README.md`, `/help` and `/api/v1/help` for projects, partitions, files, search, WebDAV, backup.
- [ ] Move `SPEC-CONFORMANCE.md` rows out of *Pending* as phases land; keep the mapping current throughout.
- [ ] Update examples under `examples/`.
- [ ] Run the section 59 acceptance walkthrough end to end.
- [ ] Final `go test ./...`, `go vet ./...`, `go build -buildvcs=false -o /tmp/gocl-clio-check .`.

**Done when:** spec v1.7 conformance is claimed only where tests exist.

---

## Session protocol (for resuming after compaction)

1. Open `IMPLEMENTATION-CONTEXT.md`; read **Current state**, **Next action**, and the **Decision log**.
2. Run the verification commands to confirm the baseline still passes.
3. Do the next action only; keep the diff small.
4. Update the context file, conformance, and changelog.
5. Commit/PR/merge/pull; record the commit in the context file.

## PR checklist

- [ ] Behaviour matches the cited spec sections.
- [ ] New/changed behaviour covered by HTTP tests with temp DB/content.
- [ ] `go test ./...`, `go vet ./...`, `go build -buildvcs=false` pass.
- [ ] `SPEC-CONFORMANCE.md` updated.
- [ ] `CHANGELOG.md` updated for user-visible change.
- [ ] `IMPLEMENTATION-CONTEXT.md` updated.
