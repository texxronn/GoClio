# Clio

## Frozen Specification v1.1 — Go implementation contract

**Status: FROZEN**

This specification is the implementation contract for Clio v1.

The implementation must treat this consolidated document as authoritative. The integrated v1.1 clarifications in section 61 and optional authentication contract in section 62 supersede earlier wording wherever they differ.

The agent may make implementation-level decisions where the specification is silent, but must not introduce new product capabilities or materially change the defined behaviour without first updating the specification.

---

# 1. Product

**Name:** Clio

**Slogan:**

> **Fits in. Gets out of the way.**

Clio is a small, self-hosted, metadata-driven personal web and data service.

It provides:

* structured data storage
* metadata
* tables
* forms
* records
* time-series data
* querying and aggregation
* Markdown pages
* HTML pages
* directories
* browsable content trees
* stable/shareable URLs
* a JSON API
* an LLM-oriented `/help` endpoint
* client-side Markdown rendering

Clio is intended to cover the common 80% of lightweight personal data and publishing use cases without becoming a general-purpose database platform.

The implementation must remain small, understandable, reliable and low-maintenance.

---

# 2. Core philosophy

Clio follows these principles:

1. **80/20 over completeness**
2. **One service**
3. **One executable**
4. **One HTTP origin**
5. **One SQLite database**
6. **No external database dependency**
7. **No application-level cache in v1**
8. **Server-rendered HTML by default**
9. **Minimal JavaScript**
10. **Metadata drives behaviour**
11. **Metadata is queryable**
12. **URLs are stable and shareable**
13. **Everything useful has a URL**
14. **The service describes itself**
15. **Prefer boring code over clever architecture**
16. **Do not build features merely because they might be useful later**

Clio must remain small enough that one person can understand the entire repository.

---

# 3. Non-goals

Clio is explicitly not intended to become:

* a Baserow clone
* a NocoDB clone
* an enterprise database platform
* a spreadsheet
* a BI platform
* a workflow engine
* an automation platform
* a message broker
* a plugin framework
* a collaborative real-time editor
* a multi-tenant SaaS platform
* a full authentication/authorization framework
* a complex relational query engine
* a general-purpose SQL interface
* a visual application builder
* a frontend SPA framework

The following are also explicitly out of scope for v1:

* nested collection groups
* arbitrary SQL
* arbitrary formulas
* joins across tables for the public API
* window functions
* rolling time-series functions
* retention policies
* automatic downsampling
* continuous aggregates
* complex migration tooling
* external job queues
* Redis
* Elasticsearch
* separate time-series databases
* separate metadata databases

---

# 4. Technology

## 4.1 Language

Clio must be implemented in:

**Go 1.25**, as declared by the repository module.

Use the standard `net/http` server and Go goroutines for ordinary request handling. Go’s HTTP server already dispatches requests concurrently; do not add a reactive framework or custom event loop. The normal request model is:

```text
HTTP request
    ↓
net/http handler goroutine
    ↓
application logic
    ↓
SQLite / filesystem
    ↓
HTTP response
```

---

## 4.2 Frameworks

Prefer the Go standard library, including `net/http` and `database/sql`, with a small SQLite driver. Do not use a web framework, ORM, dependency-injection framework, reactive framework or frontend framework. Keep the dependency count low.

---

## 4.3 Database

Clio uses exactly:

**one SQLite database**

for:

* groups
* tables
* fields
* metadata
* records
* time-series data

SQLite must operate in:

**WAL mode**

There is no second SQLite database for time-series data.

There is no separate metadata database.

There is no external database server.

---

## 4.4 Filesystem

The filesystem is used for:

* Markdown pages
* HTML pages
* uploaded files
* directory/content-tree storage

The exact internal filesystem layout is an implementation detail.

---

## 4.5 Caching

**No application-level LRU or other record/metadata cache in v1.**

SQLite's own caching and normal indexes are sufficient.

Do not introduce:

* LRU caches
* TTL caches
* cache invalidation logic
* distributed caches

Prepared SQL statements may be retained when useful for code simplicity and performance, but this is not considered an application data cache.

If performance problems are discovered later, caching can be added based on evidence.

---

# 5. Runtime resource guidance

Clio is intended to be lightweight.

**128 MB is a reference engineering budget for normal personal workloads.** It
is a benchmark target, not a hard application memory limit or an assumption
about the resources assigned to a deployment.

Clio must not impose a fixed memory ceiling or add a product configuration for
one. It should adapt to the memory and CPU resources available in its
environment, using normal Go runtime and container-aware behavior. Correctness
must not change arbitrarily with the available memory. The service should
remain correct with 64 MB, 128 MB, 512 MB or 1 GB when the workload is
reasonable for those resources.

Do not add a custom memory-management subsystem or application-level cache.
Correctness and code simplicity take precedence over micro-optimization.

---

# 6. Deployment

Clio runs as a single process.

Conceptually:

```text
                  CLIO
                    │
        ┌───────────┼───────────┐
        │           │           │
      HTTP        SQLite     Filesystem
        │           │           │
      HTML       metadata     pages
      JSON       records      files
      forms      timeseries   directories
```

Example data directory:

```text
/var/lib/clio/
    clio.db
    content/
    pages/
    files/
```

---

# 7. Configuration

Minimum configuration:

```text
CLIO_ADDR
CLIO_DATA_DIR
CLIO_DB
CLIO_BASE_URL
```

Sensible defaults must exist.

Example:

```text
CLIO_ADDR=0.0.0.0:8080
CLIO_DATA_DIR=/var/lib/clio
CLIO_DB=/var/lib/clio/clio.db
CLIO_BASE_URL=https://clio.atrangi.com
```

`CLIO_BASE_URL` is the canonical public URL.

Clio must use the configured base URL when generating absolute URLs for:

* pages
* directories
* groups
* tables
* records
* API endpoints
* help
* shared links

---

# 8. Core data model

Clio has the following concepts:

```text
Clio
└── Collection Group
    ├── Table
    │   ├── Fields
    │   └── Records
    ├── Table
    └── Table
```

Separately:

```text
Clio
└── Content Tree
    ├── Directory
    │   ├── Directory
    │   ├── Markdown page
    │   └── HTML page
    └── ...
```

The collection/table hierarchy and content-tree hierarchy are separate concepts.

---

# 9. Collection groups

A **collection group** is a logical container for related tables.

Examples:

```text
Finance
    electricity
    gas
    fuel

Vehicle
    service
    fuel

Pool
    measurements
    maintenance
```

A group has:

```text
name
label
description
order
```

There is exactly **one level of grouping**.

Groups cannot contain groups.

A group does not represent a separate database.

All groups and tables exist within the single Clio SQLite database.

---

# 10. Tables

A **table** is the primary structured-data container.

The API may use the term `collection` for historical/semantic reasons, but table is the preferred human terminology.

Every table belongs to exactly one group.

A table has:

```text
group
name
label
description
kind
fields
```

Supported table kinds:

```text
record
timeseries
```

A table can be created through the API without modifying application code or restarting Clio.

---

# 11. Record tables

A `record` table is the normal general-purpose table.

Use it for:

* bills
* maintenance records
* inventories
* expenses
* appointments
* arbitrary structured records
* relational-style data using references

A record table supports:

* CRUD
* forms
* table views
* sorting
* filtering
* paging
* grouping
* aggregation

---

# 12. Time-series tables

A `timeseries` table is intended for measurements primarily indexed by time.

Examples:

```text
pool measurements
temperature
weight
power usage
sensor readings
fuel observations
```

A time-series table has a designated timestamp field.

Example:

```json
{
  "kind": "timeseries",
  "timestamp_field": "timestamp"
}
```

The timestamp field must have a suitable date/time type.

Time-series tables support:

* append
* retrieve
* list
* paging
* sorting
* filtering
* time-range queries
* grouping
* aggregation
* time bucketing

The storage implementation remains ordinary SQLite.

Time-series data does not require a separate database.

---

# 13. Fields

Initial field types:

```text
string
text
integer
decimal
boolean
date
datetime
enum
url
reference
```

A field may have:

```text
name
label
type
required
default
description
order
readonly
hidden
unique
role
```

