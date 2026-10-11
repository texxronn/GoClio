# Lifecycle Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Status: PLAN — not started.** Written 2026-10-11 against product version
`2.4.0` (commit `6310baf`). No application code has changed.

**Issue:** [#47 — lifecycle hooks (function objects / callbacks) for records,
files and the data API](https://github.com/texxronn/GoClio/issues/47).

**Goal:** Give a project a first-class way to attach functions/callbacks to
record and file lifecycle events without forking the server. Hooks are
project-scoped, stored in SQLite, managed over the API (and optionally seeded
from a config file), and executed behind one language-agnostic JSON contract so
a hook can be upgraded from the built-in `declarative` executor to `webhook`,
`expression` or `wasm` without changing its registration.

**Primary use case:** a gift can never be stored without an occasion — on
`record.before_create` a declarative hook finds or creates an occasion deduped
by `(host, date)`, links it, and marks it generated, in the same transaction.

**Architecture:** An in-process engine (`hooks.go`) resolves the hooks matching
an event and table, runs in-process executors *inside the existing write
transaction*, and turns their results into the canonical action set
(`set`/`default`/`create`/`link`/`reject`). `webhook` hooks are written to a
durable outbox row inside the same transaction and delivered after commit by a
single background worker with bounded retries and signing. Record writes move
from single `Exec` calls to explicit transactions so before-hooks can veto and
after-hooks observe the same transaction. File hooks reuse the existing
`saveContentWrite` / `deleteContentEntry` choke points.

**Tech stack:** Go 1.25, `net/http`, `database/sql` + `mattn/go-sqlite3` (cgo,
`-tags sqlite_fts5`), `golang.org/x/net/webdav`. P3 adds
[`github.com/tetratelabs/wazero`](https://wazero.io/) (pure Go, **no cgo**).

**Spec:** [`SPEC.md`](SPEC.md) v1.10 (normative contract). Hooks become a new
`§67` and force explicit amendments to `§3.1`, `§4.2`, `§49`, `§61`, `§66.6`.

---

## Contract changes required (read first)

This feature is deliberately large. Before any code, the frozen contract has to
be amended **on purpose**; the plan treats each change as a first-class task,
not a footnote.

1. **Spec revision bump** `v1.10 → v1.11`, header, versioning table and the
   revision-history bullet.
2. **New normative section `§67 Lifecycle hooks`:** hook object, event
   vocabulary, `when` matching, canonical ABI, action vocabulary, executor
   kinds, ordering, transaction semantics, limits, scoping, delivery guarantee.
3. **`§3.1 Explicitly not in v1`** currently does not forbid hooks, so no
   removal — but it must state that hooks are the supported extension point and
   that native Go plugins / a server-side JS runtime remain excluded.
4. **`§4.2 Frameworks`** says "keep the dependency count low". Adding `wazero`
   (P3) is a conscious exception; the section states it is a pure-Go, no-cgo,
   sandboxed runtime and is the only permitted non-standard executor dependency.
5. **`§61 Repository constraints`** forbids "a queue because background work
   might someday exist". Webhook delivery is real, durability-backed
   at-least-once work, not speculation: the section gains an explicit exception
   for the bounded hook-delivery outbox (one table, one worker).
6. **`§49 Complete API v1 summary` and `§66.6`** gain the `/hooks` routes and
   the optional human page.
7. **`§51 Persistence` / backup** note that hooks, wasm modules and delivery
   records live in the same SQLite database and are covered by `VACUUM INTO`.
8. **`§47`/help text, `README.md`, `CHANGELOG.md`, `SPEC-CONFORMANCE.md`** as
   usual.

The plan keeps the built-in *generic* hook system first (not a narrow
`auto_reference` metadata shim), per the issue's primary direction.

---

## Global constraints

- Go `1.25`; build and test with `-tags sqlite_fts5` (SPEC §64.6).
- SQLite remains the only persistent store; content stays in the configured
  content directory (AGENTS.md).
- Keep the JSON error shape `{"error":"<code>","message":"..."}` (SPEC §50) and
  the existing status codes.
- Never log request bodies, credentials, query strings or published content
  (AGENTS.md). Hook payloads contain record data, so the delivery log stores
  **metadata only** — never the event body or the response body.
- Hooks are **trusted configuration**, like published HTML (SPEC §55.1). This is
  not a sandbox for untrusted publishers; it is a sandbox for *trusted but
  fallible* extension code (`wasm`) and a boundary for out-of-process code
  (`webhook`).
- In-process executors (`declarative`, `expression`, `wasm`) must not touch the
  network or the filesystem. All effects go through the action vocabulary.
- Hooks run identically for the JSON API, the server-rendered UI and ClioJS,
  because both UI paths call the same `createRecord` / `patchRecord` /
  `deleteRecord` / content choke points.
- Tests exercise behaviour through HTTP (`newTestApp`, `testRequest`,
  `testJSON`, `assertAPIError`) and use temporary SQLite DBs + content dirs.
- Every task ends with:
  ```sh
  go test -tags sqlite_fts5 ./...
  go vet -tags sqlite_fts5 ./...
  go build -tags sqlite_fts5 -buildvcs=false -o /tmp/gocl-clio-check .
  ```
- Every task that adds tests updates the matching row of
  `SPEC-CONFORMANCE.md`.
- No new dependency in P1/P2. P3's `wazero` is isolated to one file and gated by
  its own task.

---

## Design

### Event vocabulary

Additive, closed set; unknown events are rejected `422` at registration.

```text
record.before_create   record.after_create
record.before_update   record.after_update
record.before_delete   record.after_delete
file.before_put        file.after_put
file.before_delete     file.after_delete
```

- `record.*` fires for `record` and `timeseries` tables.
- `file.*` fires for content-entry writes and deletes (put/replace and delete of
  a single file or a subtree). Directory creation is not a `put`.
- `before_*` events are **vetoable** and may mutate the in-flight value.
- `after_*` events observe the value and may add/derive rows.
- `webhook` kind is valid **only** on `after_*` events (a network call cannot be
  part of a write transaction); registration of a `before_*` webhook is `422`.

### Hook object

```json
{
  "id": "3f9c…",
  "name": "ensure-occasion",
  "event": "record.before_create",
  "when": { "group": "gifts", "table": "gifts" },
  "kind": "declarative",
  "order": 10,
  "enabled": true,
  "on_error": "reject",
  "config": { },
  "created_at": "2026-10-11T00:00:00Z",
  "updated_at": "2026-10-11T00:00:00Z",
  "api_url": "…/api/v1/lenadena/hooks/3f9c…"
}
```

- `id` uses the existing opaque `newID()` (32 hex chars), like records and
  content entries. Clients must treat it as opaque (SPEC §14); the issue's
  `hk_…` shape is illustrative, not adopted, to avoid a second ID convention.
- `name` is a per-project unique identifier (`validIdentifier` rules).
- `when` is a small equality filter:
  - record events: `{"group": "...", "table": "..."}` (either key optional).
  - file events: `{"path_prefix": "/reports", "kind": "page"|"file"}`.
  - unknown keys → `422`.
- `kind ∈ {declarative, webhook, expression, wasm}`. A kind not yet implemented
  by the running phase returns `422 "hook kind is not supported"`.
- `order` is a signed 32-bit integer (default `0`), then `id` ascending.
- `enabled` (default `true`) lets a hook be parked without deleting it.
- `on_error ∈ {reject, log}` (default `reject`). For in-process kinds, `reject`
  aborts the write with `422`; `log` records the failure and continues. For
  webhooks, `on_error` governs whether a permanently failed delivery is surfaced
  in the delivery log only.
- `config` shape is validated per `kind` (below). `config.secret` (webhook) is
  write-only: never returned by a read, reported as `"secret_set": true`.

### Canonical hook ABI (v1)

One JSON contract for every executor, so a hook can change `kind` without
changing its identity. `abi` is `1`; unknown versions are rejected at load time
and the hook is skipped with a logged error.

**Input** (`hookInput`):

```json
{
  "abi": 1,
  "hook": { "id": "3f9c…", "name": "ensure-occasion", "kind": "declarative" },
  "event": "record.before_create",
  "project": "lenadena",
  "group": "gifts",
  "table": "gifts",
  "record": { "id": null, "host": "…", "date": "2026-09-29", "occasion": null },
  "previous": { },
  "fields": [ { "name": "occasion", "type": "reference", "group": "gifts", "table": "occasions" } ],
  "path": null
}
```

- `record` is the **proposed** value for `before_*` (after normal field
  validation/coercion) and the **stored** value for `after_*`.
- `previous` is the stored value for `before_update` / `after_update`, otherwise
  `{}`.
- `fields` is the affected table's field definitions (the same objects returned
  by `GET …/tables/{table}`), so executors know types and reference targets.
- `path` carries the content path for `file.*` events; `record` is `{}` there.

**Output** (`hookResult`):

```json
{
  "set":     { "occasion": "…" },
  "default": { "origin": "generated" },
  "create":  [ { "alias": "occ", "group": "gifts", "table": "occasions",
                 "record": { "name": "Generated", "host": "…", "date": "…" } } ],
  "link":    [ { "field": "occasion", "from": "occ.id" } ],
  "reject":  "message",
  "note":    "free text for the delivery log"
}
```

- Unknown action keys → rejected (fail the hook).
- `set` values are validated exactly like a field value on create/update
  (`coerce`, constraints, reference/attachment existence).
- `default` applies only when the current value is null/absent.
- `create` creates records through the same validated, hooked write path as the
  parent write (recursion depth is bounded; see below).
- `link` sets a field on the in-flight record to a created record's id
  (`<alias>.id`).
- `reject` is only honoured from `before_*` hooks; from `after_*` it fails the
  transaction (in-process) and is logged.

### Executors ("kinds")

- **`declarative` (P1).** No code execution: an ordered `config.ops` list. Ops:
  - `{"op":"set","set":{...}}`
  - `{"op":"default","set":{...}}`
  - `{"op":"validate","require":["field", …]}`
  - `{"op":"derive","field":"label","template":"{name} ({date})"}`
  - `{"op":"ensure_reference","field":"occasion","group":"gifts","table":"occasions",
     "match":{"host":"$record.host","date":"$record.date"},
     "create":{"name":"Generated","host":"$record.host","date":"$record.date","origin":"generated"}}`

  Template references are `$record.<field>` (and literals). `ensure_reference`
  is find-or-create: it evaluates `match`, queries the target table through the
  write transaction, creates with the normal record rules when absent, and emits
  `link`. This is the occasion use case.
- **`webhook` (P2).** POSTs the canonical input as JSON to `config.url` and
  accepts the canonical output as the response. After-commit only; at-least-once;
  HMAC-signed; bounded retries with backoff; idempotency key header.
- **`wasm` (P3).** A sandboxed module exporting `handle(ptr,len) -> (ptr,len)`
  over linear memory (or a simple JSON-ABI helper), run by `wazero` with fuel
  and memory caps and **no** host imports. The true in-process function object.
- **`expression` (P3).** A tiny, auditable evaluator over `$record`/`$previous`
  producing `set`/`default`/`reject`; also usable for `when` conditions.

### Execution model and transaction semantics

- **Matching** happens once per write: load enabled hooks for the project where
  `event` matches, `when` matches the group/table/path, ordered by `order`, `id`.
  Loads use the write transaction's queryer so a hook created mid-request cannot
  perturb the current write.
- **In-process executors** run in registration order inside the write
  transaction. Each hook's actions are applied immediately in order, so a later
  hook sees earlier `set`s. `reject` returns `422` and the whole transaction
  rolls back (no partial rows).
- **`create` action recursion:** a created record runs its own hooks. Depth is
  bounded (`maxHookDepth = 8`); exceeding it is an error, not a hang.
- **`webhook` hooks** are *not* run inline: after the in-process actions of the
  same event are applied, each matching webhook appends an outbox row **inside
  the write transaction**, so the event is queued iff the write commits. The
  worker delivers after commit.
- **`after_*` in-process hooks** run after the primary write but before commit,
  so they can add derived rows atomically and can still veto.
- **`after_*` webhooks** are queued in the same transaction and delivered later.
- **Ordering** is `order` then `id`, across all kinds, per event.
- **Scoping** is strictly `a.project`; a hook never sees another project's data.
- **Failure model:** `on_error: "reject"` (default) aborts; `on_error: "log"`
  records and continues. Webhook delivery failures never fail the committed
  write.

### Limits, timeouts, security

| Concern | Bound | Default |
|---|---|---|
| In-process hook wall clock | `CLIO_HOOK_TIMEOUT_MS` | 250 ms |
| Hook recursion depth | constant | 8 |
| Hook input/output JSON | `bodyLimit` | 2 MiB |
| WASM fuel | `config.fuel` (capped) | 10,000,000 |
| WASM memory | `config.memory_bytes` (capped) | 16 MiB |
| Webhook request timeout | `CLIO_HOOK_WEBHOOK_TIMEOUT_MS` | 5000 ms |
| Webhook attempts | constant | 3 |
| Webhook response body | `bodyLimit` | 2 MiB |
| Delivery worker interval | constant | 1 s |
| Delivery rows retained | days / count | 7 days |

- Webhook URLs must be absolute `http`/`https`. Redirects are not followed
  (SSRF hygiene; documented as trusted-config behaviour).
- `config.secret` is HMAC-SHA256 over the exact request body; header
  `X-Clio-Signature: sha256=<hex>`; `X-Clio-Idempotency: <key>`; `X-Clio-Event`.
- Secrets are redacted from every API read and never logged.
- WASM modules have no WASI host functions, so they cannot open sockets, read
  files or spawn processes; fuel/memory caps bound CPU and RAM.

### Registration API

```text
GET    /api/v1/{project}/hooks                 list (pageable, sorted by order,id)
POST   /api/v1/{project}/hooks                 create
GET    /api/v1/{project}/hooks/{id}            read
PATCH  /api/v1/{project}/hooks/{id}            update (merge)
DELETE /api/v1/{project}/hooks/{id}            delete (204)
POST   /api/v1/{project}/hooks/{id}/test       dry-run against a sample input (P2+)
PUT    /api/v1/{project}/hooks/{id}/module     upload a wasm module (P3)
GET    /api/v1/{project}/hooks/{id}/deliveries delivery log (P2, pageable)
```

- Routing: add `case "hooks":` to the `switch segments[1]` in `api()`
  (`app.go:315-328`) → `scoped.hooksAPI(w, r, segments[2:])`.
- Errors use the standard constructors (`invalid`/`missing`/`conflict`).
- A human page `GET /{project}/hooks` slots into `projectUI`
  (`app.go:193-220`) in P2 and uses the same JSON API.

### Config seeding

- New optional config `CLIO_HOOKS_FILE` (default empty): a JSON array of hook
  objects (each includes `project`).
- At startup, after `migrateSchema`, reconcile **create-if-missing** by
  `(project, name)`; existing hooks are never overwritten, so API edits win and
  restarts are safe.
- Malformed file → fail startup with a clear message (like other config
  errors). Seeded hooks get `config.source = "file"` for observability.
- The DB remains the source of truth; the file is a bootstrap convenience.

### Persistence schema

Append to `migrateSchema`'s `statements` (`main.go:298-307`), project-scoped with
cascade like the other tables:

```sql
CREATE TABLE IF NOT EXISTS hooks (
  id TEXT PRIMARY KEY,
  project TEXT NOT NULL DEFAULT 'default',
  name TEXT NOT NULL,
  event TEXT NOT NULL,
  when_json TEXT NOT NULL DEFAULT '{}',
  kind TEXT NOT NULL,
  ordinal INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  on_error TEXT NOT NULL DEFAULT 'reject',
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project,name),
  FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS hook_deliveries (
  id TEXT PRIMARY KEY,
  project TEXT NOT NULL,
  hook_id TEXT NOT NULL,
  event TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  status TEXT NOT NULL,          -- pending | delivered | failed
  attempts INTEGER NOT NULL DEFAULT 0,
  last_status INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS hook_deliveries_pending
  ON hook_deliveries(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS hook_modules (
  hook_id TEXT PRIMARY KEY,
  project TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size INTEGER NOT NULL,
  bytes BLOB NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(project) REFERENCES projects(name) ON DELETE CASCADE
);
```

- No schema-version table exists; `CREATE TABLE IF NOT EXISTS` + `ensureColumn`
  is the established pattern, so future columns (e.g. `abi_version`) use
  `ensureColumn`.
- `deleteProject` (`projects.go:200-228`) counts `hooks` (and cascades
  deliveries/modules) so a project with hooks is "not empty".

### Delivery model (webhooks)

- The write transaction inserts `hook_deliveries(status='pending',
  next_attempt_at=now, idempotency_key=<deterministic>)`.
- A single `hookWorker` goroutine, started next to the HTTP server and stopped
  by the serve context (like the existing graceful-shutdown path), polls due
  rows, POSTs them, and updates `status`/`attempts`/`last_status`/`next_attempt_at`.
- Backoff: `1s, 5s, 30s` (3 attempts); after the last failure `status='failed'`.
- Idempotency key = hex SHA-256 of `hook_id|event|project|table|record_id|updated_at|seq`.
- The worker never logs bodies; `last_error` stores only an error class/short
  message.

### WASM module storage

- `PUT /hooks/{id}/module` stores bytes in `hook_modules` (size-capped, e.g.
  `CLIO_MAX_UPLOAD_BYTES`), validated by compiling with `wazero` before commit.
- Config references the module implicitly by hook id; `config.entrypoint`
  defaults to `handle`.
- Compilation is cached by `sha256` in a bounded in-memory cache (documented as
  a compile cache, not a data cache; SPEC §4.5 allows implementation caches that
  do not cache application data).

### ClioJS client callbacks (P3, non-authoritative)

- `assets/clio.js` gains optional `onRecordRender(record, table, el)` and
  `beforeQuery(query)` hooks and an event emitter; bump `libraryVersion`
  `1.6.0 → 1.7.0` (`assets/clio.js:5`).
- Update `health.go` help text and `testdata/cliojs_test.cjs`.
- Explicitly documented as client-side convenience that does **not** replace or
  weaken server hooks.

### Exact integration points

| Area | File:line | Change |
|---|---|---|
| Route dispatch | `app.go:315-328` | add `case "hooks"` → `hooksAPI` |
| Human route | `app.go:193-220` | add `hooks` case (P2) |
| Schema | `main.go:298-307` | append hook tables |
| Record create | `records.go:55-87` | wrap in tx; before/after hooks |
| Record patch | `records.go:89-126` | wrap in tx; before/after hooks |
| Record delete | `records.go:247-318` | already tx; add hooks |
| Validation/lookup | `records.go:193,205` | thread a queryer (tx-aware) |
| Content write | `content_entries.go:205` (`saveContentWrite`) | file.before_put/after_put |
| Upload commit | `upload_stage.go:105-156` | hook point + overwrite rollback |
| Content delete | `content_entries.go:242,251` | file.before_delete/after_delete |
| Delete choke caller | `files_api.go:561-655`, `webdav.go:317-329` | hook wraps the catalog delete |
| Extend queryer | `content_entries.go:162` | add `Query` to the interface |
| Project delete | `projects.go:208` | count hooks |
| Worker lifecycle | `main.go:56-64,211` | start/stop `hookWorker` |

**Deliberately excluded hook points (documented in §67):**
- content **move/rename** (`content_search.go:applyContentRename`) and
- **rescan/startup reconciliation** (`content_entries.go:rescanContentMode`)
  and **ZIP upload** (`content.go:448`) fire no hooks, because they are bulk
  reconciliation/import paths, not single-resource `put`/`delete` operations.

---

## Phase P1 — registry, transaction plumbing, declarative executor

Ship the generic system and the occasion example, in-transaction.

### Task 1: Hook registry, model and CRUD API

**Deliverable:** `hooks` table, `hook` model + validation, `/api/v1/{project}/hooks`
CRUD, config seeding, project-delete integration.

**Files:** `hooks.go` (create), `hooks_test.go` (create), `main.go`, `app.go`,
`projects.go`, `SPEC-CONFORMANCE.md`.

- [ ] Add the `hooks` table to `migrateSchema` (`main.go:298-307`).
- [ ] Implement the model in `hooks.go`: `type hook struct { ID, Project,
      Name, Event, Kind, OnError string; When map[string]any; Order int;
      Enabled bool; Config map[string]any; CreatedAt, UpdatedAt string }`,
      with `encodeHook`/`decodeHook` (JSON columns `when_json`, `config`).
- [ ] Implement validation: `normalizeHook(raw) (*hook, *apiError)` —
      identifier name; known event; known kind (phase-gated);
      `when` keys per event class; `on_error`; `order` 32-bit; `config` shape
      per kind; `webhook` only on `after_*`; `secret` accepted but redacted on
      read.
- [ ] Implement store methods in `hooks.go` using `sqlExecer`/`sqlQueryer`:
      `createHook`, `hookByID`, `listHooks`, `patchHook`, `deleteHook`,
      `hooksForEvent(project, event, when)`.
- [ ] Add `hooksAPI` (collection + item) and route it in `api()`
      (`app.go:315-328`). List is pageable (`intParam`, `pageInfo`), sorted by
      `ordinal,id`. Emit `api_url`/`url` following existing conventions.
- [ ] Config seeding: read `CLIO_HOOKS_FILE` after migration; create-if-missing
      by `(project,name)`; malformed file fails startup. Add the config field to
      the config loader and `README.md`.
- [ ] `deleteProject` (`projects.go:208`) counts `hooks`.
- [ ] Tests (`hooks_test.go`): CRUD lifecycle, duplicate name → 409, missing →
      404, bad event/kind/when/order/config → 422, project scoping (a hook in
      `default` never matches another project), disabled hooks listed but not
      dispatched, `secret` never returned, config seeding creates but does not
      clobber.
- [ ] Add a `SPEC-CONFORMANCE.md` row for the hooks registry.
- [ ] Verify (full sequence).

### Task 2: Transactional, queryer-threaded record writes

**Deliverable:** `createRecord`/`patchRecord` run in explicit transactions and
all record validation/lookup accepts a queryer, so hooks can read/write inside
the same transaction.

**Files:** `records.go`, `content_entries.go`, `records_test.go`,
`query_contract_test.go`.

- [ ] Extend the existing interface (`content_entries.go:162`) with
      `Query(query string, args ...any) (*sql.Rows, error)` to form
      `sqlQueryer`; keep `*sql.DB`/`*sql.Tx` satisfaction. Add
      `func (a *app) queryerOr(q sqlQueryer) sqlQueryer`.
- [ ] Add tx-aware variants: `getRecordWith(q, group, table, id)`,
      `validateReferenceWith(q, f, value)`, `validateValuesWith(q, …)`. The
      public wrappers call them with `a.db` so existing callers are unchanged.
- [ ] Convert `createRecord` to `Begin`/`Exec`/`Commit` (mirroring
      `deleteRecord`'s shape), re-reading the row through the tx so uncommitted
      data is visible; `patchRecord` likewise.
- [ ] Keep behaviour identical when no hooks exist: full existing test suite
      stays green, including `TestMetadataAndRecordCRUDValidation`,
      `TestQueryPageKeepsCorrectTopRowsAndOrdering`, and the UI tests.
- [ ] Add tests proving atomicity: a forced failure after the write (injected in
      a later task, but add the seam now) leaves no row; concurrent create+delete
      does not deadlock.
- [ ] Verify.

### Task 3: Hook engine, declarative executor and record dispatch

**Deliverable:** `record.*` events dispatch to matching hooks; the declarative
executor implements `set`/`default`/`validate`/`derive`/`ensure_reference`;
`reject` rolls back with `422`; actions commit atomically.

**Files:** `hooks.go`, `hook_declarative.go` (create), `records.go`,
`hooks_test.go`.

- [ ] Define `hookInput`/`hookResult` (§Design) and
      `func (a *app) dispatchHooks(q sqlQueryer, ev hookEvent) (*hookResult,
      *apiError)`, where `hookEvent` carries project/event/group/table/path,
      `record`, `previous`, and `fields`.
- [ ] Change-record flow in `createRecord`:
      validate → `dispatchHooks(before_create)` → apply `set`/`default` to
      `values` → run `create`/`link` actions in-tx → INSERT → `dispatchHooks(after_create)`
      → commit. `patchRecord` and `deleteRecord` analogous (`before/after_update`,
      `before/after_delete`).
- [ ] Apply actions in a single place (`applyHookResult`) so every executor
      shares validation and semantics. `create` re-enters `createRecord` with the
      in-tx queryer and a depth counter (bounded at 8).
- [ ] Declarative executor (`hook_declarative.go`): evaluate `config.ops` in
      order; template `$record.<field>`; `ensure_reference` find-or-create via
      the tx (dedupe by `match`); `validate` → `reject` on missing required
      fields; `derive` → `set`.
- [ ] `on_error`: `reject` aborts (`422`), `log` records a warning (no bodies)
      and continues. `enabled=false` and non-matching `when` are skipped.
- [ ] Tests:
      - `before_create` `set`/`default` visible on the created record;
      - `reject` returns `422` and stores nothing (assert both via API and by
        counting rows);
      - `ensure_reference` dedupes `(host, date)` and marks `origin=generated`
        across two creates; no duplicate occasion;
      - `after_create` creates and links a related row atomically; a failure in
        the second action leaves no rows (transaction rollback);
      - ordering by `order`, then `id`;
      - project scoping (hook never fires across projects);
      - UI parity: the server-rendered create form triggers hooks.
- [ ] Update the `SPEC-CONFORMANCE.md` row.
- [ ] Verify.

### Task 4: File hook dispatch at the content choke points

**Deliverable:** `file.before_put`/`after_put`/`before_delete`/`after_delete`
dispatch through the same engine, with overwrite rollback.

**Files:** `content_entries.go`, `upload_stage.go`, `files_api.go`, `webdav.go`,
`hooks.go`, `hooks_test.go`.

- [ ] `commitUploadLocked` (`upload_stage.go:105`): run `file.before_put` in the
      tx before `saveContentWrite`/`os.Rename`; on reject, roll back and discard
      the stage. Run `file.after_put` after the catalog write and rename, before
      commit.
- [ ] Overwrite rollback: for an existing target, move the current bytes to a
      temp sibling before the rename; restore on hook rejection or commit
      failure, delete on success. (This is the one place where an in-tx hook
      rejection must also undo a filesystem rename.)
- [ ] `deleteContentEntry`/`deleteContentEntriesUnder`
      (`content_entries.go:242/251`): wrap in a transaction; `before_delete` can
      veto; `after_delete` runs before commit. Callers (`files_api.go:561-655`,
      `webdav.go:317-329`) keep holding the content lock.
- [ ] Keep the documented exclusions: move/rename, rescan and ZIP upload fire no
      hooks; assert in a test.
- [ ] Tests: `before_put` reject stores nothing (API and WebDAV); `after_put`
      can create a side row atomically; overwrite rejection restores the previous
      bytes and catalog row; `before_delete` reject keeps the entry; delete
      subtree fires per path under one transaction.
- [ ] Verify.

### Task 5: Occasion example, spec §67 and docs

**Deliverable:** the gift/occasion use case shipped under `examples/`, the new
normative section, and all user-facing docs.

**Files:** `examples/` (new hook + README), `SPEC.md`, `README.md`, `health.go`,
`CHANGELOG.md`, `SPEC-CONFORMANCE.md`.

- [ ] Add a copy-pasteable declarative hook (`examples/hooks/ensure-occasion.json`)
      and wire it into the example walkthrough; document it in
      `examples/README.md`.
- [ ] Write `SPEC.md §67 Lifecycle hooks` (§Design above), update the TOC,
      header, versioning table and revision bullet `v1.11`; amend `§3.1`,
      `§4.2`, `§49`, `§51`, `§61`, `§66.6` per "Contract changes required".
- [ ] `README.md`: how to register a hook and the occasion example.
- [ ] `health.go` help text: `/hooks` routes, events, kinds, transaction
      semantics, limits.
- [ ] `CHANGELOG.md` `[Unreleased]` entry; `SPEC-CONFORMANCE.md` rows.
- [ ] Verify.

---

## Phase P2 — webhook executor and durable delivery

### Task 6: Webhook executor, outbox and delivery worker

**Deliverable:** `after_*` hooks of kind `webhook` enqueue a durable delivery
inside the write transaction; a background worker delivers with timeout,
bounded retries, HMAC and idempotency.

**Files:** `hooks.go`, `hook_webhook.go` (create), `hook_worker.go` (create),
`main.go`, `hooks_test.go`.

- [ ] Add `hook_deliveries` + index to the schema; extend `deleteProject`.
- [ ] Validate `config`: `url` (absolute http/https), optional `secret`,
      `headers` (string map, no hop-by-hop), `fuel`-style `timeout_ms` override.
- [ ] In `dispatchHooks`, when a matching hook is `webhook` and the event is
      `after_*`, insert a pending delivery row in the tx instead of calling out.
- [ ] `hook_worker.go`: `hookWorker` polls due rows, POSTs the canonical input,
      parses the response as `hookResult` (applying it is a no-op after commit;
      it is recorded), updates `status`/`attempts`/`next_attempt_at`; backoff
      `1s/5s/30s`; `status='failed'` after the last attempt. Sign with HMAC if
      `secret` set; send `X-Clio-Idempotency` and `X-Clio-Event`.
- [ ] Start the worker in the serve path and stop it via the existing context
      (`main.go:56-64,211`), draining in-flight deliveries on shutdown.
- [ ] Never log or persist bodies; `last_error` is a class/short message only.
- [ ] Tests with `httptest.Server`: delivery is queued iff the write commits
      (reject/rollback enqueues nothing); at-least-once and idempotency key
      stable across retries; timeout does not fail the committed write; retries
      bounded and then `failed`; HMAC verifies; `before_*` webhook is `422` at
      registration; a project's webhook never sees another project's event.
- [ ] Verify.

### Task 7: Hook delivery API, human page and webhook docs

**Deliverable:** observability and a human surface for hooks.

**Files:** `hooks.go`, `app.go`, `ui.go` (or new `hooks_ui.go`), `hooks_test.go`,
`SPEC.md`, `health.go`, `README.md`, `SPEC-CONFORMANCE.md`.

- [ ] `GET /hooks/{id}/deliveries` (pageable) exposing status/attempts/time only.
- [ ] `POST /hooks/{id}/test` dry-run: run the hook against a caller-supplied
      input and return the `hookResult` without persisting.
- [ ] Human page `GET /{project}/hooks` (list + enable/disable + delivery log),
      routed in `projectUI` (`app.go:193-220`); it uses the JSON API.
- [ ] Extend `§67` and docs for the delivery guarantee, retries, signing and
      redaction; update help text and conformance.
- [ ] Verify.

---

## Phase P3 — true function objects

### Task 8: WASM executor via `wazero`

**Deliverable:** sandboxed in-process function objects, no network/filesystem,
fuel/memory bounded.

**Files:** `hook_wasm.go` (create), `hooks.go`, `main.go`, `go.mod`, `go.sum`,
`hooks_test.go`, `testdata/` (a tiny test module), docs.

- [ ] Add `github.com/tetratelabs/wazero` (pure Go, no cgo); confirm the cgo
      build and `-tags sqlite_fts5` still work.
- [ ] `PUT /hooks/{id}/module` uploads to `hook_modules` (size-capped, compiled
      before commit to validate).
- [ ] `hook_wasm.go`: compile the module (bounded compile cache keyed by
      sha256), instantiate with **no** host imports (no WASI), set fuel
      (`config.fuel`, capped) and memory (`config.memory_bytes`, capped), call
      `config.entrypoint` (default `handle`) with the canonical input JSON, read
      the canonical output JSON. A trap/fuel exhaustion/timeout becomes a hook
      failure governed by `on_error`.
- [ ] Tests: a module that sets a field; one that rejects; one that loops
      forever hits the fuel cap; one attempting an import fails to instantiate;
      memory cap enforced; kind registration and module upload validation.
- [ ] Amend `§4.2`/`§61` and `go.mod` rationale in docs.
- [ ] Verify.

### Task 9: Expression executor

**Deliverable:** a tiny evaluator for conditions and derived values (and,
optionally, richer `when` conditions).

**Files:** `hook_expression.go` (create), `hooks.go`, `hooks_test.go`, docs.

- [ ] Grammar: literals, `$record.f`, `$previous.f`, comparisons, boolean
      `and`/`or`/`not`, parentheses; no assignment, no calls.
- [ ] `config.ops` of `set`/`default`/`reject` with expression values; usable as
      `when.if` on any hook kind.
- [ ] Tests: precedence, missing fields → null, type mismatch → validation
      error, reject path, `when.if` gating.
- [ ] Verify.

### Task 10: ClioJS client callbacks

**Deliverable:** non-authoritative client-side callbacks for portal DX.

**Files:** `assets/clio.js`, `testdata/cliojs_test.cjs`, `cliojs_test.go`,
`health.go`, `README.md`.

- [ ] Add `onRecordRender(record, table, el)`, `beforeQuery(query)` and a small
      `on(event, fn)` emitter.
- [ ] Bump `libraryVersion` to `1.7.0` and update the version assertions in
      `cliojs_test.go` / `testdata/cliojs_test.cjs` and the help text.
- [ ] Document that these never replace server hooks.
- [ ] Verify (including the Node-based ClioJS suite).

---

## File structure

| File | Change | Responsibility |
|---|---|---|
| `hooks.go` | **create** | model, validation, store, routing, engine, dispatch, delivery API |
| `hook_declarative.go` | **create** | P1 declarative executor |
| `hook_webhook.go` | **create** | P2 webhook executor (enqueue + sign) |
| `hook_worker.go` | **create** | P2 delivery worker lifecycle |
| `hook_wasm.go` | **create** | P3 wazero executor |
| `hook_expression.go` | **create** | P3 expression evaluator |
| `hooks_test.go` | **create** | registry, engine, record/file dispatch, delivery |
| `records.go` | modify | tx + queryer threading; hook dispatch |
| `content_entries.go` | modify | extend queryer; file hook points |
| `upload_stage.go` | modify | before/after put; overwrite rollback |
| `files_api.go`, `webdav.go` | modify | delete-under-lock hook wrapping |
| `app.go` | modify | `/hooks` route; human page dispatch |
| `projects.go` | modify | hook count in project emptiness |
| `main.go` | modify | schema, config seeding, worker lifecycle |
| `assets/clio.js` | modify (P3) | client callbacks; version bump |
| `SPEC.md`, `README.md`, `health.go`, `CHANGELOG.md`, `SPEC-CONFORMANCE.md`, `examples/` | modify | contract + docs |
| `go.mod`, `go.sum` | modify (P3) | `wazero` |

---

## Test matrix (acceptance criteria → tasks)

| Acceptance criterion (issue #47) | Task |
|---|---|
| Create/list/patch/delete a hook; invalid event/kind → 422 | 1 |
| `before_create` declarative runs in-transaction; `reject` rolls back 422 | 2, 3 |
| Hook creates + links a related record atomically; mid-hook failure leaves no partial rows | 2, 3 |
| Hooks project-scoped; never cross projects | 1, 3 |
| Webhook at-least-once + idempotency; timeout does not fail the write; bounded retries | 6 |
| WASM fuel/time limits; no network/filesystem | 8 |
| Ordering by `order`, then id | 3 |
| Occasion example deduped by `(host,date)`, marked generated | 3, 5 |
| UI/API parity | 3, 4 |

---

## Verification

Per task (and at the end of each phase):

```sh
go test -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
go build -tags sqlite_fts5 -buildvcs=false -o /tmp/gocl-clio-check .
```

Plus, when P3 lands, confirm a cgo build with `wazero` under the same tags, and
run the Node-based ClioJS suite (`TestClioJSBehaviorWithNode`) after the client
callback task.

---

## Open decisions (resolved for this plan)

- **Registration:** API-persisted SQLite + optional `CLIO_HOOKS_FILE` seeding
  (create-if-missing).
- **Executors:** `wasm` before `expression` in P3.
- **First use case:** generic hooks first; the occasion rule is a shipped
  declarative hook, not a narrow `auto_reference` property.
- **ID format:** reuse opaque `newID()`; do not add an `hk_` prefix.
- **Webhook events:** `after_*` only; `before_*` webhooks are `422`.
- **File move/rescan/ZIP:** no hooks (bulk paths), documented in §67.

## Deferred / out of scope

- Native Go plugins (`.so`).
- A server-side JavaScript runtime.
- Distributed/broker eventing; the webhook stream is not a general agent
  subscription (revisit separately).
- Hooks on instance-level routes (projects/health).
- Composite/alternative keys, cross-project hooks.
- Editable hook source/versioning and a hook template gallery.
