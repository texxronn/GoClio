> **SUPERSEDED — HISTORICAL REFERENCE ONLY.**
>
> This document has been consolidated into the authoritative contract at
> [SPEC.md](../../SPEC.md). It is retained for background only and confers no
> requirements. Do not implement from this file.

# SPEC ADDENDUM — Indexing and Query Performance

## Status

Normative addition to the Clio specification.

This addendum defines how Clio manages database indexes and establishes the minimum performance contract required for collections, tables, the Data Browser, and API queries.

## 1. Design Principle

Indexing is primarily an **implementation concern**, not a user-facing database administration task.

Clio shall provide sensible indexing automatically for common access patterns. A normal user creating a collection or table should not need to understand database indexes.

Clio shall nevertheless retain an explicit mechanism for declaring additional indexes where application-specific access patterns require them.

The guiding principle is:

> **The simple case requires no index configuration, while the data model must remain capable of efficient access as datasets grow.**

---

## 2. Automatic Indexes

Clio shall automatically maintain the following indexes where applicable.

### 2.1 Primary Record Identifier

Every table shall have a primary record identifier.

The primary identifier shall be indexed through the database primary-key mechanism.

This index is mandatory.

### 2.2 Unique Fields

A field declared `unique` shall have a corresponding unique database constraint/index.

The database shall enforce uniqueness; Clio shall not rely solely on application-level checks.

### 2.3 Relationship / Reference Fields

Fields representing references to another record/table shall be indexed automatically when appropriate.

This supports efficient:

* relationship lookup
* reverse lookup
* filtering
* joins or equivalent relationship resolution

### 2.4 Temporal Fields

A field explicitly designated as the table's principal temporal field shall be indexed automatically.

A temporal field may be designated through schema metadata such as:

```yaml
recorded_at:
  type: datetime
  role: timestamp
```

The exact metadata representation is implementation-defined, but the semantic distinction shall exist.

This is intended to support common operations such as:

```text
latest records
records between two dates
ORDER BY recorded_at
recent measurements
historical measurements
paginated time-series access
```

Clio shall not require a special "time-series table" type.

A normal table containing a temporal field remains an ordinary Clio table.

---

## 3. Explicit Indexes

The schema shall permit an administrator/developer to declare additional indexes where automatic indexing is insufficient.

An explicit index may be defined over:

* a single field
* multiple fields, in a defined order

For example:

```yaml
indexes:
  - fields: [vehicle_id, serviced_at]
  - fields: [category, recorded_at]
```

The exact schema syntax may vary by implementation.

Clio shall create and maintain the corresponding database indexes.

Explicit indexes are an advanced capability and are not required for normal collection/table creation.

---

## 4. Index Selection

Clio shall **not** automatically index every field.

Unnecessary indexes increase storage consumption and can increase the cost of inserts and updates.

Automatic indexing shall therefore be limited to fields and access patterns for which there is a clear semantic reason.

At minimum, automatic indexing shall cover:

```text
primary identity
unique identity
relationships/references
principal temporal access
```

Additional automatic indexes may be introduced by the implementation where they are demonstrably useful and do not materially complicate the data model.

---

## 5. Browser Query Requirements

The Data Browser defined by the Collection Data Browser specification shall operate through ordinary indexed database queries where practical.

For a paginated table view, the implementation shall provide a deterministic ordering.

The preferred ordering for tables with a principal temporal field is:

```text
temporal field + record identifier
```

For example:

```sql
ORDER BY recorded_at DESC, id DESC
```

The record identifier provides a deterministic tie-breaker when multiple records have the same timestamp.

For tables without a temporal field, the implementation may use the primary record identifier or another suitable deterministic ordering.

---

## 6. Pagination

The Data Browser shall support page-based pagination.

The initial implementation may use conventional offset/limit pagination where appropriate:

```text
LIMIT N OFFSET M
```

However, the underlying API and storage design shall not prevent the implementation from using cursor/keyset pagination for larger datasets.

For large or frequently accessed tables, cursor/keyset pagination is preferred when it provides a material performance benefit.

The browser contract must remain independent of the specific pagination strategy used internally.

---

## 7. Query Performance Expectations

Clio shall design ordinary collection access so that common operations remain efficient as data volumes increase.

In particular, the following should not require a full-table scan under normal conditions when the relevant metadata/index exists:

```text
lookup by record identifier
lookup by unique field
lookup through a reference field
retrieve recent records
retrieve records within a temporal range
retrieve a page of a chronologically ordered table
```

The implementation may use database-specific optimizations to satisfy these requirements.

Clio does not require a particular database engine or index implementation.

---

## 8. Index Lifecycle

Indexes are part of the table schema's persistent implementation state.

When a schema change creates, removes, or changes an indexed field or explicit index declaration, Clio shall reconcile the database indexes with the current schema.

Schema migration shall therefore account for index creation, modification, and removal.

The implementation shall avoid creating duplicate indexes that provide no additional value.

---

## 9. User Visibility

Indexes shall normally be invisible in the primary Clio user interface.

The collection/table browser does not need to expose database indexes to ordinary users.

Advanced administrative tooling may expose index metadata for diagnostics or performance troubleshooting, but such tooling is outside the scope of the initial Clio UI.

---

## 10. No Special Time-Series Subsystem

Clio shall not introduce a separate time-series database or special table type solely to support chronological measurements.

The following shall remain a normal Clio table:

```text
power_readings
-------------
id
recorded_at
value
unit
```

The presence of a designated temporal field is sufficient for Clio to apply the appropriate indexing and query behaviour.

This keeps the data model generic while supporting common personal-data use cases such as:

* measurements
* energy consumption
* vehicle readings
* financial records
* maintenance history
* health measurements
* event history

---

## 11. API Contract

The Clio API shall not require callers to know whether an operation is backed by an index.

Indexing is an internal implementation detail.

The API contract is expressed in terms of semantic operations such as:

```text
get record
list records
filter records
sort records
paginate records
```

The storage layer is responsible for selecting the appropriate database access strategy.

---

## 12. Future Extensions

The following are intentionally outside the initial scope:

* full-text indexes
* spatial/geographic indexes
* database-specific index hints
* user-managed index tuning UI
* automatic query-plan analysis
* materialized views
* dedicated analytics indexes
* automatic index creation based on observed query workloads

These may be introduced later without changing the fundamental Clio data model.

## 13. Summary

Clio shall provide **automatic indexing for identity, uniqueness, relationships, and designated temporal access**, while permitting explicit additional indexes for advanced cases.

Indexing shall remain largely invisible to users.

The resulting model is:

```text
simple schema
     ↓
automatic sensible indexes
     ↓
efficient API + browser access
     ↓
optional explicit indexes for advanced workloads
```

The system should behave intelligently by default without turning Clio into a database administration product.