Optional validation:

```text
min
max
min_length
max_length
pattern
```

An enum field has a finite set of allowed values.

A reference field identifies another table and record.

`unique`, when true, declares a database-enforced unique constraint for that field. A date or datetime field may use `role: "timestamp"` to designate the table's principal temporal field. A table has at most one such field; it must be required and have type `date` or `datetime`. For a `timeseries` table, the designated temporal field is its `timestamp_field`.

Complex relationship semantics are out of scope for v1.

---

# 14. Records

Every record has:

```text
id
created_at
updated_at
```

plus table-defined fields.

IDs are immutable.

Opaque IDs are preferred.

Time-series records additionally contain their configured timestamp field.

---

# 15. Metadata

Metadata is a first-class Clio capability.

Metadata includes:

* groups
* tables
* table kinds
* fields
* field types
* labels
* validation
* defaults
* references
* timestamp configuration
* unique field declarations
* explicit index declarations
* display properties

Metadata must be persistent.

Metadata must be queryable through the public API.

A client or LLM must never need to inspect the SQLite database directly to understand Clio's structure.

---

# 16. Metadata API

API v1 provides:

```text
GET /api/v1/metadata
GET /api/v1/metadata/groups
GET /api/v1/metadata/groups/{group}
GET /api/v1/metadata/groups/{group}/tables
GET /api/v1/metadata/groups/{group}/tables/{table}
```

Responses must expose enough information to understand:

* groups
* tables
* table kinds
* fields
* field types
* required fields
* defaults
* validation
* references
* timestamp configuration
* unique field declarations
* explicit index declarations
* labels
* relevant URLs

---

# 17. Group API

Create group:

```http
POST /api/v1/groups
Content-Type: application/json
```

Example:

```json
{
  "name": "vehicle",
  "label": "Vehicle",
  "description": "Vehicle-related data"
}
```

Get group:

```text
GET /api/v1/groups/{group}
```

List tables:

```text
GET /api/v1/groups/{group}/tables
```

The API may support updating group metadata.

Nested groups are never supported.

---

# 18. Table creation API

Tables must be creatable entirely through the API.

```http
POST /api/v1/groups/{group}/tables
Content-Type: application/json
```

Example:

```json
{
  "name": "service",
  "label": "Vehicle Service",
  "description": "Vehicle service records",
  "kind": "record",
  "fields": [
    {
      "name": "date",
      "label": "Service date",
      "type": "date",
      "required": true
    },
    {
      "name": "odometer",
      "label": "Odometer",
      "type": "integer",
      "required": true
    },
    {
      "name": "type",
      "label": "Type",
      "type": "enum",
      "required": true,
      "values": [
        "service",
        "repair",
        "tyre",
        "inspection"
      ]
    },
    {
      "name": "cost",
      "label": "Cost",
      "type": "decimal"
    },
    {
      "name": "notes",
      "label": "Notes",
      "type": "text"
    }
  ]
}
```

Time-series example:

```json
{
  "name": "measurements",
  "label": "Pool Measurements",
  "kind": "timeseries",
  "timestamp_field": "timestamp",
  "fields": [
    {
      "name": "timestamp",
      "type": "datetime",
      "required": true
    },
    {
      "name": "ph",
      "type": "decimal"
    },
    {
      "name": "free_chlorine",
      "type": "decimal"
    },
    {
      "name": "temperature",
      "type": "decimal"
    }
  ]
}
```

Tables may also declare advanced indexes as ordered field lists. For example:

```json
{
  "indexes": [
    {"fields": ["vehicle_id", "serviced_at"]},
    {"fields": ["category", "recorded_at"]}
  ]
}
```

Every indexed field must exist in the table. Index declarations are returned in table metadata and may be changed through table metadata updates. Clio reconciles the database indexes transactionally when fields or index declarations change.

---

# 19. Table metadata updates

v1 supports basic table metadata modification.

At minimum:

* label changes
* description changes
* adding fields
* changing display properties
* changing safe validation properties
* changing explicit index declarations

Removing or changing fields must not silently destroy existing data.

Complex schema migrations are out of scope.

---

# 20. Table API

Stable API root:

```text
/api/v1/
```

Group/table operations:

```text
GET    /api/v1/groups
GET    /api/v1/groups/{group}
POST   /api/v1/groups

GET    /api/v1/groups/{group}/tables
GET    /api/v1/groups/{group}/tables/{table}
POST   /api/v1/groups/{group}/tables
PATCH  /api/v1/groups/{group}/tables/{table}
DELETE /api/v1/groups/{group}/tables/{table}
```

---

# 21. Record API

```text
GET    /api/v1/groups/{group}/tables/{table}/records
GET    /api/v1/groups/{group}/tables/{table}/records/{id}
POST   /api/v1/groups/{group}/tables/{table}/records
PATCH  /api/v1/groups/{group}/tables/{table}/records/{id}
DELETE /api/v1/groups/{group}/tables/{table}/records/{id}
```

These endpoints apply to both:

```text
record
timeseries
```

tables.

---

# 22. Querying

Clio v1 provides simple server-side querying.

The objective is to cover common personal-data questions without creating a query language.

Supported operations:

```text
paging
filtering
sorting
distinct
grouping
aggregation
time-range filtering
time bucketing
```

### Query execution and resource behavior

The query implementation must not load a complete table into application
memory to filter, sort, page, group or aggregate it. Push those operations into
SQLite wherever practical, and materialize only the selected page or aggregate
results needed for the response. Avoid application-side caches and
table-sized intermediate datasets. Transient application memory should scale
with the query and response size, not with the total number of records. SQLite
may use its normal query planner and temporary storage to execute operations.

These requirements do not define a runtime memory ceiling or a fixed resource
threshold. The 128 MB reference budget is used to benchmark representative
personal workloads only; the same query behavior must remain correct across
different deployment resource assignments.

---

# 23. Paging

Supported parameters:

```text
limit
offset
```

Example:

```text
?limit=50&offset=100
```

Response:

```json
{
  "data": [],
  "page": {
    "limit": 50,
    "offset": 100,
    "count": 50,
    "total": 1842
  }
}
```

Offset paging is sufficient for v1.

Cursor pagination is not required.

Paged results must use deterministic ordering. Tables with a principal temporal field are ordered by that field descending and then by record ID descending unless the request supplies an explicit sort. Other tables use a suitable deterministic order. The internal pagination strategy remains an implementation detail.

Reasonable implementation limits may be imposed on `limit`.

---

# 24. Sorting

Supported parameters:

```text
sort
order
```

Example:

```text
?sort=date&order=desc
```

Fields must be validated against table metadata.

---

# 25. Filtering

Supported operators:

```text
eq
ne
gt
gte
lt
lte
contains
in
isnull
```

Examples:

```text
?type=service
?cost.gte=100
?date.gte=2026-01-01
?name.contains=Toyota
```

The exact parameter encoding may be selected by the implementation but must be documented and consistent.

All values must be parameterized SQL values.

Arbitrary SQL is prohibited.

---

# 26. Distinct

Example:

```text
?distinct=type
```

Returns the unique values for the requested field.

---

# 27. Grouping

Queries support **one-level grouping**.

Example:

```text
?group_by=type
```

or:

```text
?group_by=year
```

Nested grouping is not required.

---

# 28. Aggregation

Supported aggregate functions:

```text
count
sum
avg
min
max
```

Examples:

```text
?aggregate=count
```

```text
?aggregate=cost:sum
```

```text
?group_by=type&aggregate=count,cost:sum
```

Operations must be validated against the field type.

Examples:

* `count` is valid for all records
* `sum` is valid for numeric values
* `avg` is valid for numeric values
* `min` and `max` are valid for ordered values

Grouped response example:

```json
{
  "groups": [
    {
      "type": "service",
      "count": 12,
      "cost_sum": 1840.50
    },
    {
      "type": "repair",
      "count": 7,
      "cost_sum": 932.10
    }
  ]
}
```

---

# 29. Time-series querying

Time-series tables support:

```text
from
to
```

Example:

```text
?from=2026-09-01T00:00:00Z&to=2026-09-30T23:59:59Z
```

Time-series queries also support:

```text
bucket=hour
bucket=day
bucket=week
bucket=month
```

Example:

```text
?from=2026-09-01
&to=2026-09-30
&bucket=day
&aggregate=temperature:avg,temperature:min,temperature:max
```

This should produce data directly useful for reports and charts.

Only fixed simple buckets are required.

No support for:

* rolling windows
* arbitrary time expressions
* window functions
* automatic downsampling
* retention policies
* continuous aggregates

---

# 30. Query safety

Any field used for:

* filtering
* sorting
* grouping
* aggregation
* distinct
* time bucketing

must be validated against metadata.

SQL identifiers must never be copied directly from unvalidated user input.

Values must be parameterized.

The client must never be able to submit arbitrary SQL.

---

# 31. Query philosophy

The query system should answer common questions such as:

* How much did I spend?
* How many records do I have?
* What happened each month?
* What is the average?
* What are the latest 20 records?
* What are the values by category?
* What was the minimum and maximum?
* Give me daily averages for this measurement.

It must not evolve into a general-purpose analytical engine.

---

# 32. Human-facing table URLs

Table views use the reserved `/t/` namespace so they cannot collide with content paths.

```text
/t/{group}/{table}
/t/{group}/{table}/new
/t/{group}/{table}/{id}
/t/{group}/{table}/{id}/edit
```

Examples:

```text
/t/vehicle/service
/t/vehicle/fuel
/t/pool/measurements
/t/finance/electricity
```

These URLs must be stable.

---

# 33. Generic table UI

Every table automatically gets a generic HTML table view.

The view must:

* use metadata-defined labels
* display fields in metadata order
* link records to record pages
* provide New
* provide Edit where appropriate
* support basic filtering
* support sorting
* support paging
* support grouping/aggregation where useful
* support time-range controls for time-series tables

The UI does not need to expose every advanced API query option.

---

# 34. Generic forms

Every table automatically gets generic forms.

Routes:

```text
/t/{group}/{table}/new
/t/{group}/{table}/{id}/edit
```

Field controls:

```text
string       → text input
text         → textarea
integer      → numeric input
decimal      → numeric input
boolean      → checkbox
date         → date input
datetime     → datetime input
enum         → select
url          → URL input
reference    → select/search control
```

Server-side validation is authoritative.

Client-side validation is optional.

---

# 35. Content tree

The content tree is separate from structured-data groups and tables.

It consists of:

```text
Directory
Page
File
```

Directories may contain:

* child directories
* Markdown pages
* HTML pages
* files
* links to tables
* links to records

---

# 36. Directory URLs

Every directory has a stable URL.

Example:

```text
/pool
/pool/reports
/pool/reports/weekly
```

Directory URLs must be browsable.

A directory page displays:

* child directories
* pages
* relevant files
* breadcrumbs
* links to available resources

The root is browsable.

---

# 37. Directory API

The machine-readable directory API uses the `path` query parameter for nested paths:

```text
GET    /api/v1/directories
GET    /api/v1/directories?path=/...
POST   /api/v1/directories
DELETE /api/v1/directories?path=/...
```

`GET` must return directory metadata and children.

Example:

```json
{
  "path": "/pool",
  "url": "https://clio.example.com/pool",
  "children": [
    {
      "name": "measurements",
      "type": "directory",
      "url": "https://clio.example.com/pool/measurements"
    },
    {
      "name": "weekly.md",
      "type": "page",
      "content_type": "text/markdown",
      "url": "https://clio.example.com/pool/weekly.md"
    }
  ]
}
```

This endpoint must be suitable for machine and LLM discovery.

---

# 38. Creating directories

A directory may be created through:

* HTML form submission
* API

API:

```http
POST /api/v1/directories
Content-Type: application/json
```

Example:

```json
{
  "path": "/pool/reports"
}
```

Directory semantics must be deterministic and documented.

---

# 39. Pages

Clio supports:

```text
Markdown
HTML
```

A page has:

```text
name
title
content_type
content
created_at
updated_at
```

Pages have stable URLs.

---

# 40. Page API

Create/update page:

```http
POST /api/v1/pages
Content-Type: application/json
```

Markdown example:

```json
{
  "path": "/reports/electricity.md",
  "content_type": "text/markdown",
  "content": "# Electricity\n\nLatest usage..."
}
```

HTML example:

```json
{
  "path": "/reports/electricity.html",
  "content_type": "text/html",
  "content": "<h1>Electricity</h1><p>Latest usage...</p>"
}
```

The response must include:

* canonical path
* absolute URL
* content type
* timestamps

Markdown must render as HTML when viewed.

HTML must render as HTML.

---

# 41. Client-side Markdown rendering

Clio must provide a small client-side Markdown rendering capability.

The purpose is to allow a browser page or lightweight client-side script to:

1. fetch Markdown content
2. render it locally in the browser

This is particularly useful for generated pages and simple client-side applications built against Clio.

The client-side renderer must:

* require no frontend framework
* require no frontend build system
* have a small dependency footprint
* support the same practical Markdown subset used by normal Clio pages
* produce HTML suitable for display in the browser
* apply appropriate HTML sanitisation/safety rules

The implementation may use a small embedded/vendored Markdown parser if that materially improves correctness.

Clio should not require npm, Node.js or a separate frontend toolchain.

The renderer should be exposed as a simple browser-side asset/API, for example:

```text
/assets/clio-markdown.js
```

The asset URL is `/assets/clio-markdown.js`.

A minimal usage model should be possible:

```html
<script src="/assets/clio-markdown.js"></script>
<script>
  // Fetch Markdown from Clio and render it.
</script>
```

The exact JavaScript API should remain deliberately small.

The client-side renderer is a required v1 capability; individual pages may choose whether to use it.

Normal Clio page requests continue to support server-side rendering.

The same Markdown source must render consistently enough between server-side and client-side rendering for normal Clio content.

---

# 42. Directory-tree upload

Clio must support publishing an entire directory tree and its contents.

Example:

```http
POST /api/v1/directories
Content-Type: application/zip
```

Example archive:

```text
pool/
├── index.md
├── measurements/
│   └── latest.md
└── reports/
    ├── weekly.html
    └── monthly.md
```

The complete tree is published under the requested destination path.

The operation must:

* preserve relative paths
* reject absolute paths
* reject `..`
* reject invalid paths
* prevent extraction outside Clio's content directory
* have deterministic overwrite behaviour
* return resulting URLs

The implementation must guard against unreasonable archive expansion.

No external unzip command should be required unless there is a compelling implementation reason.

---

# 43. Stable URLs

Every published:

* directory
* page
* table
* record

must have a stable URL.

Changing a display label must not silently change the URL.

If the user supplies a path, Clio should preserve it.

If Clio generates a path, it must avoid collisions.

Absolute URLs use `CLIO_BASE_URL`.

URLs must be suitable for:

* bookmarks
* sharing
* external documents
* human communication
* LLM input

---

# 44. Help

Clio must expose:

```text
/help
/api/v1/help
```

`/help` returns an easy-to-read HTML page.

`/api/v1/help` returns concise machine-oriented documentation, preferably Markdown or structured JSON.

The help must describe the actual running service sufficiently for an LLM to use it automatically.

The help must include:

* what Clio is
* base URL
* API version
* groups
* tables
* record tables
* time-series tables
* metadata
* metadata API
* table creation
* fields
* forms
* table views
* CRUD
* filtering
* sorting
* paging
* grouping
* aggregation
* time-series querying
* directories
* directory API
* Markdown
* HTML
* client-side Markdown rendering
* directory-tree uploads
* URL conventions
* health
* errors
* important constraints
* concrete request/response examples

The purpose is that an LLM can be given:

```text
https://clio.example.com/help
```

and then immediately operate Clio.

The help endpoint is a first-class product feature.

---

# 45. Health

Clio must expose:

```text
/health
/api/v1/health
```

The browser-facing `/health` returns human-readable HTML.

The API endpoint returns JSON.

Example:

