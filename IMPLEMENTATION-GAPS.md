# GoClio implementation gaps

This checklist tracks work against the consolidated contract in [`SPEC.md`](SPEC.md).
Items are addressed in order, with each change verified before moving to the
next item. Update the status and evidence here as work lands.

## Work queue

### 1. Create directories from the browser UI — complete

The directory page lists content but has no create form, and the content UI
rejects `POST`. The API can create directories. The contract requires both
HTML form submission and API creation.

- [x] Add a directory creation form and POST handler.
- [x] Validate that the submitted name is a single safe path segment.
- [x] Cover successful creation, duplicate names, and invalid names in HTTP tests.

### 2. Add automated regression coverage — complete

The automated suite now covers the required data, API, content, rendering, and
UI behaviors, including the documented archive resource limits and common
error responses.

- [x] Add a reusable test setup for an isolated SQLite database and content directory.
- [x] Add tests for record CRUD, metadata reads/updates, validation, and references.
- [x] Add tests for filtering, sorting, paging, distinct values, aggregates, and time-series range/bucket behavior.
- [x] Add tests for page/directory APIs, Markdown and HTML rendering safety, ZIP path traversal/overwrite, depth limits, and per-file/total expanded-size rejection.
- [x] Add tests for compressed archive size, entry-count limits, malformed requests, invalid query parameters, missing resources, method errors, and page/directory validation.
- [x] Add HTTP/UI tests for the root and group/table listings, table pages, new/edit forms, record pages and deletion, directory creation, health, help, and the Markdown asset.

### 3. Complete query controls in the table UI — complete

The table page now exposes metadata-driven grouping and aggregate selectors,
time-series range and bucket controls, and renders aggregate, grouped, and
bucketed results. Filtering, sorting, and paging controls retain the selected
query parameters when navigating results.

- [x] Add usable grouping and aggregate controls where applicable.
- [x] Add time-bucket controls for time-series tables.
- [x] Keep paging and filtering behavior consistent with the API.

### 4. Complete service lifecycle and request logging — complete

The server handles interrupt and termination signals with a graceful drain and
forced-close timeout. Failed HTTP responses and recovered request panics are
logged without request paths, query strings, bodies, or panic details.

- [x] Handle termination signals and shut down the HTTP server cleanly.
- [x] Log shutdown and failed HTTP responses without exposing sensitive content.

### 5. Complete repository delivery artifacts — complete

Repository delivery artifacts now include a non-root Docker image and run
instructions, project guidance and a supported-settings example, plus runnable
record, time-series, Markdown, and client-rendering samples.

- [x] Dockerfile and container startup instructions.
- [x] `AGENTS.md` and example configuration.
- [x] Example record/time-series metadata and Markdown/client-rendering examples.

The consolidated specification and README already document the API, metadata,
build/run flow, and backup/restore.

### 6. Check query memory use against the reference resource budget — complete

- [x] Measure representative workloads and identify a practical dataset size.
- [x] Reduce transient memory use for paged, sorted, distinct, grouped, aggregate, and bucket queries.

Queries were benchmarked on synthetic 10,000- and 50,000-row tables with
`category` (string), `amount` (integer), and a representative note field. A
100-row page was used for list workloads. The 50,000-row test is the practical
personal-data workload measured here, not an enforced table-size limit.

Before the query changes, a 50,000-row default page allocated 122.1 MB per
query; explicit sorting allocated 335.9 MB, and a global aggregate allocated
100.8 MB. After SQLite pushdown, total Go allocations per operation were 0.19
MB for a default page, 1.07 MB for a sorted page, 9.82 MB for a global
aggregate, 0.03 MB for distinct values, and 9.83 MB for grouped aggregates.
These are total allocations, not peak resident memory; aggregation preserves
exact numeric semantics while SQLite streams values through the aggregate.

Sampling Linux `VmRSS` during one-shot 50,000-row queries measured peak process
RSS of 22.6–26.7 MiB across default-page, sorted-page, global-aggregate,
distinct and grouped-aggregate workloads. Sampled Go heap growth above the
post-GC baseline was 0.2–3.3 MiB. The 128 MB figure remains a reference
benchmark, not a runtime limit or deployment assumption. Reproduce the
allocation and heap sampling runs with:

