# Clio

## Consolidated Specification v1.5 — Go implementation contract

**Status: FROZEN**

This specification is the implementation contract for Clio v1. It is a single
consolidated document: sections 1–64 are the complete, equally normative contract.
The former v1.1 clarifications and addenda have been merged into the topical
sections rather than appended, so there are no separate clarification or appendix
parts. Where two statements appear to conflict, the conflict is a defect in this
document and must be reported and resolved here rather than decided ad hoc in code.

### Document, product and API versioning

These version identifiers are independent and must not be conflated:

| Identifier | Value | Where it appears |
| --- | --- | --- |
| Specification revision | `1.4` | This document |
| Product version | `1.0.0` | `version` in `/api/v1/health` |
| API version | `v1` | `/api/v1/` route prefix |

A specification revision does not imply a product or API version change. An
incompatible API change requires a new API version (`/api/v2/`); a product release
that keeps `/api/v1/` semantics may increment the product version independently.

### Normative language

- **must / must not** — required for conformance.
- **should / should not** — strongly recommended; deviations need justification.
- **may** — genuinely optional.
- Text labelled **Informative** is explanatory only and is not a conformance
  requirement.

Example payloads are illustrative unless a rule references them explicitly.

### Changelog

- **v1.0** — original product and API contract.
- **v1.1** — added clarifications (former `SPEC-Addendum.md`) and the optional
  authentication and transport contract (former `SPEC-Addendum-Auth.md`).
- **v1.2** — consolidated the document:
  - removed contradictions between the original sections and the clarifications
    (time-range formats, directory-tree upload, page overwrite semantics, reserved
    root paths, API summaries, versions);
  - added the query defaults, limits and semantics that were previously implicit
    (null filter behaviour, `contains`, single-field sorting, deterministic default
    ordering, virtual time groups, aggregate defaults, distinct ordering,
    preconditions);
  - added the value/format rules that were previously implicit (canonical decimal
    notation and average precision, request coercions, record ID and uniqueness
    semantics, reference shape, reserved field names);
  - added content/publishing details (canonical path and page write constraints,
    upload response, Markdown subset and safety);
  - expanded the error-code table, health and backup notes, and the explicit
    out-of-scope list;
  - recorded the automatic index and schema-change behaviour in one place.
- **v1.3** — merged the former clarification and appendix sections into the topical
  body and renumbered the document contiguously (1–63); updated all internal
  cross-references.
- **v1.4** — added the optional `decimal_format` query parameter (section 14.6)
  so record reads can return decimal fields as JSON numbers for consumers that
  cannot handle string decimals, while the default string representation and all
  existing responses are unchanged.
- **v1.5** — added the content filesystem (section 64): stable content-entry
  identifiers, the files API and `/f/{id}` stable URLs, the `attachment` field
  type, native text extraction with a bounded SQLite full-text index, the search
  and enrichment APIs, optional WebDAV exposure at `/dav`, and the corresponding
  backup requirements. Added `attachment` to the field types (section 13),
  reserved `/f` and `/dav` (section 32.3), added a files count to health
  (section 47), and removed the prohibition on renaming or moving published
  content (section 3.1).

### Table of contents

