> **SUPERSEDED — HISTORICAL REFERENCE ONLY.**
>
> This document has been consolidated into the authoritative contract at
> [SPEC.md](../../SPEC.md). It is retained for background only and confers no
> requirements. Do not implement from this file.

# Clio

## SPEC-Addendum-ClioJS-library

**Status: Implementation Addendum**

This document defines the optional browser-side JavaScript client library for Clio.

It extends the frozen Clio specification but does not change the Clio HTTP API, data model, query semantics or deployment architecture.

The library is intentionally small.

Its purpose is:

> **Make the Clio HTTP API pleasant to consume from browser JavaScript without introducing a frontend framework, build system or large SDK.**

---

# 1. Goals

The Clio JavaScript library provides a lightweight browser API for:

* discovering Clio metadata
* accessing groups and tables
* querying records
* creating/updating/deleting records
* creating groups and tables
* accessing pages/directories
* iterating through paged query results
* performing common queries without manually constructing HTTP requests

The library must remain a thin wrapper around the existing Clio REST API.

---

# 2. Non-goals

The library is not:

* a frontend framework
* a state-management library
* an ORM
* a data-binding framework
* a charting library
* a UI component library
* a replacement for the Clio REST API
* a client-side database
* a caching layer

The library must not maintain application-wide state.

It must not implement its own persistence.

---

# 3. Distribution

Clio serves the browser library itself.

Recommended stable URL:

```text
/assets/clio.js
```

A versioned URL should also be available:

```text
/assets/clio/v1/clio.js
```

The stable URL may point to the current compatible v1 library.

The versioned URL must remain compatible for the lifetime of the corresponding API version.

The library must not require:

* npm
* Node.js
* bundlers
* transpilers
* package installation
* frontend build tools

A user should be able to use ClioJS in an ordinary HTML page with:

```html
<script src="/assets/clio.js"></script>
```

---

# 4. Same-origin operation

The default ClioJS client operates against the origin from which the library was loaded.

Example:

```text
https://clio.example.com/assets/clio.js
```

automatically targets:

```text
https://clio.example.com/api/v1/
```

No CORS configuration is required.

The library may optionally accept an explicit base URL, but same-origin use must be the simplest and preferred mode.

Example:

```javascript
const clio = new Clio({
  baseUrl: "https://clio.example.com"
});
```

---

# 5. Authentication

ClioJS must work with Clio's optional HTTP Basic Authentication.

The library should rely on the browser's normal authentication mechanisms rather than implementing its own credential/session system.

The library must not store passwords.

The library may allow a caller to provide an authentication mechanism through configuration, but this is optional.

Authentication behaviour remains governed by `SPEC-Addendum-Auth`.

---

# 6. Global API

The browser library should expose a single global:

```javascript
window.Clio
```

Example:

```javascript
const clio = new Clio();
```

The implementation may alternatively expose a factory function, but a simple global constructor/API is preferred.

No global namespace pollution beyond `Clio` should occur.

---

# 7. Metadata API

ClioJS must provide convenient access to metadata.

Example:

```javascript
const metadata = await clio.metadata();
```

Group access:

```javascript
const groups = await clio.groups();
```

Specific group:

```javascript
const group = await clio.group("pool").get();
```

Tables:

```javascript
const tables = await clio.group("pool").tables();
```

Specific table:

```javascript
const table =
  await clio.table("pool", "measurements").metadata();
```

The returned structures should correspond closely to the JSON returned by the REST API.

The library should not create a second incompatible object model.

---

# 8. Table access

Recommended API:

```javascript
const measurements =
  clio.table("pool", "measurements");
```

The table object provides:

```text
metadata()
records()
get(id)
create(record)
update(id, record)
delete(id)
query(...)
```

Example:

```javascript
const table =
  clio.table("vehicle", "service");

const record = await table.get("123");

await table.create({
  date: "2026-09-28",
  odometer: 82431,
  type: "service",
  cost: "183.42"
});
```

---

# 9. Query API

The query API provides a convenient wrapper around the Clio v1 query parameters.

Example:

```javascript
const result = await clio
  .table("vehicle", "service")
  .query({
    limit: 50,
    sort: "date",
    order: "desc"
  });
```

Filtering:

```javascript
const result = await clio
  .table("vehicle", "service")
  .query({
    filter: {
      type: "service",
      "cost.gte": 100
    }
  });
```

The exact JavaScript representation may be implemented differently, but it must map directly and predictably to the documented REST API.

The library must not introduce a separate query language.