```sh
go test -run '^$' -bench '^BenchmarkQueryRecordsMemory$' -benchtime=1x -benchmem
go test -run '^$' -bench '^BenchmarkQueryRecordsWorkingSet$' -benchtime=1x -benchmem
```

For the process-RSS measurement, build the test binary with `go test -c`, run
each `rows_50000` subbenchmark once, and sample `/proc/<pid>/status`'s `VmRSS`
while it runs (the measurements above sampled at 0.5 ms intervals).

Filtering, time ranges, sorting, paging, distinct selection, grouping and
bucket/aggregate operations are executed by SQLite. Clio decodes only selected
record pages or aggregate result pages; no table-sized application dataset or
query cache is retained. Exact decimal/integer comparison and aggregate
semantics are preserved, and query behavior does not depend on an assumed
memory ceiling.

## Verification log

- Initial review: `go test ./...` compiled the package but reported `[no test files]`.
- Initial review: `go vet ./...` passed.
- Initial review: `go build -buildvcs=false` passed. Plain `go build` could not read VCS status from the empty `.git` directory in the supplied workspace.
- Directory UI gap: added isolated HTTP tests for root/nested creation, duplicate and invalid names, and JSON API compatibility.
- Test suite expansion: added HTTP-level coverage for metadata/schema updates, record CRUD and validation, references, query operations, UTC ranges/all bucket units, page and directory APIs, Markdown safety, table forms, and ZIP path safety/overwrite/depth/expanded file size.
- Regression completion: added archive compressed-size, total-expanded-size, and entry-count limit tests; expanded API request/query, page/directory error, root/table UI, edit/delete, and page lifecycle coverage.
- Regression fix: content paths blocked by an existing file now return a conflict instead of an internal server error.
- Table query UI: added group/aggregate controls, grouped and aggregate result tables, time-series bucket/range controls, and query-preserving pagination; regression tests cover grouping, aggregation, filtering/sorting, paging, and time buckets.
- Service lifecycle: SIGINT/SIGTERM trigger graceful HTTP shutdown with a 10-second drain timeout; failed responses and recovered panics log method/status without URL or panic content.
- Lifecycle regression tests: verify active requests finish during signal-driven shutdown and failure/panic logs omit request paths and query values.
- Delivery artifacts: added a non-root multi-stage Docker image, persistent-volume startup instructions, repository guidance, supported environment example, and API table/publishing samples.
- Artifact verification: sample metadata and record payloads pass through HTTP tests; Markdown and browser-rendered HTML examples publish and serve successfully. Docker image builds and its health page was smoke-tested.
- Current: `GOCACHE=/tmp/gocl-go-cache go test ./...` passes.
- Current: `go test -count=1 -cover ./...` passes with 61.2% statement coverage after replacing application-side query processing with SQLite-backed queries.
- Current: `GOCACHE=/tmp/gocl-go-cache go vet ./...` passes.
- Current: `GOCACHE=/tmp/gocl-go-cache go build -buildvcs=false -o /tmp/gocl-clio-audit .` passes.
- Query-memory gap: added representative 10,000-/50,000-row allocation and sampled heap benchmarks; HTTP regression coverage exercises bounded sorted pages, ties, offsets, and null ordering.
- Query-memory results: after SQLite pushdown, 50,000-row workloads peaked at 22.6–26.7 MiB RSS in the test process; default-page allocations fell from 122.1 MB to 0.19 MB per query.
- Query resource behavior: SPEC now treats 128 MB as a reference benchmark; no application memory limit, memory-limit setting, or cache was introduced. HTTP tests check exact large-number semantics and bounded allocations for 10,000-row distinct, grouped and filtered-page queries.
- Current query-memory verification: `go test -count=1 ./...`, `go test -count=1 -cover ./...`, `go vet ./...` and `go build -buildvcs=false -o /tmp/gocl-clio-check .` pass.