```json
{
  "status": "ok",
  "version": "1.0.0",
  "uptime_seconds": 12345,
  "memory": {
    "alloc_bytes": 18432000,
    "sys_bytes": 25165824,
    "heap_alloc_bytes": 12000000,
    "heap_inuse_bytes": 14000000
  },
  "database": {
    "status": "ok"
  },
  "groups": 5,
  "tables": 12,
  "records": 1842,
  "pages": 32,
  "directories": 11
}
```

Memory metrics should use Go runtime statistics appropriate to the implementation.

Health should answer:

* Is Clio alive?
* Which version?
* Uptime?
* Memory usage?
* Is SQLite healthy?
* Number of groups?
* Number of tables?
* Number of records?
* Number of pages?
* Number of directories?

---

# 46. API versioning

The stable external API begins at:

```text
/api/v1/
```

Incompatible API changes must use a future version such as `/api/v2/`.

Existing `/api/v1/` semantics must not silently change.

---

# 47. Complete API v1 summary

```text
GET    /api/v1/health
GET    /api/v1/help

GET    /api/v1/metadata
GET    /api/v1/metadata/groups
GET    /api/v1/metadata/groups/{group}
GET    /api/v1/metadata/groups/{group}/tables
GET    /api/v1/metadata/groups/{group}/tables/{table}

GET    /api/v1/groups
GET    /api/v1/groups/{group}
POST   /api/v1/groups

GET    /api/v1/groups/{group}/tables
GET    /api/v1/groups/{group}/tables/{table}
POST   /api/v1/groups/{group}/tables
PATCH  /api/v1/groups/{group}/tables/{table}
DELETE /api/v1/groups/{group}/tables/{table}

GET    /api/v1/groups/{group}/tables/{table}/records
GET    /api/v1/groups/{group}/tables/{table}/records/{id}
POST   /api/v1/groups/{group}/tables/{table}/records
PATCH  /api/v1/groups/{group}/tables/{table}/records/{id}
DELETE /api/v1/groups/{group}/tables/{table}/records/{id}

GET    /api/v1/directories
GET    /api/v1/directories?path=/...
POST   /api/v1/directories
DELETE /api/v1/directories?path=/...

GET    /api/v1/pages?path=/...
POST   /api/v1/pages
DELETE /api/v1/pages?path=/...
```

Client-side Markdown rendering is exposed as a static browser asset rather than a separate API service.

The optional ClioJS browser client is served at `/assets/clio.js` and `/assets/clio/v1/clio.js`; its contract is specified in section 63.

---

# 48. Persistence

SQLite is authoritative for:

* groups
* tables
* fields
* metadata
* records
* time-series data

SQLite must use WAL mode.

Pages/directories/files are stored in the Clio data directory.

Transactions must be used for operations that require atomic changes.

Data must survive process restart.

Clio must be able to open an existing database without special operational procedures.

---

# 49. SQLite indexing

Indexing is an implementation concern. Clio shall not index every field; it automatically indexes record identity, fields declared `unique`, reference fields, and the principal temporal field. Primary and group/table access indexes are also maintained. SQLite enforces uniqueness rather than relying on application checks.

Applications may declare additional single-field or compound indexes using the table `indexes` property. Compound field order is significant. Index declarations must be validated against table metadata and reconciled transactionally when the table schema changes; stale or duplicate indexes must be removed. Adding a unique index that conflicts with existing data fails without partially applying the schema change.

Time-series timestamp access uses an index scoped by table and timestamp. A table with a principal temporal field is paged in descending temporal order with record ID as a deterministic tie-breaker; other tables use a deterministic order. Offset/limit pagination remains sufficient for v1. Query operations continue to execute in SQLite and must not load whole tables into application memory.

Index configuration is optional for ordinary use, invisible in the primary UI, and requires no index-administration endpoint. Clio does not provide full-text/spatial indexes, index hints, workload-driven index creation, or query-plan analysis in v1.

---

# 50. Security

v1 may run in a trusted environment or use the optional single-user Basic Authentication described in section 62. Deployments may also provide authentication/access control at a reverse proxy.

Clio must nevertheless:

* validate all input
* enforce request size limits
* prevent path traversal
* prevent arbitrary filesystem access
* use parameterized SQL
* safely render HTML
* validate names/identifiers
* reject malformed JSON
* safely handle archive extraction
* prevent unreasonable archive expansion
* avoid exposing internal stack traces

A full identity and authorization system is out of scope. Optional single-user Basic Authentication is defined in section 62.

---

# 51. HTML and Markdown safety

Markdown and HTML are first-class page types.

The implementation must distinguish intentionally published HTML from user-entered text.

User-controlled text rendered into Clio-generated pages must be escaped appropriately.

Markdown output must be sanitised appropriately before being inserted into a page where necessary.

The client-side Markdown renderer must apply equivalent safety expectations.

Clio must not accidentally turn arbitrary record values into executable HTML/JavaScript.

Security behaviour must be documented.

---

# 52. Error handling

Minimum meaningful HTTP status codes:

```text
200 OK
201 Created
204 No Content
400 Bad Request
404 Not Found
409 Conflict
422 Unprocessable Entity
500 Internal Server Error
```

JSON error format:

```json
{
  "error": "validation_error",
  "message": "cost is required"
}
```

Error responses must be consistent.

Internal exception details must not be exposed through API responses.

---

# 53. Observability

v1 requires only basic logging.

Log at least:

* startup
* shutdown
* configuration failures
* HTTP failures
* database errors
* content publication failures
* archive upload failures

No external metrics system is required.

No distributed tracing is required.

`/health` is the primary built-in operational diagnostic.

---

# 54. Backup and restore

The entire Clio data directory must be straightforward to back up.

Documentation must describe:

* backup
* restore
* database integrity expectations

No external backup system is part of Clio.

---

# 55. Testing

The implementation must include automated tests.

## Data model

Test:

* group creation
* group metadata
* table creation
* table metadata
* record CRUD
* time-series creation
* time-series append
* time-range querying
* validation
* references where implemented
* automatic unique/reference/temporal indexes
* explicit compound index creation and removal
* index reconciliation after schema updates
* uniqueness violations and rollback

## Metadata

Test:

* root metadata
* group metadata
* table metadata
* field metadata
* API-created tables
* API-created time-series tables

## Querying

Test:

* paging
* filtering
* sorting
* distinct
* grouping
* count
* sum
* avg
* min
* max
* time ranges
* time buckets

## Content

Test:

* directory creation
* directory listing
* Markdown publishing
* HTML publishing
* directory-tree upload
* stable URL generation
* traversal rejection
* client-side Markdown rendering for representative Markdown content

## HTTP/API

Test:

* correct status codes
* malformed requests
* missing resources
* invalid fields
* invalid paths
* invalid archives
* `/health`
* `/help`

## UI

Test at minimum:

* root
* group/table listing
* table page
* new form
* edit form
* record page
* directory page
* Markdown rendering
* HTML rendering
* health page
* help page

Tests should verify behaviour rather than implementation details.

---

# 56. End-to-end acceptance test

A fresh Clio installation must work without custom application code.

## Step 1 — Create group

Create:

```text
Vehicle
```

## Step 2 — Create record table through API

Create:

```text
Vehicle / Service
```

Fields:

```text
date
odometer
type
cost
notes
```

## Step 3 — Use generated UI

Open:

```text
/t/vehicle/service
```

Then:

```text
/t/vehicle/service/new
```

Create a record.

Confirm:

* record appears
* record can be opened
* record can be edited
* changes persist

## Step 4 — Query data

Use the API to:

* list records
* sort
* filter
* page
* group
* aggregate

## Step 5 — Create time-series table

Create:

```text
Pool / Measurements
```

Fields:

```text
timestamp
ph
free_chlorine
temperature
```

Insert measurements.

Query:

* latest data
* time range
* daily buckets
* averages/min/max

## Step 6 — Query metadata

Retrieve metadata entirely through API.

Confirm that a client can discover:

* groups
* tables
* table kinds
* fields
* validation
* timestamps

## Step 7 — Publish content

Create:

```text
/pool/reports
```

Post Markdown.

Open its URL.

## Step 8 — Upload directory tree

Upload a complete tree containing:

```text
index.md
measurements/latest.md
reports/weekly.html
reports/monthly.md
```

Browse the tree.

Open every resulting page.

