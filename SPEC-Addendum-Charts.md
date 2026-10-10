# SPEC ADDENDUM — Charts for Time-Series Tables

## Status

**Proposed.** This addendum is a design and implementation plan. It is not yet
normative and has not been incorporated into [SPEC.md](SPEC.md). Nothing here
requires implementation until it is reviewed and the durable rules are merged
into the authoritative contract.

## Purpose

Add rudimentary graphs and charts for time-series tables in the Collection Data
Browser (`/{project}/data/{group}/{table}`), so timestamped measurements can be
inspected visually without authoring a custom Page or Form.

The feature must preserve Clio's existing principles:

* server-rendered HTML by default;
* minimal JavaScript and no frontend framework;
* no build step and no external charting dependency;
* no new API endpoint;
* one service, one executable, one origin.

The chart view reuses the existing time-series query capability (`from`, `to`,
`bucket`, `aggregate`) and renders inline SVG in the browser.

## 1. Scope

### 1.1 MVP

* A `Table | Chart` view toggle in the Data Browser content pane, shown only
  when table metadata reports `kind: "timeseries"`.
* A single series plotted for one numeric field (`integer` or `decimal`).
* Aggregate selection: `avg` (default), `min`, `max`, `sum`, `count`.
* Bucket selection: `hour`, `day`, `week`, `month`, with a sensible default
  derived from the selected range.
* Range presets: 24 hours, 7 days, 30 days, 90 days, 1 year, All.
* Chart types: line (default) and bar.
* Shareable URL state.
* Loading, empty, and error states, plus a notice when the bucket limit is
  reached.

### 1.2 Non-goals

The following are explicitly out of scope for the first iteration:

* multiple series, dual axes, or moving averages;
* zoom, pan, brush selection, or annotations;
* interactive tooltips;
* server-rendered or static chart images;
* CSV, PNG, or SVG export;
* any new REST endpoint, database object, or configuration setting.

## 2. User interface

The Data Browser keeps the table grid as the default representation. A small
segmented control switches between the table view and the chart view.

The chart pane contains:

```text
Title
[ Table | Chart ]
Range: 24h 7d 30d 90d 1y All
Bucket: hour | day | week | month
Field: <numeric visible field>
Aggregate: avg | min | max | sum | count
Type: line | bar
──────────────────────────────────────
<svg chart>
42 buckets · avg of temperature · UTC
```

The toggle is hidden for `record` tables. Hidden fields are excluded from the
field list. Changing any control re-queries the API; the table view remains the
accessible fallback and is unchanged.

## 3. Data source

Chart data is read from the existing records endpoint using a time bucket and an
aggregate. `bucket` requires `aggregate`, comparisons and buckets use UTC, and
empty buckets are omitted (SPEC sections 29 and 29.2–29.3).

Example:

```text
GET /api/v1/{project}/data/groups/{group}/tables/{table}/records
    ?from=2026-08-30T00:00:00Z
    &to=2026-09-29T00:00:00Z
    &bucket=day
    &aggregate=temperature:avg
    &limit=1000
```

Rules:

* `from` and `to` are RFC 3339 timestamps derived from the selected range, with
  `to` exclusive.
* For a field aggregate, the parameter is `aggregate={field}:{function}`. For
  `count`, the fieldless `aggregate=count` form is used.
* The response shape is the existing bucket response:
  `{"buckets":[{"bucket_start":"…","{field}_{function}":…}],"page":{…}}`.
* `limit` is 1000, the maximum for bucketed queries. If the response fills the
  limit, the chart notes that only the first 1000 buckets are shown.
* Decimal values are JSON strings by default; the client converts them with
  `Number()` before plotting. The optional `decimal_format=number` parameter
  (SPEC section 14.6) may be adopted later, but client-side parsing keeps the
  chart correct regardless of the server revision.

## 4. Rendering

Charts are rendered as inline SVG:

* build with `document.createElementNS("http://www.w3.org/2000/svg", …)`;
* use a `viewBox` with `width:100%` so the chart scales with the pane;
* colour with `currentColor` and the existing CSS custom properties
  (`--accent`, `--line`, `--muted`) so light and dark themes and print work;
* draw a small number of horizontal gridlines with value labels and
  first/middle/last time labels, formatted for the bucket granularity;
* render a line as a `<polyline>` and bars as `<rect>` elements;
* expose `role="img"` with `<title>` and `<desc>` for assistive technology.

No animation is required. No external resource or font is loaded.

## 5. URL and navigation

Chart state is encoded in the existing browser URL so views are shareable and
browser history works, consistent with SPEC section 35.1:

```text
/{project}/data/{group}/{table}?view=chart&range=30d&bucket=day&agg=avg&field=temperature&type=line
```

The default view (`table`) omits `view`. The existing `?page=` parameter
continues to apply to the table view only.

## 6. Code shape

The implementation stays inside `assets/clio.js` and does not expand the public
ClioJS client contract in the first iteration:

* `DataBrowser` gains a view state (`table` or `chart`), a chart controls bar,
  and a `drawChart(...)` path alongside `drawGrid(...)`;
* small pure helpers handle scales, axis ticks, and SVG construction;
* stale responses are ignored using the existing request sequence guard;
* control changes are debounced briefly.

If a reusable chart helper is later exposed (for example `Clio.Chart`), it must
be specified deliberately and covered by the ClioJS contract.

## 7. Specification integration

When accepted, the durable rules should be merged into [SPEC.md](SPEC.md):

* a new subsection under section 35 defining the chart view, controls, query
  usage, UTC and label behaviour, the bucket-limit note, URL parameters, and the
  "no new endpoint, no dependency, no build step" boundary;
* a note in section 35.3 extending the browser verification requirements to
  cover chart rendering and bucketed queries;
* a note in the ClioJS section that the browser uses existing bucket and
  aggregate queries and requires no new client capability;
* updates to the API help text and to `SPEC-CONFORMANCE.md`.

## 8. Verification

* Node behaviour tests (`testdata/cliojs_test.cjs`) provide the primary
  coverage:
  * extend the DOM stub with `createElementNS` and SVG-friendly attributes;
  * assert that a timeseries table triggers a bucketed aggregate query;
  * assert that the SVG contains the expected `polyline`/`rect` node count and
    axis labels;
  * assert that decimal strings such as `"20.30"` are plotted as numbers;
  * assert that the toggle is absent for `record` tables.
* Go HTTP tests (`collection_browser_test.go`) confirm the browser routes and
  shell are unchanged and still load ClioJS.
* Update `SPEC-CONFORMANCE.md` when coverage is added.

## 9. Delivery

* No schema or API change is required; deployment is a single image replace.
* The browser asset changes. Because static assets are currently served without
  cache validators, add a cache-busting version to the script reference (for
  example `/assets/clio.js?v=<libraryVersion>`) or bump the versioned asset path
  so clients pick up the new client.

## 10. Phases

1. **MVP** — view toggle, single series, `avg`, range presets, line chart, URL
   state, empty and error states, tests, and the section 35 rules.
2. **Polish** — bar chart, `count`, bucket-limit notice, improved time labels,
   optional local-time display.
3. **Extensions** — multiple series, custom `from`/`to` inputs, tooltips and a
   resize observer, then a server-rendered SVG option if a no-JavaScript chart
   is required.

## 11. Open questions

* Single series for the MVP, or multiple series from the start?
* Should a line connect across missing buckets or break at gaps?
* Should time labels be UTC (simplest, matches the API) or viewer-local?
* Is a no-JavaScript or static chart needed, or is the client-rendered browser
  view sufficient?