<!-- TOC -->
- [1. Product](#1-product)
- [2. Core philosophy](#2-core-philosophy)
- [3. Non-goals](#3-non-goals)
  - [3.1 Explicitly not in v1](#31-explicitly-not-in-v1)
- [4. Technology](#4-technology)
  - [4.1 Language](#41-language)
  - [4.2 Frameworks](#42-frameworks)
  - [4.3 Database](#43-database)
  - [4.4 Filesystem](#44-filesystem)
  - [4.5 Caching](#45-caching)
- [5. Runtime resource guidance](#5-runtime-resource-guidance)
- [6. Deployment](#6-deployment)
- [7. Configuration](#7-configuration)
  - [7.1 CLIO_ADDR](#71-clio_addr)
- [8. Core data model](#8-core-data-model)
- [9. Collection groups](#9-collection-groups)
  - [9.1 Identifier rules](#91-identifier-rules)
- [10. Tables](#10-tables)
- [11. Record tables](#11-record-tables)
- [12. Time-series tables](#12-time-series-tables)
- [13. Fields](#13-fields)
  - [13.1 Field names](#131-field-names)
  - [13.2 Field properties](#132-field-properties)
  - [13.3 Type-specific rules](#133-type-specific-rules)
  - [13.4 References](#134-references)
- [14. Records](#14-records)
  - [14.1 Record JSON representation](#141-record-json-representation)
  - [14.2 Canonical decimal notation](#142-canonical-decimal-notation)
  - [14.3 Request value coercion](#143-request-value-coercion)
  - [14.4 Missing and null values](#144-missing-and-null-values)
  - [14.5 Unknown and readonly fields](#145-unknown-and-readonly-fields)
  - [14.6 Numeric decimal output](#146-numeric-decimal-output)
- [15. Metadata](#15-metadata)
- [16. Metadata API](#16-metadata-api)
- [17. Group API](#17-group-api)
- [18. Table creation API](#18-table-creation-api)
- [19. Table metadata updates](#19-table-metadata-updates)
  - [19.1 Table schema changes](#191-table-schema-changes)
- [20. Table API](#20-table-api)
  - [20.1 API path parameter rules](#201-api-path-parameter-rules)
- [21. Record API](#21-record-api)
- [22. Querying](#22-querying)
  - [22.1 Query parameter encoding](#221-query-parameter-encoding)
  - [22.2 Query execution and resource behavior](#222-query-execution-and-resource-behavior)
- [23. Paging](#23-paging)
- [24. Sorting](#24-sorting)
  - [24.1 Null ordering](#241-null-ordering)
- [25. Filtering](#25-filtering)
  - [25.1 Filtering syntax](#251-filtering-syntax)
  - [25.2 Operator semantics](#252-operator-semantics)
- [26. Distinct](#26-distinct)
  - [26.1 Distinct response](#261-distinct-response)
- [27. Grouping](#27-grouping)
- [28. Aggregation](#28-aggregation)
  - [28.1 Ungrouped aggregate response](#281-ungrouped-aggregate-response)
  - [28.2 Grouped aggregate response](#282-grouped-aggregate-response)
- [29. Time-series querying](#29-time-series-querying)
  - [29.1 Time-series time semantics](#291-time-series-time-semantics)
  - [29.2 Time-series range semantics](#292-time-series-range-semantics)
  - [29.3 Time-series bucket semantics](#293-time-series-bucket-semantics)
- [30. Query safety](#30-query-safety)
- [31. Query philosophy](#31-query-philosophy)
- [32. Human-facing URL namespaces](#32-human-facing-url-namespaces)
  - [32.1 Content tree](#321-content-tree)
  - [32.2 Tables](#322-tables)
  - [32.3 Reserved root paths](#323-reserved-root-paths)
- [33. Generic table UI](#33-generic-table-ui)
- [34. Generic forms](#34-generic-forms)
- [35. Collection Data Browser](#35-collection-data-browser)
  - [35.1 Routes and layout](#351-routes-and-layout)
  - [35.2 Client-side rendering](#352-client-side-rendering)
  - [35.3 Boundaries and verification](#353-boundaries-and-verification)
- [36. Content tree](#36-content-tree)
  - [36.1 Content-path rules](#361-content-path-rules)
  - [36.2 Content resource identity](#362-content-resource-identity)
- [37. Directory URLs](#37-directory-urls)
- [38. Directory API](#38-directory-api)
  - [38.1 Directory API path handling](#381-directory-api-path-handling)
- [39. Creating directories](#39-creating-directories)
- [40. Pages](#40-pages)
- [41. Page API](#41-page-api)
  - [41.1 Page read API](#411-page-read-api)
- [42. Client-side Markdown rendering](#42-client-side-markdown-rendering)
  - [42.1 Client-side renderer contract](#421-client-side-renderer-contract)
- [43. ClioJS browser client](#43-cliojs-browser-client)
  - [43.1 Distribution and scope](#431-distribution-and-scope)
  - [43.2 Client operations](#432-client-operations)
  - [43.3 Data and Markdown behavior](#433-data-and-markdown-behavior)
  - [43.4 Help and verification](#434-help-and-verification)
- [44. Directory-tree upload](#44-directory-tree-upload)
  - [44.1 Upload semantics](#441-upload-semantics)
  - [44.2 Directory-tree upload limits](#442-directory-tree-upload-limits)
  - [44.3 Directory-tree upload overwrite behaviour](#443-directory-tree-upload-overwrite-behaviour)
- [45. Stable URLs](#45-stable-urls)
- [46. Help](#46-help)
- [47. Health](#47-health)
- [48. API versioning](#48-api-versioning)
- [49. Complete API v1 summary](#49-complete-api-v1-summary)
- [50. Error handling](#50-error-handling)
- [51. Persistence](#51-persistence)
- [52. SQLite indexing](#52-sqlite-indexing)
- [53. Security](#53-security)
- [54. Optional authentication and transport security](#54-optional-authentication-and-transport-security)
  - [54.1 Authentication model](#541-authentication-model)
  - [54.2 Configuration](#542-configuration)
  - [54.3 HTTPS policy](#543-https-policy)
  - [54.4 Authentication responses and logging](#544-authentication-responses-and-logging)
  - [54.5 Authentication and transport tests](#545-authentication-and-transport-tests)
- [55. HTML and Markdown safety](#55-html-and-markdown-safety)
  - [55.1 HTML trust boundary](#551-html-trust-boundary)
- [56. Observability](#56-observability)
- [57. Backup and restore](#57-backup-and-restore)
- [58. Testing](#58-testing)
- [59. End-to-end acceptance test](#59-end-to-end-acceptance-test)
- [60. Repository deliverables](#60-repository-deliverables)
- [61. Repository constraints](#61-repository-constraints)
- [62. Definition of done](#62-definition-of-done)
  - [62.1 v1 scope](#621-v1-scope)
- [63. Final product boundary](#63-final-product-boundary)
- [64. Content filesystem, attachments, search, and WebDAV](#64-content-filesystem-attachments-search-and-webdav)
<!-- /TOC -->

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

## 3.1 Explicitly not in v1

Beyond the non-goals above, the following are deliberately absent from v1 and
must not be assumed by clients:

* multi-field sorting (`sort` names exactly one field);
* response field projection or `fields=` selection;
* `ETag`/`Last-Modified` conditional requests and all other caching headers;
* CORS headers or cross-origin browser access (v1 is same-origin);
* group update or delete endpoints (section 17);
* file versioning, and per-file sharing or permissions beyond section 54;
  explicit rename and move of content entries are supported through the content
  filesystem (section 64);
* record or table import/export endpoints beyond the ZIP directory upload.

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

## 7.1 CLIO_ADDR

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
by a trusted reverse proxy as described in section 54.

`CLIO_BASE_URL` is independent of `CLIO_ADDR`.

Example:

```text
CLIO_ADDR=0.0.0.0:8080
CLIO_BASE_URL=https://clio.atrangi.com
```

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

## 9.1 Identifier rules

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

# 10. Tables

A **table** is the primary structured-data container.

"Collection" is not an API resource in v1. Tables are addressed as `tables` in
the API. The term survives only in the human-facing data-browser routes under
`/collections` (section 35) and in UI copy, where a "collection" means a group
and its tables. It is not a synonym for a table.

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
attachment
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

## 13.1 Field names

Field names follow the same identifier rules as group and table names
(section 9.1): non-empty, begin with a lowercase ASCII letter or digit, and
contain only lowercase ASCII letters, digits, `_` and `-`. Field names are
normalized to lowercase on creation, are unique within a table, and must not
contain `.` (section 22.1).

The system-managed record fields `id`, `created_at` and `updated_at` are
reserved: a field may not use those names.

## 13.2 Field properties

- `label` defaults to the field name. Labels may contain arbitrary Unicode text.
- `order` controls display order; fields are returned in `order` (then creation
  order) and the generic UI displays them in that order.
- `readonly` fields may not be set by clients in create or update requests
  (section 14.5).
- `hidden` fields are omitted from the generic UI and the data browser grid, but
  are still returned by the API.
- `unique` declares a database-enforced uniqueness constraint **for that field
  within its table**. Uniqueness is enforced by SQLite, not by an application
  check. Because SQL treats `NULL` values as distinct, multiple records may hold
  a null value in a unique field; uniqueness applies only to non-null values.
  Adding or enabling a unique constraint whose existing data contains duplicates
  fails with `409 Conflict` and does not partially apply the schema change.
- `role` may only be `"timestamp"`. A date or datetime field may use
  `role: "timestamp"` to designate the table's principal temporal field. A table
  has at most one such field; it must be `required` and have type `date` or
  `datetime`. For a `timeseries` table, the designated temporal field is its
  `timestamp_field` and the two must agree.
- `default` is applied when a create request omits the field. A required field
  may not have a null default. Defaults are expressed in the field's request
  representation: integers as integral JSON numbers, decimals as JSON strings
  (section 14.1).

## 13.3 Type-specific rules

- An `enum` field requires a non-empty list of unique string values; a record
  value must be one of them.
- A `url` field must be an absolute URL containing a scheme.
- A `reference` field identifies a record in another (or the same) table. Its
  field definition must include `group` and `table` properties naming the target
  table. The stored value is the target record ID (section 14.1), and Clio
  validates that the target exists on create, update and default application.
  References are enforced as described in section 13.4.
- An `attachment` field stores a content-entry ID (section 64.9). Clio validates
  that the target entry exists on create, update and default application, and a
  referenced entry cannot be deleted until the referencing value is cleared.
- `min`/`max` apply to ordered types: `string`, `text`, `url`, `enum`,
  `integer`, `decimal`, `date` and `datetime`. `min_length`/`max_length` apply to
  `string`, `text`, `url` and `enum`, and are measured in Unicode code points.
  `pattern` is a regular expression matched against the value's text form.
  `min` must not exceed `max`, and `min_length` must not exceed `max_length`.

Complex relationship semantics are out of scope for v1.

## 13.4 References

A `reference` field definition names its target with `group` and `table`
properties, and its stored value is the target record ID (section 14.1). Targets
may be in the same group or another group. A reference is validated when it is
created, updated, or supplied as a field default, and a reference to a
non-existent record is rejected.

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

# 14. Records

Every record has:

```text
id
created_at
updated_at
```

plus table-defined fields.

Every defined field is returned in a record representation; fields without a
value are returned as JSON `null` (section 14.4).

Records are identified by an opaque `id`. IDs are generated by Clio, immutable,
and **globally unique across the instance**, not merely within a table. This is
required because a `reference` value stores only the target record ID
(section 13.4). The concrete format of an ID is an implementation detail and is
not part of the API contract; clients must treat IDs as opaque strings and must
not parse, sort or assume a length. (The reference implementation generates
32-character lowercase hexadecimal IDs.)

`created_at` and `updated_at` are system-managed RFC 3339 UTC timestamps.
Clients may not set them (section 14.5). `updated_at` changes on every update;
the identifier and creation time never change.

Time-series records additionally contain their configured timestamp field.

## 14.1 Record JSON representation

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
| attachment | JSON string containing a content-entry ID        |

Example:

```json
{
  "id": "9f2c4e8a1b3d4f5061728394a5b6c7d8",
  "created_at": "2026-09-27T12:00:00Z",
  "updated_at": "2026-09-27T12:00:00Z",
  "date": "2026-09-27",
  "odometer": 82431,
  "type": "service",
  "cost": "183.42",
  "notes": "Annual service"
}
```

Using a JSON string for `decimal` prevents loss of precision in clients and is especially appropriate for financial values. Record reads may opt into JSON numbers with `decimal_format=number` (section 14.6).

## 14.2 Canonical decimal notation

A canonical decimal:

* has an optional leading `-`;
* has at least one integer digit and no redundant leading zeros (`0`, not `00`);
* has an optional fractional part with no trailing zeros and no trailing `.`;
* never uses exponent notation;
* represents negative zero as `0`.

So `183.42`, `0`, `-1.5` and `100` are canonical; `0183.420`, `1e3` and `-0`
are not. Stored decimal values are always canonicalized to this form. Decimal
aggregate results are also canonical, except that `avg` is computed to at most
ten decimal places and then canonicalized (for example `"167.0747368421"`).
`sum` over decimals is exact; integer `sum` is a JSON integer rather than a
string.

## 14.3 Request value coercion

Requests are strict about JSON types; Clio does not guess:

| Type | Accepted request JSON |
| --- | --- |
| `string`, `text`, `url`, `enum`, `reference` | JSON string |
| `integer` | JSON number with an integral value (`1.0` is accepted and normalized to `1`; `1.5` and `"5"` are rejected) |
| `decimal` | JSON string (not a JSON number) |
| `boolean` | JSON boolean |
| `date` | `"YYYY-MM-DD"` |
| `datetime` | RFC 3339 timestamp; normalized to UTC |

A `datetime` value supplied with an offset is normalized to UTC (section 29.1).
An `enum` value must be one of the declared values, and a `url` must be an
absolute URL with a scheme.

## 14.4 Missing and null values

Every defined field is returned in a record representation.

If a field has no value:

```json
"cost": null
```

is returned.

Missing and null therefore have different meanings in requests but not in normal record responses.

### On create

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

### On PATCH

An omitted field means:

> leave the existing value unchanged.

An explicit:

```json
"cost": null
```

means:

> clear the value.

A required field may not be cleared.

## 14.5 Unknown and readonly fields

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

## 14.6 Numeric decimal output

The default record representation encodes `decimal` fields as JSON strings
(section 14.1). Record reads accept an optional `decimal_format` query parameter
for consumers that require numeric JSON:

```text
decimal_format=string   (default) decimal fields are JSON strings
decimal_format=number   decimal fields are JSON numbers
```

`decimal_format` is valid on:

```text
GET /api/v1/groups/{group}/tables/{table}/records
GET /api/v1/groups/{group}/tables/{table}/records/{id}
```

It also applies to the decimal-valued results of `distinct`, `aggregate`,
`group_by` and `bucket` queries issued on the record list endpoint. Any other
value returns:

```text
422 Unprocessable Entity
```

The numeric form is the field's canonical decimal notation written as a JSON
number literal; the digits are not altered, so precision is preserved. Null
values remain `null`, and non-decimal fields are unaffected. The default remains
`string`; omitting the parameter or supplying `string` produces the existing
response unchanged.

Consumers that need exact decimal arithmetic should still prefer the default
string form or a decimal-aware parser, because a JSON number may be read as a
binary floating-point value by some clients.

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

A group's `name`, `label`, `description` and `order` are fixed at creation in
v1. There is no group update endpoint and no group delete endpoint in v1.

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

## 19.1 Table schema changes

v1 schema modification rules:

### Allowed

* add a field
* change label, description and `order`
* change display properties (`readonly`, `hidden`)
* change non-destructive validation metadata
* enable or disable `unique` on a field, subject to the duplicate check below
* change explicit `indexes` declarations

### `fields` is additive

A `PATCH` request's `fields` property is a partial, additive list of field
definitions, merged by name. A supplied field that names an existing field is
merged onto that definition (omitted properties keep their existing values); a
supplied field with a new name is appended. Existing fields that are not named
in the request are retained with their definitions and stored values unchanged.

An empty or partial `fields` list therefore never removes fields. This keeps the
documented "add a field" operation safe when a client sends only the new field.

To remove fields, name them explicitly in the `remove_fields` property:

```json
{"remove_fields": ["notes"]}
```

A field may not be named in both `fields` and `remove_fields` in the same
request. Naming a field that does not exist returns `422 Unprocessable Entity`.

### Type changes

A field's type may only be changed when the table contains no records.

This avoids implicit data conversion.

### Required fields added to a non-empty table

A required field added to a table that already contains records must declare a
`default`. Otherwise the operation returns `409 Conflict`, because existing
records could not satisfy the new constraint. The default (or the existing
value) is validated against the field's constraints before the change is
committed.

### Field deletion

A field may only be deleted when there are no stored values for that field.

Deletion is requested explicitly through `remove_fields`. Otherwise the
operation returns:

```text
409 Conflict
```

### Uniqueness

Enabling `unique` on a field whose existing records already contain duplicates
returns `409 Conflict` and rolls back the schema change.

### Application to existing records

When a schema change is accepted, Clio reconciles stored record data in the same
transaction: removed fields are dropped, newly added fields receive their
default (or null), and non-null values are re-validated against the new
constraints. A change that would make an existing record invalid fails and is
not applied. Clio must not silently discard existing data.

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

## 20.1 API path parameter rules

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

## 22.1 Query parameter encoding

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
decimal_format
```

Because filters are namespaced under `filter.`, a field named `limit` or `sort`
remains reachable as `filter.limit` / `filter.sort`; reserved and field names do
not collide. The `filter.` prefix is therefore mandatory and the bare
`?field=value` form shown in early examples is not part of this contract.

Query parameters are validated: unknown filter fields, unsupported operators,
out-of-range `limit`/`offset`, malformed `from`/`to`, and value/type mismatches
all return `422 Unprocessable Entity`.

## 22.2 Query execution and resource behavior

The query implementation must not load a complete table into application
memory to filter, sort, page, group or aggregate it. Push those operations into
SQLite wherever practical, and materialize only the selected page or aggregate
results needed for the response. Avoid application-side caches and
table-sized intermediate datasets. Transient application memory should scale
with the query and response size, not with the total number of records. SQLite
may use its normal query planner and temporary storage to execute operations.

These requirements do not define a runtime memory ceiling or a fixed resource
threshold. The reference engineering budget in section 5 is a benchmark target
for representative personal workloads only; the same query behavior must remain
correct across different deployment resource assignments.

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

## Defaults and limits

The following defaults and bounds are normative:

| Query | Default `limit` | Maximum `limit` |
| --- | --- | --- |
| record list (`GET .../records`) | 100 | 1000 |
| `distinct` | 100 | 1000 |
| `group_by` | 100 | 1000 |
| `bucket` | 1000 | 1000 |

`offset` defaults to `0` and must be non-negative. A `limit` outside `1..max` or a
negative `offset` returns `422 Unprocessable Entity`. The response `page.limit`
echoes the effective limit, so clients must read it rather than assume their
requested value was granted.

## Deterministic ordering

Paged results are deterministic and stable across requests:

- A table with a principal temporal field (a `timeseries` table's
  `timestamp_field`, or a field with `role: "timestamp"`) is ordered by that
  field descending, then by record ID descending, unless the request supplies an
  explicit `sort`.
- Other tables are ordered by `created_at` descending, then by a stable
  insertion-order tie-breaker, unless the request supplies an explicit `sort`.

An explicit `sort` places non-null values first in the requested direction and
then applies a stable insertion-order tie-breaker so that equal values page
deterministically (section 24.1).

The internal pagination strategy remains an implementation detail; only the
ordering and the response shape are contractual.

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

- v1 supports a **single** sort field. Multiple sort fields are out of scope.
- `order` is optional and defaults to `asc`; it must be `asc` or `desc`.
- `order` is only valid together with `sort`. Supplying `order` alone is an
  error, as is `sort` with an empty value.
- The sort field must be validated against table metadata; an unknown field is an
  error.
- Null values sort last regardless of direction (section 24.1).

Fields must be validated against table metadata.

## 24.1 Null ordering

When sorting:

* non-null values sort before null values
* null values always sort last
* this remains true for both ascending and descending order

This behaviour is fixed in v1.

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

Filters use the `filter.{field}[.{operator}]` parameter form defined in
section 25.1. Omitting the operator means `eq`.

Examples:

```text
?filter.type=service
?filter.cost.gte=100
?filter.date.gte=2026-01-01
?filter.name.contains=Toyota
```

## 25.1 Filtering syntax

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

Operator semantics — which operators match null values, the case-insensitive
substring behaviour of `contains`, the `true`/`false` requirement of `isnull`,
and per-type operator validation — are defined in section 25.

## 25.2 Operator semantics

- `eq` (`=`) and `gt`/`gte` (`>`, `>=`) do not match null values.
- `ne` (`<>`), `lt` (`<`) and `lte` (`<=`) **match null values**: a record whose
  field is null satisfies `ne`, `lt` and `lte`. This matches the intuitive
  reading that a null value is "not equal to" and "not greater than or equal to"
  a value, and it is fixed in v1.
- `contains` is a case-insensitive substring test over the field's text form. It
  is valid only for `string`, `text` and `url` fields and does not interpret SQL
  `LIKE` wildcards (`%` and `_` are literal characters).
- `in` matches any of the supplied values (OR within the field); it does not match
  null unless null is supplied, which URL query encoding cannot express.
- `isnull` requires `true` (field is null) or `false` (field is not null); any
  other value is an error.

Operators are validated against the field type: `contains` requires a string-like
field, and the ordered operators `gt`/`gte`/`lt`/`lte` require an ordered field
(`string`, `text`, `url`, `enum`, `integer`, `decimal`, `date` or `datetime`).

All values must be parameterized SQL values.

Arbitrary SQL is prohibited.

---

# 26. Distinct

Example:

```text
?distinct=type
```

Returns the unique values for the requested field. Values are returned in
ascending order with null last (section 24.1); `null` may appear as a distinct
value. Distinct results are pageable with `limit` and `offset` using the
distinct defaults from section 23, and the response shape is defined in
section 26.1. Filters and time ranges apply before distinct selection.

## 26.1 Distinct response

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

Paging applies to distinct values (default `limit` 100, maximum 1000; section 23).

`null` may appear as a distinct value.

Values are ordered ascending, with `null` last (section 24.1).

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

`group_by` may name any field, or one of the virtual temporal groups `year`,
`month`, `day` and `week`, which are valid only for `timeseries` tables and group
by the configured timestamp field. Virtual group keys are:

| `group_by` | Key |
| --- | --- |
| `year` | integer year, e.g. `2026` |
| `month` | `YYYY-MM`, e.g. `2026-09` |
| `day` | `YYYY-MM-DD`, e.g. `2026-09-27` |
| `week` | the Monday of the UTC week, `YYYY-MM-DD` |

If `aggregate` is omitted, grouping defaults to `count`. Grouped results are
ordered ascending by group key and are pageable using the grouping defaults from
section 23. The response shape is defined in section 28.2.

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

Aggregate result keys use `{field}_{function}` for field aggregates, or `count`
for the fieldless `count` (section 28.1). `aggregate` may be combined with
filters and time ranges; when combined with `bucket`, `aggregate` is required.

Numeric results are exact where possible: integer `sum` is a JSON integer, and
decimal values and decimal aggregate results are JSON strings in canonical
decimal notation. Decimal `avg` is reported to at most ten decimal places
(section 14.1).

Grouped response example (decimal results are strings; see section 28.2):

```json
{
  "groups": [
    {
      "type": "service",
      "count": 12,
      "cost_sum": "1840.50"
    },
    {
      "type": "repair",
      "count": 7,
      "cost_sum": "932.10"
    }
  ]
}
```

## 28.1 Ungrouped aggregate response

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

## 28.2 Grouped aggregate response

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

Grouped results are pageable (default `limit` 100, maximum 1000; section 23) and
are ordered ascending by group key. When `aggregate` is omitted, only `count` is
returned (section 27).

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
?from=2026-09-01T00:00:00Z
&to=2026-10-01T00:00:00Z
&bucket=day
&aggregate=temperature:avg,temperature:min,temperature:max
```

This should produce data directly useful for reports and charts.

Rules:

- `from` and `to` are RFC 3339 timestamps, are compared in UTC, and define the
  half-open interval `[from, to)` (sections 29.1 and 29.2). Date-only values
  are not accepted.
- `to` must not be earlier than `from`.
- `from`, `to` and `bucket` are valid only for `timeseries` tables. A `record`
  table with a `role: "timestamp"` field uses that field for default ordering but
  does not support time-range or bucket queries.
- `bucket` requires `aggregate`; an aggregate is not optional when bucketing.
- Buckets use UTC and start on hour boundaries, `00:00` UTC, Monday `00:00` UTC,
  and the first day of the month at `00:00` UTC respectively (section 29.3).
- Only buckets containing at least one record are returned; empty buckets are
  not synthesized.

Only fixed simple buckets are required.

No support for:

* rolling windows
* arbitrary time expressions
* window functions
* automatic downsampling
* retention policies
* continuous aggregates

## 29.1 Time-series time semantics

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

## 29.2 Time-series range semantics

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

## 29.3 Time-series bucket semantics

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

`bucket_start` is an RFC 3339 UTC timestamp at the bucket boundary. Bucketed
results are ordered ascending by `bucket_start` and are pageable (default
`limit` 1000, maximum 1000; section 23). `aggregate` is required when `bucket` is
supplied.

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

# 32. Human-facing URL namespaces

## 32.1 Content tree

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

## 32.2 Tables

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

## 32.3 Reserved root paths

The following root paths are reserved by Clio:

```text
/api
/health
/help
/assets
/t
/collections
/files
/f
/dav
/favicon.svg
```

User-created content directories/pages may not use those exact root names.

The root content tree therefore cannot collide with Clio's API, operational
endpoints, assets, table namespace, the data browser or the favicon.

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
attachment   → file link/picker control
```

Server-side validation is authoritative.

Client-side validation is optional.

---

# 35. Collection Data Browser

This section consolidates the former `SPEC-Addendum-DataBrowsing.md`, which is
retained only as a historical reference, and is normative for the built-in
browser interface. It is an inspection and navigation view, not a spreadsheet
editor; it does not create, edit, or delete records.

## 35.1 Routes and layout

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

## 35.2 Client-side rendering

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

## 35.3 Boundaries and verification

The browser is for inspection and navigation only. Record creation and editing
remain in the existing `/t/` forms or API. Collection paths are reserved from
published content so they cannot shadow the browser routes. The content
filesystem has a parallel explorer at `/files` (section 64.14).

Tests cover browser route handling and methods, loading ClioJS, group/table
navigation, metadata-ordered visible columns, safe text rendering, URL/page
state, empty collections/tables, and API error display. No external service or
frontend framework is required.

---

# 36. Content tree

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

Pages and files are content entries with stable identifiers; identity, the
files API, and stable file URLs are defined in section 64.

## 36.1 Content-path rules

Content paths are UTF-8 paths consisting of path segments.

Rules:

* Paths are valid UTF-8.
* `/` is the root and the only path without a leading segment.
* Every non-root path begins with `/`.
* Empty path segments are rejected.
* A trailing `/` is not canonical and is rejected.
* `.` and `..` segments are rejected.
* Backslash is rejected.
* Control characters are rejected.
* Path traversal is always rejected.
* The first segment may not be a reserved root name (section 32.3).
* Paths are case-sensitive.
* Paths are URL-encoded when transported in URLs.
* Canonical URLs use normal percent-encoding.
* A directory and page/file may not occupy the same exact canonical path.
* A regular file may not occupy an intermediate segment of a deeper path; such a
  request returns `409 Conflict`.
* Symbolic links are not permitted in content paths.
* Paths are not silently renamed because of case changes or title changes.

A page path includes its extension when supplied.

Examples:

```text
/reports/weekly.md
/reports/summary.html
```

The extension is therefore part of the canonical content URL.

Clio must not silently remove or add `.md` or `.html`.

## 36.2 Content resource identity

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

# 37. Directory URLs

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

# 38. Directory API

The machine-readable directory API uses the `path` query parameter for nested paths:

```text
GET    /api/v1/directories
GET    /api/v1/directories?path=/...
POST   /api/v1/directories
DELETE /api/v1/directories?path=/...
```

The directory API is a compatibility facade over the filesystem REST API
(section 64.4).

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

## 38.1 Directory API path handling

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

# 39. Creating directories

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

# 40. Pages

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

# 41. Page API

Pages are entries in the content filesystem (section 64). The Page API remains
available and behaves as before; it is a compatibility facade over the same
entries used by the files API (section 64.4), so a page created through either
interface has the same path and stable ID.

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

## Page write rules

`POST /api/v1/pages` is a create-or-replace (upsert) operation: it returns `201
Created` when the page did not exist and `200 OK` when an existing page was
replaced. This is the explicit overwrite operation referred to by section 36.2.
Replacing a directory, file or other incompatible resource at the path returns
`409 Conflict`.

- `path` is required and obeys the canonical content-path rules (section 36.1).
- The path must end in `.md` or `.html`; any other extension is rejected.
- `content_type` is required and must match the extension: `text/markdown` for
  `.md`, `text/html` for `.html`.
- `content` is required and is stored verbatim. Markdown source is stored as
  Markdown; HTML is stored as HTML.
- Writes are atomic: content is written to a temporary file and atomically
  renamed, so a failed write does not corrupt an existing page.
- `GET /api/v1/pages?path=...` returns the stored source (section 41.1);
  `DELETE /api/v1/pages?path=...` removes the page.

## 41.1 Page read API

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

# 42. Client-side Markdown rendering

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

## 42.1 Client-side renderer contract

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

The asset exposes a single global function:

```js
ClioMarkdown.render(source) // returns an HTML string
```

The name is part of the contract so that pages and tests are stable. The
ClioJS client also exposes the same capability as `Clio.Markdown.render(source)`
when the renderer asset is loaded (section 43.3). The function must be simple
enough that a complete example fits in a small HTML page, and it must apply the
safety rules in section 55.

---

# 43. ClioJS browser client

Clio serves an optional, lightweight browser JavaScript client over the existing
Clio v1 REST API. This section consolidates the former
`SPEC-Addendum-ClioJS.md`, which is retained only as a historical reference.
ClioJS adds no API endpoints or semantics.

Use the ClioJS script itself to implement Data Browsing.

## 43.1 Distribution and scope

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

ClioJS provides two built-in UI components: `Clio.DataBrowser.mount(element)`,
the read-only collection browser in section 35, and
`Clio.FileBrowser.mount(element)`, the content filesystem explorer in
section 64.14. Neither establishes a general application framework or state
store.

## 43.2 Client operations

`new Clio()` targets the origin from which the script is served and uses
same-origin browser credentials. An explicit `baseUrl` may be provided. The
client exposes convenient access to metadata and groups, and a table handle
obtained with `clio.table(group, table)`. Table handles support metadata,
record listing/querying, get/create/update/delete, and the lightweight
`list`, `first`, `count`, `distinct`, and `aggregate` conveniences. The client
also supports creating groups/tables, accessing page source and directories,
listing and mutating content entries, search, and the file explorer.

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

## 43.3 Data and Markdown behavior

The client preserves JSON types returned by Clio. In particular, decimal field
values remain strings and are never converted to JavaScript floating-point
numbers; dates and datetimes remain strings as well. Record PATCH requests pass
omitted and explicit-null properties through unchanged so the API retains its
defined partial-update semantics.

`clio.page(path)` returns the original page source and metadata from the page
API. ClioJS integrates with the separately served `/assets/clio-markdown.js`
renderer through `Clio.Markdown.render(source)` when that renderer is loaded.
Markdown safety requirements remain those in sections 44 and 42.1.

## 43.4 Help and verification

`/help` documents both asset URLs, same-origin usage, a short example, and the
available client capabilities so browser users and generated pages can discover
ClioJS without a separate package. Tests cover its static routes, metadata,
table and record operations, query encoding, paging/async iteration, errors,
decimal preservation, page/directory access, content files and search, the
file explorer, cancellation where implemented, and Markdown integration. The
asset must work in an ordinary browser page.

---

# 44. Directory-tree upload

Clio must support publishing an entire directory tree and its contents in a single
request. The endpoint is `POST /api/v1/directories` with
`Content-Type: application/zip` and a destination supplied by the `path` query
parameter (omitted means the root).

## 44.1 Upload semantics

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

The successful response is JSON containing the destination path and the
resulting absolute URLs, for example:

```json
{
  "path": "/reports",
  "urls": [
    "https://clio.example.com/reports/pool/weekly.md"
  ]
}
```

Entries that are absolute, use a drive-letter path, contain `..`, duplicate an
earlier entry, or place a file where another entry requires a directory are
rejected before publication. Applying an archive that would replace an existing
resource is also rejected with `409 Conflict` unless `overwrite=true`
(section 44.3).

## 44.2 Directory-tree upload limits

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

## 44.3 Directory-tree upload overwrite behaviour

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

The upload operation **must** validate the entire archive before modifying the
published tree and **must** stage the archive before committing the resulting
tree, so that validation errors do not leave a partially published archive. On
failure the published tree must be left as it was. A successful upload returns
`201 Created` with the response body described in section 44.1.

---

# 45. Stable URLs

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

# 46. Help

Clio must expose:

```text
/help
/api/v1/help
```

`/help` returns an easy-to-read HTML page.

`/api/v1/help` returns concise machine-oriented Markdown (`text/markdown;
charset=utf-8`). It is the format clients and LLMs should consume; `/help` is the
human rendering of the same content.

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
* query defaults and limits
* time-series querying
* directories
* directory API
* Markdown
* HTML
* client-side Markdown rendering
* directory-tree uploads
* files and stable file IDs
* full-text search
* attachments
* WebDAV
* file-system browsing at `/files`
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

# 47. Health

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
  "directories": 11,
  "files": 57
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
* Number of files?

`database.status` is `ok` only when a SQLite integrity check (`PRAGMA
quick_check`) returns `ok`; otherwise it is `error` and the top-level `status`
becomes `error`. The resource counts describe the whole instance. `version`
reports the product version; release builds inject the exact version at build
time, while the document header records the version this specification was
written against (currently `1.0.0`).

---

# 48. API versioning

The stable external API begins at:

```text
/api/v1/
```

Incompatible API changes must use a future version such as `/api/v2/`.

Existing `/api/v1/` semantics must not silently change.

---

# 49. Complete API v1 summary

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

GET    /api/v1/files
GET    /api/v1/files?path=/...
GET    /api/v1/files/{id}
GET    /api/v1/files/{id}/content
PUT    /api/v1/files?path=/...
POST   /api/v1/files/directories
DELETE /api/v1/files?path=/...
DELETE /api/v1/files/{id}
POST   /api/v1/files/move
POST   /api/v1/files/copy
POST   /api/v1/files/rescan

GET    /api/v1/search?q=...

GET    /api/v1/files/extraction?path=/...
PUT    /api/v1/files/extraction?path=/...
DELETE /api/v1/files/extraction?path=/...
```

Client-side Markdown rendering is exposed as a static browser asset rather than a separate API service.

The optional ClioJS browser client is served at `/assets/clio.js` and `/assets/clio/v1/clio.js`; its contract is specified in section 43. The content filesystem explorer is served at `/files` and uses ClioJS (section 64.14); the collection data browser remains at `/collections` (section 35).

---

# 50. Error handling

HTTP status codes used by the API:

| Status | Meaning |
| --- | --- |
| `200 OK` | Request succeeded; content or representation returned. |
| `201 Created` | Resource created. |
| `204 No Content` | Resource deleted; no body. |
| `400 Bad Request` | Malformed request, e.g. malformed query parameters. |
| `401 Unauthorized` | Authentication required or invalid (section 54). |
| `403 Forbidden` | Transport requirement not met (section 54.3). |
| `404 Not Found` | Resource or endpoint does not exist. |
| `405 Method Not Allowed` | Method not supported for the route. |
| `409 Conflict` | Request conflicts with existing state (duplicate name, referenced record, overwrite refused, unsafe schema change). |
| `413 Payload Too Large` | Request body exceeds the configured limit. |
| `422 Unprocessable Entity` | Well-formed request that fails validation. |
| `500 Internal Server Error` | Unexpected server error. |

Internal exception details must not be exposed through API responses; the body
of a `500` is always the generic `internal_error` shape below.

JSON error format:

```json
{
  "error": "validation_error",
  "message": "cost is required"
}
```

The `error` member is a stable machine-readable code. The defined codes are:

```text
validation_error   (422)
not_found          (404)
conflict           (409)
method_not_allowed (405)
body_too_large     (413)
bad_request        (400)
internal_error     (500)
```

Error responses must be consistent, and `message` is human-readable and not part
of the compatibility contract.

---

# 51. Persistence

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

# 52. SQLite indexing

Indexing is an implementation concern. Clio shall not index every field. It
automatically maintains indexes for record identity, group/table access,
deterministic default ordering (`created_at`), fields declared `unique`,
reference fields, and the principal temporal field (and the `timeseries` timestamp
access path). SQLite enforces uniqueness rather than relying on application
checks.

Applications may declare additional single-field or compound indexes using the
table `indexes` property. Declared indexes are **non-unique**; uniqueness is
declared on the field itself with `unique: true` (section 13.2). Compound field
order is significant. Index declarations must be validated against table
metadata and reconciled transactionally when the table schema changes; stale or
duplicate indexes must be removed. Enabling a unique field constraint that
conflicts with existing data fails with `409 Conflict` without partially applying
the schema change.

Time-series timestamp access uses an index scoped by table and timestamp. Default
record ordering is defined in section 23. Offset/limit pagination remains
sufficient for v1. Query operations continue to execute in SQLite and must not
load whole tables into application memory.

Index configuration is optional for ordinary use, invisible in the primary UI, and requires no index-administration endpoint. Clio provides a bounded full-text index over extracted content using SQLite's full-text capability (section 64.6); it does not introduce an external search engine or a separate search database. Spatial indexes, index hints, workload-driven index creation, and query-plan analysis remain out of scope.

---

# 53. Security

v1 may run in a trusted environment or use the optional single-user Basic Authentication described in section 54. Deployments may also provide authentication/access control at a reverse proxy.

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

A full identity and authorization system is out of scope. Optional single-user Basic Authentication is defined in section 54.

---

# 54. Optional authentication and transport security

Clio supports an optional, intentionally small single-user access gate. This
section consolidates the former `SPEC-Addendum-Auth.md`, which is retained only
as a historical reference. Authentication remains disabled by default, and
enabling it does not change the API or data model.

## 54.1 Authentication model

The only built-in authentication scheme is HTTP Basic Authentication with one
configured username and password hash. There are no user tables, roles,
sessions, authorization rules, registration, password reset, OAuth, OIDC, JWT,
or MFA. The configured identity has access to the whole Clio instance.

When enabled, authentication applies to every HTTP route, including health,
help, published content, directories, table views, API endpoints, and static
assets. Health is not an unauthenticated exception.

## 54.2 Configuration

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

## 54.3 HTTPS policy

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

## 54.4 Authentication responses and logging

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

## 54.5 Authentication and transport tests

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

# 55. HTML and Markdown safety

Markdown and HTML are first-class page types.

The implementation must distinguish intentionally published HTML from user-entered text.

User-controlled text rendered into Clio-generated pages must be escaped appropriately.

Markdown output must be sanitised appropriately before being inserted into a page where necessary.

The client-side Markdown renderer must apply equivalent safety expectations.

## Markdown subset

The server-side and client-side renderers support the same deliberately small
subset:

- ATX headings `#` through `######`.
- Unordered lists using `- ` or `* `.
- Fenced code blocks delimited by ```` ``` ````.
- Inline code delimited by backticks.
- `**bold**` and `*italic*`.
- Links of the form `[text](https://...)`, limited to `http` and `https` URLs.

All other text is treated literally and escaped. Raw HTML in Markdown is never
interpreted.

## Sanitisation rules

Both renderers escape all text before emitting markup, and emit only the fixed
set of safe tags above (`h1`–`h6`, `ul`, `li`, `p`, `pre`, `code`, `strong`,
`em`, `a`). No attributes are accepted from source text, and only `http`/`https`
link targets are emitted. As a result, a Markdown page cannot introduce
executable markup or event handlers through its content. The client-side
renderer must satisfy the same rules (sections 44 and 42.1).

Clio must not accidentally turn arbitrary record values into executable HTML/JavaScript.

Security behaviour must be documented.

## 55.1 HTML trust boundary

HTML pages are considered **trusted published content**.

Clio stores and serves HTML page content without sanitizing or rewriting it.

This means an HTML page may contain executable JavaScript.

Therefore:

* Clio must be deployed where publishers are trusted.
* The write API must be protected by the surrounding deployment if untrusted users can access Clio.
* Clio does not attempt to turn arbitrary user-submitted HTML into a safe multi-user content platform.
* Authorization beyond the single configured identity in section 54 remains an external deployment concern in v1.

Markdown and Clio-generated HTML views must still escape/sanitize untrusted data appropriately.

Non-page content is served as a download with `Content-Disposition: attachment`
and `X-Content-Type-Options: nosniff`, including through `/f/{id}` (section
64.5). Because WebDAV allows a publisher to write `.html` into the content tree,
the trusted-publisher boundary applies to WebDAV as well (section 64.10).

This boundary must be clearly documented in the README and `/help`.

---

# 56. Observability

v1 requires only basic logging.

Log at least:

* startup
* shutdown
* configuration failures
* HTTP failures
* database errors
* content publication failures
* extraction failures
* archive upload failures
* WebDAV failures

No external metrics system is required.

No distributed tracing is required.

`/health` is the primary built-in operational diagnostic.

---

# 57. Backup and restore

The entire Clio data directory must be straightforward to back up.

Documentation must describe:

* backup
* restore
* database integrity expectations

A consistent backup captures the SQLite database, its WAL sidecar files
(`-wal`/`-shm`) and the published content directory together. The database is
mandatory: it holds durable state that the filesystem cannot reproduce —
content-entry IDs, entry timestamps, and agent-supplied extracted text
(section 64.11). The documented procedure may stop the service before copying,
which is always correct. Clio
need not implement an online backup API in v1; operators who require a hot copy
may use SQLite's own `VACUUM INTO` or `.backup` tooling and then copy the content
tree.

Restoring replaces the complete database (with its WAL sidecars as captured) and
the content directory, then starts Clio with the same configuration. Recovery is
confirmed through `/health`, whose `database.status` runs a SQLite integrity
check (section 47).

No external backup system is part of Clio.

---

# 58. Testing

The implementation must include automated tests. The mapping from specification
areas to concrete tests, plus the verification commands, is maintained in
[`SPEC-CONFORMANCE.md`](SPEC-CONFORMANCE.md).

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
* content-entry identity and stable IDs across rename and move
* the files API, `/f/{id}` serving, and download headers
* text extraction, search, and agent enrichment
* `attachment` field validation and delete integrity
* WebDAV methods and the opt-in flag
* the `/files` explorer and ClioJS file browser

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

## Additional acceptance requirements

Section 56 defines the end-to-end acceptance walkthrough and section 58 defines
the automated test coverage. The following requirements are part of the same
acceptance suite and do not replace either of those sections.

### Routing

Verify that all of the following can coexist:

```text
/pool/measurements
/t/pool/measurements
```

where the first is a content resource and the second is a table.

No routing ambiguity exists.

### Markdown API

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

### Directory upload

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

### Record semantics

Verify:

* omitted field uses default or becomes null
* required omitted field fails
* explicit null clears nullable field
* required field cannot be null
* omitted PATCH fields remain unchanged
* unknown fields are rejected
* readonly fields are rejected
* decimals round-trip without floating-point conversion

### Query semantics

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

# 59. End-to-end acceptance test

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

# 60. Repository deliverables

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

# 61. Repository constraints

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

# 62. Definition of done

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
* files are first-class with stable IDs
* text extraction and search work
* `attachment` fields link records to files
* WebDAV can be enabled
* the content filesystem is browsable at `/files`
* the implementation remains small and understandable

## 62.1 v1 scope

All capabilities described in this specification are required v1 capabilities.

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

# 63. Final product boundary

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

---

# 64. Content filesystem, attachments, search, and WebDAV

## 64.1 Framing and scope

The content tree (section 36) is a filesystem. Its entries are directories, pages
and files. Pages are files with rendering semantics. Every page or file is a
**content entry** with a stable identifier. This section is additive: it does
not change the semantics of sections 1–63 except where it explicitly says so
(section 64.2 replaces the restriction on renaming or moving published content
in section 3.1).

## 64.2 Content entries and identity

A content entry is a page or a file and has:

```text
id
path
kind
content_type
size
sha256 (optional)
created_at
updated_at
```

- `id` is opaque, generated by Clio, globally unique across the instance,
  immutable, and stable across rename and move. Its concrete format is an
  implementation detail; clients must treat it as an opaque string and must not
  parse, sort, or assume a length.
- `kind` is `page` or `file`. A path whose final segment ends in `.md` or
  `.html` is a `page`; every other regular file is a `file`. The extension
  determines the kind; Clio must not silently add or remove extensions.
- `path` is the canonical content path (section 36.1). It is mutable through an
  explicit rename or move.
- `content_type` is the media type served for the entry. For pages it is
  determined by the extension (`text/markdown`, `text/html`); for files it is
  inferred from the extension or the upload's declared type.
- `sha256`, when present, is the lowercase hexadecimal SHA-256 of the stored
  bytes. It may be absent for entries discovered by reconciliation.
- `created_at` and `updated_at` are system-managed RFC 3339 UTC timestamps.
- Directories do not have content entries; they are structural and addressed by
  path. Moving a directory updates the `path` of every descendant entry and
  preserves each descendant's `id`.
- The set of content entries is durable, authoritative state: IDs cannot be
  derived from the filesystem, so it must be preserved by backup (section 57).

Reconciliation: at startup and on rescan (section 64.4), Clio reconciles entries
with the filesystem. An existing path keeps its ID; a new path receives a new
ID; a row whose path no longer exists is removed. A file renamed directly on the
filesystem (outside Clio) is observed as a removal and an addition and therefore
receives a new ID; supported rename and move are performed through the files API
(section 64.4) or WebDAV (section 64.10). Symbolic links are never followed.

## 64.3 Stable file URLs

Every content entry has two URLs:

- its canonical path URL, for example `https://clio.example.com/bills/2026-03.pdf`;
- its stable ID URL, `/f/{id}`.

`/f/{id}` serves or redirects to the entry's current bytes regardless of its
current path; an optional cosmetic name segment, `/f/{id}/{name}`, is accepted
and ignored for resolution. The path URL changes when the entry is moved; the ID
URL does not. `/f` is a reserved root (section 32.3). `/f` responses use the
serving rules of section 64.5.

Pages are rendered at their path URL (section 41); `/f/{id}` serves the raw
stored bytes for any entry, including a page.

## 64.4 Filesystem REST API

The content filesystem is exposed as a REST API under `/api/v1/files`. It
addresses filesystem nodes — directories, pages and files — and unifies the
operations that the directory API (section 38) and page API (section 41) expose
as compatibility facades. In this API, "files" names the filesystem resource;
directories are nodes of it.

```text
GET    /api/v1/files                          # list entries (flat catalog)
GET    /api/v1/files?path=/...                # node metadata; directory listing
GET    /api/v1/files/{id}                     # file/page entry by id
GET    /api/v1/files/{id}/content             # download bytes (HEAD, ranges)
PUT    /api/v1/files?path=/...                # create/replace a file (raw bytes)
POST   /api/v1/files/directories              # create a directory
DELETE /api/v1/files?path=/...                # delete a file, page or directory
DELETE /api/v1/files/{id}                     # delete a file/page entry by id
POST   /api/v1/files/move                     # rename/move a node
POST   /api/v1/files/copy                     # copy a node
POST   /api/v1/files/rescan                   # reconcile catalog with disk
```

Conventions and behavior:

- Content paths are carried in the `path` query parameter (section 20.1); IDs
  are path parameters. Paths obey section 36.1.
- `GET /api/v1/files?path=...` returns the node at that path. For a directory it
  returns the directory and its children (paged); for a page or file it returns
  the content-entry representation. A missing path returns `404 Not Found`.
- `GET /api/v1/files` (no `path`) lists content entries across the whole
  filesystem. It accepts `limit` and `offset` (section 23 defaults) and optional
  `prefix`, `content_type` and `kind` filters.
- `PUT /api/v1/files?path=...` creates or replaces a file. The request body is
  the raw bytes; the media type is taken from the `Content-Type` header,
  defaulting to `application/octet-stream`. It returns `201 Created` for a new
  path and `200 OK` when an existing file is replaced. Replacing preserves the
  ID. A path that names a directory returns `409 Conflict`. Writes are atomic
  (section 41) and subject to the limits of section 64.12.
- `POST /api/v1/files/directories` accepts `{"path": "/bills/archive"}` and
  creates a directory, returning `201 Created`. Creating over an existing node
  returns `409 Conflict`, and the root cannot be created. This is the same
  operation as the directory API (section 38), which remains available.
- `DELETE /api/v1/files?path=...` deletes the node at that path; for a directory
  it deletes the subtree. It returns `204 No Content`. Deleting the root, or
  deleting a page/file (or a directory containing one) referenced by an
  `attachment` field, returns `409 Conflict` (section 64.9). `DELETE
  /api/v1/files/{id}` deletes a file or page by ID.
- `POST /api/v1/files/move` accepts `{"from": "/old", "to": "/new"}` and
  renames or moves a file, page or directory. IDs are preserved, including for
  every descendant of a moved directory. The operation is atomic; a conflicting
  destination returns `409 Conflict`, an invalid path `422 Unprocessable
  Entity`, and a missing source `404 Not Found`.
- `POST /api/v1/files/copy` accepts `{"from": "/old", "to": "/new"}` and copies
  a file, page or directory. Copies receive new IDs; enrichment (section 64.8)
  is not copied. Status codes match `move`.
- `POST /api/v1/files/rescan` reconciles the catalog with the filesystem and
  returns a summary of added, removed and refreshed entries.

A content-entry representation is:

```json
{
  "id": "9f2c4e8a1b3d4f5061728394a5b6c7d8",
  "path": "/bills/2026-03.pdf",
  "kind": "file",
  "content_type": "application/pdf",
  "size": 184320,
  "sha256": "9f2c...",
  "created_at": "2026-09-27T12:00:00Z",
  "updated_at": "2026-09-27T12:00:00Z",
  "url": "https://clio.example.com/bills/2026-03.pdf",
  "stable_url": "https://clio.example.com/f/9f2c4e8a1b3d4f5061728394a5b6c7d8",
  "indexed": true
}
```

A directory representation is:

```json
{
  "path": "/bills",
  "kind": "directory",
  "url": "https://clio.example.com/bills",
  "children": [
    {
      "path": "/bills/2026-03.pdf",
      "kind": "file",
      "id": "9f2c4e8a1b3d4f5061728394a5b6c7d8",
      "content_type": "application/pdf",
      "size": 184320,
      "updated_at": "2026-09-27T12:00:00Z",
      "url": "https://clio.example.com/bills/2026-03.pdf",
      "stable_url": "https://clio.example.com/f/9f2c4e8a1b3d4f5061728394a5b6c7d8"
    },
    {
      "path": "/bills/archive",
      "kind": "directory",
      "url": "https://clio.example.com/bills/archive"
    }
  ],
  "page": { "limit": 100, "offset": 0, "count": 2, "total": 2 }
}
```

Directories are structural and do not have IDs (section 64.2); they are still
nodes of the REST API. File and page entries are addressed by path or ID.
`GET /api/v1/files/{id}/content` serves the raw bytes with the disposition and
headers of section 64.5, supports `HEAD` and byte ranges, and returns
`404 Not Found` for a missing ID.

The directory API (section 38) and page API (section 41) remain available and
are compatibility facades over the same tree; they produce and address the same
nodes and IDs.

## 64.5 File serving

Non-page content is served with:

```text
Content-Disposition: attachment
X-Content-Type-Options: nosniff
```

This applies to `/f/{id}` for every entry and to direct path URLs for files. HTML
pages continue to be served as trusted executable content at their path URL
(section 55.1). A file download must not execute active content in Clio's
origin. GET responses support byte ranges.

## 64.6 Extraction and the text index

Clio maintains a full-text index of extracted text over content entries.

- Text-like content (for example `text/*`, `.md`, `.txt`, `.csv`, `.json`,
  `.html`) is indexed natively. HTML is indexed as text with markup removed.
- PDF text is extracted natively when a text layer is present. Image-only or
  scanned content yields no native text; OCR is out of scope for the executable
  and is handled by sidecar agents (section 64.8).
- Extracted text is capped per entry (default 1 MiB) and never replaces the
  stored bytes.
- The index is an implementation detail built on SQLite. It must not introduce
  an external service or a separate database, and it is a SQLite index rather
  than an application cache (section 4.5).
- The metadata catalog and the native text index are derived and can be rebuilt
  by rescan. Agent-supplied text (section 64.8) is not reproducible and is
  preserved by backup (section 57).

## 64.7 Search API

```text
GET /api/v1/search?q=<terms>
```

Search covers pages and files in one result set, is paged (`limit`, `offset`;
section 23), and orders results by relevance. A result is:

```json
{
  "id": "9f2c...",
  "path": "/bills/2026-03.pdf",
  "kind": "file",
  "content_type": "application/pdf",
  "source": "native",
  "snippet": "...amount due \u27e6184.20\u27e7 on 2026-03-15...",
  "score": 1.42
}
```

- `q` is a set of whitespace-separated terms. Clio treats each term as a
  literal and combines terms with AND; it does not expose a raw search-engine
  query syntax. An empty or oversized query returns `422 Unprocessable Entity`.
- `snippet` contains matched terms delimited by private sentinel characters and
  is otherwise plain text. It is untrusted and must be escaped before markup is
  applied; the built-in UI escapes first, then substitutes its own highlight
  element.
- `source` identifies the text's provenance: `native` or an agent identifier
  (section 64.8).

## 64.8 Agent enrichment API

Sidecar agents may contribute extracted text (for example OCR or deeper PDF
parsing) so that it becomes searchable:

```text
GET    /api/v1/files/extraction?path=/...
PUT    /api/v1/files/extraction?path=/...
DELETE /api/v1/files/extraction?path=/...
```

`PUT` accepts:

```json
{
  "fingerprint": "184320:2026-09-27T12:00:00Z",
  "text": "Invoice 4421 ... total due 184.20",
  "provider": "ocr:tesseract",
  "title": "Invoice 4421"
}
```

- The enclosing entry may be given by `path` or by file ID.
- `fingerprint` must match the current entry fingerprint (size and modified
  time, or `sha256` when known); a mismatch returns `409 Conflict`. This
  prevents a stale extraction from shadowing changed content.
- Enrichment is keyed by entry ID, so it survives rename and move.
- `DELETE` removes the agent-provided text; the entry then falls back to any
  native text.

## 64.9 Attachment fields

`attachment` is a field type (section 13). A field definition may declare:

```json
{"name": "invoice", "type": "attachment", "required": false}
```

An optional `accept` property may list accepted extensions or media types as a UI
hint; it does not replace validation.

- The stored value is a content-entry ID (section 64.2), returned in the record
  representation as a JSON string, analogous to a `reference` field returning a
  target record ID.
- Clio validates on create, update and default application that the ID names an
  existing content entry; otherwise the request fails with
  `422 Unprocessable Entity`.
- Filtering, sorting and `distinct` operate on the ID string.
- A record that references an entry prevents deletion of that entry (or of a
  directory containing it) with `409 Conflict`, until the referencing values are
  cleared. There is no cascade deletion. Replacing the bytes at the same path is
  permitted and keeps the ID.
- Clients resolve an attachment to a URL through the files API or by linking to
  `/f/{id}`.

## 64.10 WebDAV

Clio may expose the content filesystem over WebDAV (RFC 4918) at `/dav`, mapping
`/dav/<path>` to content `<path>`. `/dav` is a reserved root (section 32.3).

- WebDAV is optional and disabled by default. It is enabled with
  `CLIO_WEBDAV_ENABLED=true`. When enabled it is subject to the transport and
  authentication policy of section 54 and is protected exactly like every other
  route.
- Supported methods include `OPTIONS`, `PROPFIND`, `PROPPATCH`, `GET`, `HEAD`,
  `PUT`, `DELETE`, `MKCOL`, `COPY`, `MOVE`, `LOCK` and `UNLOCK`. Responses use
  standard WebDAV status codes.
- All mutations flow through the same validation, indexing and timestamps as the
  files API. In particular:
  - `PUT` creates or replaces an entry, preserving the ID on replace.
  - `MOVE` renames or moves an entry or directory, preserving IDs.
  - `COPY` creates a new entry with a new ID.
  - `DELETE` enforces the same `409 Conflict` for referenced entries.
  - Writing `.md` or `.html` creates or replaces a page with page timestamps.
- Locking is provided but is not persistent across restarts.
- WebDAV exposes the entire content tree, including pages. Because a publisher
  can write `.html` through WebDAV, the trusted-publisher boundary of
  section 55.1 applies.

## 64.11 Backup and restore

Content-entry IDs, entry timestamps, and agent-supplied text are durable state
that the filesystem cannot reproduce. A consistent backup therefore captures the
SQLite database (with its `-wal`/`-shm` sidecars) and the content directory
together, as required by section 57. Restoring a backup preserves IDs, so
`/f/{id}` URLs and `attachment` values remain valid; a rescan rebuilds the
derived catalog and native text. The content directory alone is not a complete
backup.

## 64.12 Limits

```text
Maximum individual file upload: 16 MiB
Maximum extracted text per entry: 1 MiB
```

These are safety limits and may be implemented as constants. Exceeding the
upload limit returns `413 Payload Too Large`.

## 64.13 API and product versioning

The files, search and enrichment endpoints are additive under `/api/v1/`.
Adding the `attachment` field type and enabling WebDAV do not change existing
`/api/v1/` semantics. Removing the Page API (section 41) or otherwise making an
incompatible change requires a future `/api/v2/` (section 48).

## 64.14 Web-based file explorer

The content filesystem must be browsable in a browser through a dedicated
explorer at `/files`, analogous to the collection data browser (section 35).
`/files` is a reserved root (section 32.3) and cannot be shadowed by content.
This is in addition to the directory pages served at content paths
(section 37), which remain browsable.

- The explorer is a server-served shell that loads `/assets/clio.js` and uses
  the ClioJS component `Clio.FileBrowser.mount(element)` over the files and
  search APIs. It introduces no new endpoint, frontend dependency, package
  installation, or build step (section 43).
- It presents the content tree using content-entry metadata (section 64.2):
  directories, pages and files with kind, size, content type and modified time.
  Files link to `/f/{id}` for stable downloads and to their path URL; pages link
  to their rendered path URL.
- Navigation uses breadcrumbs, and the current directory is reflected in the URL
  so browser history works and locations are shareable.
- It exposes the light actions of the files API: create folder, upload, rename
  or move, and delete. Failures surface the API status and code, including
  `409 Conflict` when an entry is referenced by an attachment field
  (section 64.9).
- It provides search over `GET /api/v1/search` (section 64.7). Snippets are
  untrusted: the explorer escapes them before applying its own highlight mark.
- Entry names and record values are rendered as text, never as executable HTML.
- WebDAV (section 64.10) remains the mechanism for bulk and drag-and-drop file
  operations; the explorer is a browsing and light-administration surface.

Security note: the explorer's mutating actions are ordinary same-origin requests
protected by the authentication policy of section 54. Cross-site request forgery
protection is not part of v1; when authentication is enabled, deployments should
rely on the same-origin policy and network controls and may front Clio with a
proxy that enforces CSRF protection.