Confirm stable URLs.

## Step 9 — Client-side Markdown

Load Markdown content through the Clio API from a simple HTML page and render it using the Clio client-side Markdown asset.

No frontend framework or build process should be necessary.

## Step 10 — LLM discovery

Provide an LLM only with:

```text
https://clio.example.com/help
```

Ask it to:

1. discover Clio
2. create a `pool_measurements` time-series table
3. insert records
4. query the latest measurements
5. calculate daily aggregates
6. create a Markdown report
7. publish the report into a directory

The documentation and metadata APIs must contain enough information for the agent to accomplish this without human explanation.

---

# 57. Repository deliverables

The implementation agent must produce:

```text
1. Complete Go 1.25 implementation
2. Automated tests
3. Dockerfile
4. Container/run documentation
5. README
6. API v1 documentation
7. Metadata API documentation
8. Example record table
9. Example time-series table
10. Example Markdown pages
11. Example client-side Markdown usage
12. Backup/restore documentation
13. AGENTS.md
14. Example configuration
```

The repository must support a straightforward build/test flow, for example:

```sh
go test ./...
go build ./...
```

Container startup must be documented.

---

# 58. Repository constraints

The implementation must remain small.

Do not introduce:

* a framework because it is conventional
* an abstraction because it might be useful later
* a service boundary because the component sounds independent
* a cache because caching sounds like good practice
* a queue because background work might someday exist
* a frontend build system because modern web applications usually have one
* an ORM because SQL is inconvenient
* a separate database because time-series data exists

Prefer direct, obvious code.

The client-side Markdown renderer must remain a small supporting capability, not grow into a client application framework.

---

# 59. Definition of done

Clio v1 is complete when:

* one Go process runs the service
* `net/http` goroutines handle HTTP requests
* one SQLite database stores structured data
* SQLite is in WAL mode
* no application cache is required
* groups exist as one-level containers
* tables are metadata-driven
* record tables work
* time-series tables work
* tables can be created through API
* metadata is queryable
* metadata drives forms
* metadata drives table views
* CRUD works through UI and API
* paging works
* filtering works
* sorting works
* grouping works
* aggregation works
* time-range queries work
* time buckets work
* Markdown pages work
* HTML pages work
* client-side Markdown rendering works
* directories work
* directory trees are browsable
* complete directory trees can be uploaded
* resources have stable URLs
* base URL is configurable
* `/health` works
* `/help` works
* API v1 is documented
* API v1 is usable by an LLM
* no CORS configuration is required
* automated tests pass
* the implementation remains small and understandable

---

# 60. Final product boundary

The goal is **not** to build a smaller Baserow.

The goal is to build:

> **The smallest useful personal web and data service that can become whatever application is needed next.**

Clio should provide primitives, not endless features.

Its primary primitives are:

```text
Groups
Tables
Fields
Records
Queries
Directories
Pages
URLs
API
Metadata
```

Everything else should be built from those primitives.

# 61. Integrated v1.1 clarifications

The following clauses incorporate the former `SPEC-Addendum.md` into this contract. They are normative and resolve ambiguities in sections 1–60. Where wording differs, these v1.1 clauses take precedence. The separate addendum file is no longer part of the contract.

## 61.1 Human-facing URL namespaces

The earlier v1.0 examples allowed table URLs and content-directory URLs to occupy the same path.

This is resolved by using separate namespaces.

### 61.1.1 Content tree

Human-facing content occupies the root namespace:

```text
/
```

Examples:

```text
/pool
/pool/reports
/pool/reports/weekly.md
/vehicle
/vehicle/service-history
```

Directories, Markdown pages, HTML pages and files exist in this namespace.

### 61.1.2 Tables

Human-facing table URLs use the reserved `/t/` namespace:

```text
/t/{group}/{table}
/t/{group}/{table}/new
/t/{group}/{table}/{id}
/t/{group}/{table}/{id}/edit
```

Examples:

```text
/t/pool/measurements
/t/pool/measurements/new
/t/vehicle/service
/t/finance/electricity
```

This namespace is exclusively for structured Clio tables.

### 61.1.3 Reserved root paths

The following root paths are reserved by Clio:

```text
/api
/health
/help
/assets
/t
```

User-created content directories/pages may not use those exact root names.

The root content tree therefore cannot collide with Clio's API, operational endpoints, assets or table namespace.

---

## 61.2 Content-path rules

Content paths are UTF-8 paths consisting of path segments.

Rules:

* `/` is the root.
* Empty intermediate path segments are rejected.
* `.` and `..` segments are rejected.
* Backslash is rejected.
* Control characters are rejected.
* Path traversal is always rejected.
* Paths are case-sensitive.
* Paths are URL-encoded when transported in URLs.
* Canonical URLs use normal percent-encoding.
* A directory and page/file may not occupy the same exact canonical path.
* Paths are not silently renamed because of case changes or title changes.

A page path includes its extension when supplied.

Examples:

```text
/reports/weekly.md
/reports/summary.html
```

The extension is therefore part of the canonical content URL.

Clio must not silently remove or add `.md` or `.html`.

---

## 61.3 Content resource identity

A canonical content path identifies exactly one resource.

A resource may be:

```text
directory
page
file
```

Two resources may not share the same canonical path.

Creating a resource where another resource already exists returns:

```text
409 Conflict
```

unless an explicit overwrite operation is supported by that endpoint.

Display titles do not determine resource identity.

---

## 61.4 Directory API path handling

Because directory paths may contain multiple path segments, the machine API uses a query parameter rather than embedding an arbitrary path inside the API route.

### List root

```http
GET /api/v1/directories
```

### Get directory

```http
GET /api/v1/directories?path=/pool/reports
```

### Create directory

```http
POST /api/v1/directories
Content-Type: application/json
```

```json
{
  "path": "/pool/reports"
}
```

### Delete directory

```http
DELETE /api/v1/directories?path=/pool/reports
```

The API must return the canonical path and absolute URL.

---

## 61.5 Page read API

The v1.0 Page API is extended with a read operation.

### Get page source

```http
GET /api/v1/pages?path=/reports/electricity.md
```

The response is JSON:

```json
{
  "path": "/reports/electricity.md",
  "url": "https://clio.example.com/reports/electricity.md",
  "content_type": "text/markdown",
  "content": "# Electricity\n\nLatest usage...",
  "created_at": "2026-09-27T12:00:00Z",
  "updated_at": "2026-09-27T12:00:00Z"
}
```

For Markdown pages, `content` is always the original Markdown source.

For HTML pages, `content` is the stored HTML source.

The API does not return rendered HTML in this endpoint.

The normal human-facing page URL is responsible for server-side rendering.

This endpoint exists specifically so clients and LLM-generated/browser-side applications can retrieve the source content.

---

## 61.6 Client-side Markdown rendering contract

The client-side Markdown renderer consumes the `content` returned by:

```text
GET /api/v1/pages?path=...
```

For a Markdown page:

```text
API
 ↓
JSON.content
 ↓
Clio Markdown renderer
 ↓
HTML
```

The renderer must not require:

* Node.js
* npm
* React
* Vue
* a frontend build
* a separate server

The browser asset is exposed under:

```text
/assets/clio-markdown.js
```

The exact JavaScript API is implementation-defined but must be simple enough that a complete example fits in a small HTML page.

---

## 61.7 Directory-tree upload

A directory-tree upload specifies its destination through the `path` query parameter.

Example:

```http
POST /api/v1/directories?path=/pool/reports
Content-Type: application/zip
```

If omitted, the destination is the root:

```text
/
```

Archive entries are interpreted exactly relative to the destination.

For example:

```text
archive:
    weekly.md
    measurements/latest.md
```

uploaded to:

```text
/pool/reports
```

produces:

```text
/pool/reports/weekly.md
/pool/reports/measurements/latest.md
```

There is no automatic stripping of the archive's top-level directory.

If the archive contains:

```text
pool/
    weekly.md
```

and the destination is `/reports`, the result is:

```text
/reports/pool/weekly.md
```

---

## 61.8 Directory-tree upload limits

The following limits apply in v1:

```text
Maximum compressed upload size: 32 MiB
Maximum expanded size:          256 MiB
Maximum file count:             10,000
Maximum individual file size:   16 MiB
Maximum directory depth:        32
```

These are safety limits, not expected workload limits.

An archive exceeding any limit must be rejected before publication.

Archive entries must:

* be relative
* not contain `..`
* not be absolute
* not escape the destination directory
* not use invalid filesystem paths

---

## 61.9 Directory-tree upload overwrite behaviour

By default:

```text
overwrite=false
```

If an archive would replace an existing page, file or incompatible resource, the operation returns:

```text
409 Conflict
```

The API may accept:

```text
?path=/pool/reports&overwrite=true
```

to explicitly permit replacement.

Existing directories may be reused.

The upload operation should validate the entire archive before modifying the published tree.

The implementation should stage the archive before committing the resulting tree so that validation errors do not leave a partially published archive.

---

## 61.10 Record JSON representation

The structured-data API uses the following JSON representations.

| Clio type | JSON representation                               |
| --------- | ------------------------------------------------- |
| string    | JSON string                                       |
| text      | JSON string                                       |
| integer   | JSON integer                                      |
| decimal   | JSON string containing canonical decimal notation |
| boolean   | JSON boolean                                      |
| date      | `"YYYY-MM-DD"`                                    |
| datetime  | RFC 3339 UTC string                               |
| enum      | JSON string                                       |
| url       | JSON string                                       |
| reference | JSON string containing target record ID           |

Example:

```json
{
  "id": "01J...",
  "created_at": "2026-09-27T12:00:00Z",
  "updated_at": "2026-09-27T12:00:00Z",
  "date": "2026-09-27",
  "odometer": 82431,
  "type": "service",
  "cost": "183.42",
  "notes": "Annual service"
}
```

Using a JSON string for `decimal` prevents loss of precision in clients and is especially appropriate for financial values.

---

## 61.11 Missing and null values

Every defined field is returned in a record representation.

If a field has no value:

```json
"cost": null
```

is returned.

Missing and null therefore have different meanings in requests but not in normal record responses.

## On create

If a field is omitted:

1. If a default exists, the default is applied.
2. Otherwise the field becomes `null`.
3. If the field is required, the request fails validation.

If the field is explicitly:

```json
"cost": null
```

the field is null.

A required field may not be null.

## On PATCH

An omitted field means:

> leave the existing value unchanged.

An explicit:

```json
"cost": null
```

means:

> clear the value.

A required field may not be cleared.

---

## 61.12 Unknown and readonly fields

Unknown fields in create/update requests are rejected.

Example:

```text
422 Unprocessable Entity
```

A client may not set system fields:

```text
id
created_at
updated_at
```

A client may not set a field marked:

```text
readonly=true
```

Attempting to do so returns:

```text
422 Unprocessable Entity
```

System-managed fields are returned normally in records.

---

## 61.13 Table schema changes

v1 schema modification rules:

### Allowed

* add a field
* change label
* change description
* change display properties
* change non-destructive validation metadata

### Type changes

A field's type may only be changed when the table contains no records.

This avoids implicit data conversion.

### Field deletion

A field may only be deleted when there are no stored values for that field.

Otherwise the operation returns:

```text
409 Conflict
```

Clio must not silently discard existing data.

---

## 61.14 References

A `reference` field contains the target record ID.

References are enforced by Clio.

Deleting a record that is referenced by another record returns:

```text
409 Conflict
```

unless all referencing values are cleared first.

Deleting a table that is referenced by another table returns:

```text
409 Conflict
```

unless those references are removed first.

There is no automatic cascade deletion in v1.

A nullable reference may be explicitly set to `null`.

A required reference may not be null.

---

## 61.15 Query parameter encoding

Query parameters use normal URL query encoding.

Field names used by the query API may not contain:

```text
.
```

This allows unambiguous filter parameter names.

Reserved parameters include:

```text
limit
offset
sort
order
distinct
group_by
aggregate
from
to
bucket
```

---

## 61.16 Filtering syntax

Filters use:

```text
filter.{field}
filter.{field}.eq
filter.{field}.ne
filter.{field}.gt
filter.{field}.gte
filter.{field}.lt
filter.{field}.lte
filter.{field}.contains
filter.{field}.in
filter.{field}.isnull
```

Examples:

```text
?filter.type=service
```

```text
?filter.cost.gte=100
```

```text
?filter.name.contains=Toyota
```

```text
?filter.cost.isnull=true
```

For `in`, the parameter may be repeated:

```text
?filter.type.in=service&filter.type.in=repair
```

Repeated `in` values are treated as an OR within that field.

Different filter fields/operators are combined using AND.

Example:

```text
?filter.type.in=service&filter.cost.gte=100
```

means:

```text
(type == service) AND (cost >= 100)
```

Values are URL-decoded according to normal HTTP query semantics.

Clients must percent-encode special characters as required by URL encoding.

---

## 61.17 Null ordering

When sorting:

* non-null values sort before null values
* null values always sort last
* this remains true for both ascending and descending order

This behaviour is fixed in v1.

---

## 61.18 Distinct response

Example:

```http
GET /api/v1/groups/vehicle/tables/service/records?distinct=type
```

Response:

```json
{
  "values": [
    "inspection",
    "repair",
    "service"
  ],
  "page": {
    "limit": 100,
    "offset": 0,
    "count": 3,
    "total": 3
  }
}
```

Paging applies to distinct values.

`null` may appear as a distinct value.

---

## 61.19 Ungrouped aggregate response

Example:

```http
GET /api/v1/groups/vehicle/tables/service/records?aggregate=count,cost:sum,cost:avg
```

Response:

```json
{
  "aggregate": {
    "count": 19,
    "cost_sum": "3174.42",
    "cost_avg": "167.0747368421"
  }
}
```

Aggregate field names use:

```text
{field}_{aggregate}
```

except `count`, which is simply:

```text
count
```

Decimal aggregate results are JSON strings using canonical decimal notation.

---

## 61.20 Grouped aggregate response

Example:

```text
?group_by=type&aggregate=count,cost:sum
```

Response:

```json
{
  "groups": [
    {
      "type": "inspection",
      "count": 4,
      "cost_sum": "420.00"
    },
    {
      "type": "repair",
      "count": 7,
      "cost_sum": "932.10"
    },
    {
      "type": "service",
      "count": 8,
      "cost_sum": "1822.32"
    }
  ],
  "page": {
    "limit": 100,
    "offset": 0,
    "count": 3,
    "total": 3
  }
}
```

Grouped results are pageable.

---

## 61.21 Time-series time semantics

Time-series timestamps are:

* stored logically in UTC
* returned as RFC 3339 UTC timestamps
* compared in UTC

Example:

```text
2026-09-27T12:30:00Z
```

Clients may provide RFC 3339 timestamps containing offsets.

Clio normalizes them to UTC.

For example:

```text
2026-09-27T22:30:00+10:00
```

and:

```text
2026-09-27T12:30:00Z
```

represent the same instant.

---

## 61.22 Time-series range semantics

For:

```text
from
to
```

the time interval is:

```text
[from, to)
```

That means:

* `from` is inclusive
* `to` is exclusive

Both parameters must be RFC 3339 timestamps.

Example:

```text
?from=2026-09-01T00:00:00Z
&to=2026-10-01T00:00:00Z
```

selects all measurements during September 2026.

---

## 61.23 Time-series bucket semantics

Buckets use UTC.

Supported:

```text
hour
day
week
month
```

Bucket starts are:

* hour: beginning of UTC hour
* day: 00:00 UTC
* week: Monday 00:00 UTC
* month: first day of month at 00:00 UTC

Example:

```text
bucket=day
```

returns:

```json
{
  "buckets": [
    {
      "bucket_start": "2026-09-27T00:00:00Z",
      "count": 24,
      "temperature_avg": "20.43"
    }
  ],
  "page": {
    "limit": 1000,
    "offset": 0,
    "count": 1,
    "total": 1
  }
}
```

Only buckets containing at least one record are returned.

Empty buckets are not synthesized.

---

## 61.24 HTML trust boundary

HTML pages are considered **trusted published content**.