---

# 10. Convenience methods

The library should provide lightweight convenience methods for common queries.

Examples:

```javascript
table.list(options)
table.first(options)
table.count(options)
table.distinct(field, options)
table.aggregate(spec, options)
```

These are convenience wrappers and must not expose capabilities unavailable through the REST API.

---

# 11. Paging

The library must hide ordinary API paging where practical.

A user should not need to manually implement:

```text
limit
offset
next page
next page
next page
```

for normal iteration.

The preferred mechanism is asynchronous iteration.

Example:

```javascript
for await (const record of clio
  .table("vehicle", "service")
  .records()) {

  console.log(record);
}
```

The implementation should automatically request additional pages as required.

---

# 12. Query options

The iterator must accept normal Clio query options.

Example:

```javascript
for await (const record of clio
  .table("pool", "measurements")
  .records({
    from: "2026-09-01T00:00:00Z",
    to: "2026-10-01T00:00:00Z",
    sort: "timestamp",
    order: "asc"
  })) {

  console.log(record);
}
```

Paging must remain transparent to the caller.

---

# 13. Page size

ClioJS may choose an appropriate default page size.

The caller may override it.

Example:

```javascript
for await (const record of table.records({
  limit: 100
})) {
  ...
}
```

The library must honour the server's maximum page size.

It must not assume that the requested size was granted.

---

# 14. Query results

For operations that return a complete query response, ClioJS should preserve the server response structure.

Example:

```javascript
const result = await table.query({
  group_by: "type",
  aggregate: "count,cost:sum"
});
```

The caller receives the grouped response rather than an opaque library-specific abstraction.

This keeps ClioJS transparent and easy to understand.

---

# 15. Time-series support

Time-series tables are accessed through the same API.

Example:

```javascript
const pool =
  clio.table("pool", "measurements");

for await (const observation of pool.records({
  from: "2026-09-01T00:00:00Z",
  to: "2026-10-01T00:00:00Z"
})) {
  console.log(observation.timestamp);
}
```

Aggregation:

```javascript
const result = await pool.query({
  from: "2026-09-01",
  to: "2026-10-01",
  bucket: "day",
  aggregate: "temperature:avg,temperature:min,temperature:max"
});
```

ClioJS does not reinterpret time-series semantics.

All timestamp semantics remain those defined by the Clio API.

---

# 16. Record creation and updates

Create:

```javascript
await table.create({
  date: "2026-09-28",
  amount: "123.45"
});
```

Update:

```javascript
await table.update("123", {
  amount: "140.00"
});
```

Delete:

```javascript
await table.delete("123");
```

The library must preserve the API's PATCH semantics:

* omitted properties remain unchanged
* explicit `null` clears a nullable field
* required fields cannot be cleared
* unknown fields are rejected by the server

---

# 17. Directory access

ClioJS provides access to the directory API.

Example:

```javascript
const directory =
  await clio.directory("/pool/reports");
```

The result should expose:

* path
* URL
* children

Example:

```javascript
const directory =
  await clio.directory("/pool");

for (const child of directory.children) {
  console.log(child.name, child.type);
}
```

The library should not duplicate the server's content-tree logic.

---

# 18. Page access

ClioJS provides access to page source.

Example:

```javascript
const page =
  await clio.page("/pool/reports/latest.md");
```

For Markdown pages:

```javascript
console.log(page.content);
```

The page object should expose:

```text
path
url
content_type
content
created_at
updated_at
```

The content returned is the original source as defined by the Clio page API.

---

# 19. Client-side Markdown rendering

ClioJS should integrate with the separately exposed Clio Markdown renderer.

Example:

```javascript
const page =
  await clio.page("/pool/reports/latest.md");

const html =
  Clio.Markdown.render(page.content);

document.querySelector("#content").innerHTML = html;
```

The exact rendering API may vary, but it should remain simple.

The renderer must apply the same safety expectations defined in the main Clio specification.

---

# 20. Error handling

ClioJS must reject failed HTTP requests with useful errors.

Errors should expose:

```text
HTTP status
Clio error code
Clio message
```

Example:

```javascript
try {
  await table.get("does-not-exist");
} catch (error) {
  console.log(error.status);
  console.log(error.code);
  console.log(error.message);
}
```

The library must not hide server errors.

---

# 21. HTTP behaviour

ClioJS should use the browser's standard:

```javascript
fetch()
```

API where practical.

The library must not introduce its own networking stack.

It should support:

* GET
* POST
* PATCH
* DELETE

