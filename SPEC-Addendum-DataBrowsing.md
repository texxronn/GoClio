# SPEC ADDENDUM — Collection Data Browser

## Status

Normative addition to the Clio specification.

This addendum defines the standard browser interface for inspecting data stored in Clio collections and tables.

## 1. Purpose

Clio shall provide a built-in browser for inspecting collection data without requiring the creation of a custom Page or Form.

The Data Browser is an **inspection and navigation interface**.

It is intentionally not a spreadsheet editor.

The basic interaction shall be:

> Select a collection → select a table → inspect its records.

Every normal Clio table shall therefore have a useful browser representation automatically.

---

## 2. Canonical URI

The canonical URI for a table browser shall be:

```text
/collections/{collection}/{table}
```

Examples:

```text
/collections/health/weight
/collections/home/power
/collections/vehicle/service
```

The collection-level URI:

```text
/collections/{collection}
```

may later provide a collection overview, but is not required for the initial implementation.

Pagination may be represented in the query string:

```text
/collections/health/weight?page=3
```

The implementation may add additional query parameters for filtering or sorting in future versions without changing the base URI structure.

---

## 3. Primary Layout

The browser shall use a simple three-part layout:

```text
┌─────────────────────────────────────────────────────────────────────┐
│ Collection: [ health ▼ ]                                           │
├───────────────────┬─────────────────────────────────────────────────┤
│                   │                                                 │
│ Tables            │  weight                                        │
│                   │                                                 │
│ ▾ health          │  ┌────────┬─────────┬──────────────┐            │
│   weight          │  │ Date   │ Weight  │ Notes        │            │
│   blood_pressure  │  ├────────┼─────────┼──────────────┤            │
│   ...             │  │ ...    │ ...     │ ...          │            │
│                   │  │ ...    │ ...     │ ...          │            │
│                   │  └────────┴─────────┴──────────────┘            │
│                   │                                                 │
│                   │       [ Previous ] 1 2 3 ... [ Next ]            │
└───────────────────┴─────────────────────────────────────────────────┘
```

The three areas are:

1. **Collection selector**
2. **Table navigation pane**
3. **Read-only data grid**

The interface shall remain compact and information-dense.

---

## 4. Collection Selector

A compact drop-down shall list the collections available to the current user.

Changing the selected collection shall:

* update the table navigation pane
* select the appropriate table if one is already specified in the URI
* update the URI to reflect the selected collection/table

The currently selected collection shall always be apparent from the interface.

The selector shall not require the user to navigate through a separate
collection overview page.