Clio stores and serves HTML page content without sanitizing or rewriting it.

This means an HTML page may contain executable JavaScript.

Therefore:

* Clio must be deployed where publishers are trusted.
* The write API must be protected by the surrounding deployment if untrusted users can access Clio.
* Clio does not attempt to turn arbitrary user-submitted HTML into a safe multi-user content platform.
* Authorization beyond the single configured identity in section 62 remains an external deployment concern in v1.

Markdown and Clio-generated HTML views must still escape/sanitize untrusted data appropriately.

This boundary must be clearly documented in the README and `/help`.

---

## 61.25 CLIO_ADDR

`CLIO_ADDR` specifies the local HTTP bind address using:

```text
host:port
```

Examples:

```text
0.0.0.0:8080
127.0.0.1:8080
:8080
```

Clio serves HTTP by default. It may terminate TLS directly when a certificate
and private-key pair are configured; TLS termination may instead be performed
by a trusted reverse proxy as described in section 62.

`CLIO_BASE_URL` is independent of `CLIO_ADDR`.

Example:

```text
CLIO_ADDR=0.0.0.0:8080
CLIO_BASE_URL=https://clio.atrangi.com
```

---

## 61.26 API path parameter rules

The API uses query parameters for arbitrary content paths.

This avoids ambiguity caused by nested path parameters.

Examples:

```text
GET /api/v1/directories?path=/pool/reports
GET /api/v1/pages?path=/pool/reports/weekly.md
DELETE /api/v1/directories?path=/pool/reports
```

Group and table identifiers are simple path parameters because they are restricted identifiers.

---

## 61.27 Identifier rules

Group and table names must:

* be non-empty
* begin with a lowercase ASCII letter or digit
* contain only lowercase ASCII letters, digits, `_` and `-`
* be case-sensitive by definition but normalized to lowercase on creation
* not contain `.`
* not contain `/`

Example:

```text
pool
vehicle
vehicle_service
pool-measurements
```

Display labels may contain arbitrary Unicode text.

Content paths have the separate content-path rules defined above.

---

## 61.28 v1 scope

All capabilities explicitly listed in the v1.0 specification and this clarification are required v1 capabilities.

The following are particularly important and are not optional:

* API v1
* metadata API
* table creation API
* record CRUD
* time-series support
* query/filter/sort/paging
* grouping
* aggregation
* time bucketing
* directory API
* Markdown/HTML publishing
* directory-tree upload
* stable URLs
* `/health`
* `/help`
* client-side Markdown renderer

The client-side renderer remains intentionally tiny and must not evolve into a frontend framework.

---

## 61.29 Updated API v1 summary

```text
GET    /api/v1/health
GET    /api/v1/help

GET    /api/v1/metadata
GET    /api/v1/metadata/groups
GET    /api/v1/metadata/groups/{group}
GET    /api/v1/metadata/groups/{group}/tables
GET    /api/v1/metadata/groups/{group}/tables/{table}

GET    /api/v1/groups
GET    /api/v1/groups/{group}
POST   /api/v1/groups

GET    /api/v1/groups/{group}/tables
GET    /api/v1/groups/{group}/tables/{table}
POST   /api/v1/groups/{group}/tables
PATCH  /api/v1/groups/{group}/tables/{table}
DELETE /api/v1/groups/{group}/tables/{table}

GET    /api/v1/groups/{group}/tables/{table}/records
GET    /api/v1/groups/{group}/tables/{table}/records/{id}
POST   /api/v1/groups/{group}/tables/{table}/records
PATCH  /api/v1/groups/{group}/tables/{table}/records/{id}
DELETE /api/v1/groups/{group}/tables/{table}/records/{id}

GET    /api/v1/directories
GET    /api/v1/directories?path=/...
POST   /api/v1/directories
DELETE /api/v1/directories?path=/...

GET    /api/v1/pages?path=/...
POST   /api/v1/pages
DELETE /api/v1/pages?path=/...
```

Static browser support:

```text
/assets/clio-markdown.js
```

---

## 61.30 Updated acceptance tests

The original acceptance tests remain valid, with these additional requirements.

## Routing

Verify that all of the following can coexist:

```text
/pool/measurements
/t/pool/measurements
```

where the first is a content resource and the second is a table.

No routing ambiguity exists.

## Markdown API

Create:

```text
/pool/reports/latest.md
```

through the API.

Retrieve it through:

```text
GET /api/v1/pages?path=/pool/reports/latest.md
```

Verify that the original Markdown source is returned.

Use that source with:

```text
/assets/clio-markdown.js
```

and verify browser rendering.

## Directory upload

Upload:

```text
weekly.md
measurements/latest.md
```

to:

```text
/pool/reports
```

Verify the resulting paths are:

```text
/pool/reports/weekly.md
/pool/reports/measurements/latest.md
```

Verify the configured upload limits are enforced.

## Record semantics

Verify:

* omitted field uses default or becomes null
* required omitted field fails
* explicit null clears nullable field
* required field cannot be null
* omitted PATCH fields remain unchanged
* unknown fields are rejected
* readonly fields are rejected
* decimals round-trip without floating-point conversion

## Query semantics

Verify:

* paging
* exact filtering
* operator filtering
* `in`
* `isnull`
* sorting
* null ordering
* distinct response
* aggregate response
* grouped response
* time-range boundaries
* UTC normalization
* daily/weekly/monthly buckets

---

## 62. Optional authentication and transport security

Clio supports an optional, intentionally small single-user access gate. This
section integrates the requirements from `SPEC-Addendum-Auth.md`; this section
is authoritative if wording differs. Authentication remains disabled by
default, and enabling it does not change the API or data model.

### 62.1 Authentication model

The only built-in authentication scheme is HTTP Basic Authentication with one
configured username and password hash. There are no user tables, roles,
sessions, authorization rules, registration, password reset, OAuth, OIDC, JWT,
or MFA. The configured identity has access to the whole Clio instance.

When enabled, authentication applies to every HTTP route, including health,
help, published content, directories, table views, API endpoints, and static
assets. Health is not an unauthenticated exception.

### 62.2 Configuration

Optional settings and defaults are:

| Setting | Default | Meaning |
| --- | --- | --- |
| `CLIO_AUTH_ENABLED` | `false` | Require Basic Authentication. |
| `CLIO_AUTH_USER` | `admin` | The one configured username. |
| `CLIO_AUTH_PASSWORD_HASH` | unset | Required when authentication is enabled; a bcrypt hash. |
| `CLIO_REQUIRE_HTTPS` | `true` | Require encrypted transport or an allowed trusted HTTP source when authentication is enabled. |
| `CLIO_TRUSTED_HTTP_NETWORKS` | `127.0.0.0/8,::1/128` | Comma-separated CIDRs allowed to use HTTP; loopback is always allowed. |
| `CLIO_TRUST_PROXY` | `false` | Allow trusted reverse-proxy protocol information to identify HTTPS. |
| `CLIO_TRUSTED_PROXY_NETWORKS` | empty | Comma-separated CIDRs of immediate trusted proxy peers. Required when `CLIO_TRUST_PROXY=true`. |
| `CLIO_TLS_CERT` | unset | Optional certificate file for direct TLS; must be configured with `CLIO_TLS_KEY`. |
| `CLIO_TLS_KEY` | unset | Optional private-key file for direct TLS; must be configured with `CLIO_TLS_CERT`. |

Invalid booleans, malformed CIDRs, an incomplete TLS key pair, or trusted-proxy
mode without trusted proxy CIDRs are startup configuration errors. A missing
or invalid password hash is an error when authentication is enabled. Never
put a plaintext password in persistent configuration.

Clio uses bcrypt for password hashing. Generate a compatible hash with:

```sh
clio hash-password
```

The command prompts for the password without echoing it and prints only the
hash, which can be supplied as `CLIO_AUTH_PASSWORD_HASH` through a secret
manager or protected environment file. The application never logs credentials
or the configured hash.

### 62.3 HTTPS policy

When authentication is enabled and `CLIO_REQUIRE_HTTPS=true`, Clio accepts
requests only when at least one of these conditions holds:

* Clio received the request over direct TLS.
* The immediate peer address is loopback.
* The immediate peer address is within `CLIO_TRUSTED_HTTP_NETWORKS`.
* Trusted-proxy mode is enabled, the immediate peer belongs to
  `CLIO_TRUSTED_PROXY_NETWORKS`, and that proxy indicates the original protocol
  was HTTPS.

Private network ranges are not implicitly trusted. Operators must explicitly
add any trusted LAN or container network to the HTTP allowlist. When
`CLIO_REQUIRE_HTTPS=false`, HTTP is accepted by explicit operator choice.

The optional `CLIO_TLS_CERT` and `CLIO_TLS_KEY` pair enables direct TLS on the
configured `CLIO_ADDR`. Otherwise Clio serves plain HTTP and may sit behind a
TLS-terminating reverse proxy.

Forwarded protocol headers are ignored unless `CLIO_TRUST_PROXY=true` and the
immediate peer address is in `CLIO_TRUSTED_PROXY_NETWORKS`. Only a single
`X-Forwarded-Proto: https` value is accepted as evidence of HTTPS; untrusted,
duplicated, or chained values cannot bypass transport enforcement. A reverse
proxy must replace, rather than append to, untrusted forwarded-protocol input.

When transport requirements are not met, Clio returns `403 Forbidden` and
does not redirect the request. This avoids redirecting a request after
credentials may have been sent over an insecure connection.

### 62.4 Authentication responses and logging

Missing or invalid credentials return `401 Unauthorized` and:

```text
WWW-Authenticate: Basic realm="Clio"
```

The response must not reveal whether a username exists or whether a password
was wrong. Authentication failures are logged with a generic reason category;
logs must never contain plaintext passwords, Authorization headers, password
hashes, or request bodies. Brute-force rate limiting remains the responsibility
of a reverse proxy or deployment network policy.

Basic Authentication does not add a CORS requirement. Browsers and ordinary
HTTP clients use their standard Basic Authentication support. When enabled,
README and `/help` document the authentication state, hash generation, HTTPS
policy, trusted HTTP networks, trusted-proxy requirements, and the fact that
Basic Authentication must be protected by TLS on untrusted networks.

### 62.5 Authentication and transport tests

Automated tests must cover:

* disabled authentication and enabled authentication;
* valid, missing, and invalid credentials, including the challenge header;
* protected API, HTML, health, and static-asset routes;
* direct TLS, localhost HTTP, trusted HTTP CIDRs, rejected untrusted HTTP, and
  explicit HTTP allowance when HTTPS enforcement is disabled;
* trusted-proxy HTTPS acceptance and rejection of spoofed forwarded headers
  from untrusted peers;
* invalid CIDRs/proxy settings and missing or invalid password hashes.

---

# 63. ClioJS browser client

Clio serves an optional, lightweight browser JavaScript client over the existing
Clio v1 REST API. This section integrates `SPEC-Addendum-ClioJS.md` and is
authoritative if wording differs. ClioJS adds no API endpoints or semantics.

Use the ClioJS script itself to implement Data Browsing.

## 63.1 Distribution and scope

The stable script URL is `/assets/clio.js`; the versioned URL
`/assets/clio/v1/clio.js` serves the compatible v1 client. A plain HTML page can
load it directly with `<script src="/assets/clio.js"></script>`. ClioJS requires
no package installation, Node.js, bundler, transpiler, frontend framework, or
build step. Its sole global is `Clio` and it identifies its library and API
versions through `Clio.version` and `Clio.apiVersion`.

ClioJS is a thin wrapper around `fetch()` and the existing HTTP API. It must not
become an ORM, application framework, state store, persistence layer, or cache.
The server remains authoritative; the client introduces no alternate query,
validation, time-series, or concurrency semantics.

`Clio.DataBrowser.mount(element)` is the sole built-in UI component in the
ClioJS asset. It is narrowly scoped to the read-only collection browser in
section 64 and does not establish a general application framework or state
store.

## 63.2 Client operations

`new Clio()` targets the origin from which the script is served and uses
same-origin browser credentials. An explicit `baseUrl` may be provided. The
client exposes convenient access to metadata and groups, and a table handle
obtained with `clio.table(group, table)`. Table handles support metadata,
record listing/querying, get/create/update/delete, and the lightweight
`list`, `first`, `count`, `distinct`, and `aggregate` conveniences. The client
also supports creating groups/tables and accessing page source and directories.

Record query options map predictably to the documented API query parameters:
`limit`, `offset`, `sort`, `order`, filters, time bounds, buckets, grouping,
aggregates, and distinct. A filter object maps keys to `filter.{field}` or
`filter.{field}.{operator}` parameters; repeated filter values are preserved.
`query()` returns the server's response shape. `records()` is an asynchronous
iterator that requests later pages as needed, advances by the returned page
count, and honors the server's effective page size rather than assuming a
requested size was granted.

Calls accept `AbortSignal` where applicable. Failed HTTP requests reject with
an error exposing HTTP `status`, Clio error `code`, and the server's `message`.
The client relies on browser-standard HTTP Basic Authentication and must not
store credentials.

## 63.3 Data and Markdown behavior

The client preserves JSON types returned by Clio. In particular, decimal field
values remain strings and are never converted to JavaScript floating-point
numbers; dates and datetimes remain strings as well. Record PATCH requests pass
omitted and explicit-null properties through unchanged so the API retains its
defined partial-update semantics.

`clio.page(path)` returns the original page source and metadata from the page
API. ClioJS integrates with the separately served `/assets/clio-markdown.js`
renderer through `Clio.Markdown.render(source)` when that renderer is loaded.
Markdown safety requirements remain those in sections 41 and 61.6.

## 63.4 Help and verification

`/help` documents both asset URLs, same-origin usage, a short example, and the
available client capabilities so browser users and generated pages can discover
ClioJS without a separate package. Tests cover its static routes, metadata,
table and record operations, query encoding, paging/async iteration, errors,
decimal preservation, page/directory access, cancellation where implemented,
and Markdown integration. The asset must work in an ordinary browser page.

---

# 64. Collection Data Browser

This section integrates `SPEC-Addendum-DataBrowsing.md` and is authoritative
for the built-in browser interface. It is an inspection and navigation view,
not a spreadsheet editor; it does not create, edit, or delete records.

## 64.1 Routes and layout

The canonical table-browser URI is:

```text
/collections/{group}/{table}
```

The path uses the existing group identifier for the collection segment. The
query parameter `page` is one-based; the browser maps it to the API's existing
`limit` and `offset` parameters. `/collections` opens the browser and selects
the first available group and table. `/collections/{group}` selects the first
table in that group. These are browser entry points, not new API endpoints.

The browser is a compact, information-dense, three-part layout: a collection
selector, a table-navigation pane, and a read-only data grid. The selector
lists groups available through the API. Selecting another collection refreshes
the table navigation, keeps the current table name if it exists in that group
or selects its first table, and updates the URL. Selecting a table and changing
pages likewise update the URL and support normal browser history navigation.

## 64.2 Client-side rendering

The browser shell is served at `/collections` and its nested routes. It loads
`/assets/clio.js` and uses `Clio.DataBrowser.mount(element)` to retrieve group,
table, metadata, and record data through ClioJS and the existing v1 API. No new
API endpoint, frontend dependency, package installation, or build step is
introduced. The browser renders data in the user's browser; server-side table
and form views at `/t/{group}/{table}` remain available.

The grid displays visible fields in metadata order with metadata-defined labels.
Hidden fields are omitted. Record values are rendered as text, not executable
HTML. Decimal values remain the strings returned by the API. The browser shows
the selected table and current record range and provides previous/next and
numbered page navigation. Its initial page size is 50 records; page navigation
maps to `limit=50` and `offset=(page-1)*50` and uses the API response's total.

## 64.3 Boundaries and verification

The browser is for inspection and navigation only. Record creation and editing
remain in the existing `/t/` forms or API. Collection paths are reserved from
published content so they cannot shadow the browser routes.

Tests cover browser route handling and methods, loading ClioJS, group/table
navigation, metadata-ordered visible columns, safe text rendering, URL/page
state, empty collections/tables, and API error display. No external service or
frontend framework is required.

---