as required by the Clio API.

---

# 22. Request cancellation

Where practical, API calls should accept an `AbortSignal`.

Example:

```javascript
const controller =
  new AbortController();

const result =
  await table.query({
    signal: controller.signal
  });
```

This is a convenience feature and must remain lightweight.

---

# 23. Caching

ClioJS must not maintain a persistent application cache.

No:

* local database
* IndexedDB cache
* localStorage cache
* LRU cache
* automatic metadata cache

Temporary in-memory caching of a request during one operation is acceptable where required for implementation simplicity.

The server remains authoritative.

---

# 24. Mutations and concurrency

ClioJS must not attempt to implement its own optimistic concurrency protocol.

The server's API remains authoritative.

If the server returns a conflict or validation error, the error is returned to the caller.

---

# 25. Type handling

ClioJS should preserve server JSON types.

In particular:

* integers remain JavaScript numbers where safely representable
* booleans remain booleans
* strings remain strings
* dates remain strings
* datetimes remain strings
* decimals remain strings

The library must **not automatically convert decimal values into JavaScript floating-point numbers**.

Example:

```javascript
record.cost === "183.42";
```

This avoids loss of precision for financial values.

---

# 26. API versioning

ClioJS v1 targets:

```text
/api/v1/
```

The client library should identify its API compatibility version.

The library must not silently use incompatible API semantics.

A future ClioJS v2 may target `/api/v2/`.

---

# 27. Version information

The library should expose:

```javascript
Clio.version
```

and preferably:

```javascript
Clio.apiVersion
```

Example:

```javascript
console.log(Clio.version);
console.log(Clio.apiVersion);
```

---

# 28. Generated and hand-written pages

ClioJS must be usable from:

* hand-written HTML pages
* Markdown-generated pages
* HTML pages posted by an LLM
* static content published through Clio
* simple custom applications

The library must not assume a particular frontend framework.

---

# 29. Agent/LLM compatibility

The ClioJS library must be described in `/help`.

The help documentation should include a small usage example.

An LLM should be able to discover:

```text
1. /help
2. /metadata
3. API
4. /assets/clio.js
5. client-side usage
```

and generate a browser page using ClioJS without requiring separate package installation.

---

# 30. Example complete page

A minimal page should be possible:

```html
<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Pool</title>
</head>
<body>

<h1>Latest Pool Measurements</h1>

<div id="measurements"></div>

<script src="/assets/clio.js"></script>
<script>
(async () => {
  const clio = new Clio();
  const table = clio.table("pool", "measurements");

  const rows = [];

  for await (const record of table.records({
    sort: "timestamp",
    order: "desc",
    limit: 20
  })) {
    rows.push(record);

    if (rows.length >= 20) {
      break;
    }
  }

  document.querySelector("#measurements")
    .textContent = JSON.stringify(rows, null, 2);
})();
</script>

</body>
</html>
```

No build process is required.

---

# 31. Implementation constraints

ClioJS must remain small.

Do not introduce:

* React
* Vue
* Svelte
* Angular
* Redux
* frontend state-management frameworks
* a package manager requirement
* a bundling requirement
* an ORM
* a client-side database

A small amount of helper code is preferred to a general-purpose SDK architecture.

The library should be understandable by reading one or a few small JavaScript files.

---

# 32. Testing

The ClioJS implementation must include tests for:

* metadata access
* table access
* record CRUD
* filtering
* sorting
* paging
* async iteration
* aggregation
* time-series queries
* page retrieval
* directory retrieval
* error handling
* decimal preservation
* cancellation where implemented
* Markdown integration

Browser-level tests should verify that the library can load from:

```text
/assets/clio.js
```

and successfully communicate with the running Clio service.

---

# 33. Definition of done

The ClioJS library is complete when:

* it is served directly by Clio
* it requires no package installation
* it requires no frontend build process
* it uses the existing Clio API
* it supports table metadata
* it supports CRUD
* it supports query parameters
* it transparently handles paging
* it supports async iteration
* it supports time-series queries
* it supports directory access
* it supports Markdown page retrieval
* it integrates with the client-side Markdown renderer
* it preserves decimal values
* it exposes useful HTTP errors
* it works in a plain browser page
* it is documented through `/help`
* it remains small and framework-independent

---

# 34. Final boundary

ClioJS exists to make this:

```text
HTTP API
   ↓
pleasant browser JavaScript
```

and nothing more.

The library must remain a **thin client over Clio's API**, not become a second application framework.
