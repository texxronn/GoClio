# Changelog

Notable changes to GoClio are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and product versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Version identifiers are independent; see "Document, product and API versioning"
in [`SPEC.md`](SPEC.md). The API version (`v1`) changes only for incompatible
API changes, and the specification revision tracks the contract document rather
than the product.

## [Unreleased]

### Added

- Projects registry (spec v1.6–v1.7): the implicit `default` project and the
  `GET`/`POST /api/v1/projects` and `GET`/`DELETE /api/v1/projects/{project}`
  API. `POST` rejects reserved names (`api`, `health`, `help`, `projects`,
  `assets`, `favicon.svg`, `default`) with `422` and duplicates with `409`;
  `DELETE` removes only an empty project and never `default`.
- Project-scoped storage: `groups_meta`, `tables_meta`, `fields_meta` and
  `records` gain a `project` column with project-scoped primary and foreign
  keys. Existing databases are rebuilt on open and every row is assigned to
  `default`; no stored data is rewritten. Group and table names are now unique
  per project.
- `/health` and `/api/v1/health` report a `projects` count.
- Project-first routing (spec v1.7 §66): every data and content route now begins
  with the project. Instance routes stay unscoped (`/health`, `/help`,
  `/api/v1/health`, `/api/v1/help`, `/api/v1/projects`, `/assets/...`,
  `/favicon.svg`). Bare `/` and `/api/v1` redirect to `/{default}/` and
  `/api/v1/{default}`; an unknown project returns `404`.
- The data partition moved under `/api/v1/{project}/data/...` (metadata, groups,
  tables, fields, records) and `/{project}/data/...` (table UI, forms, data
  browser at `/{project}/collections/...`). Every data query is scoped by
  project, and managed indexes include the project scope.
- The content tree moved under `/api/v1/{project}/files/{directories,pages}`
  and `/{project}/files/...`. These are the legacy directory and page operations
  under the files partition until the unified files API lands; storage is not yet
  partitioned per project. ClioJS is project-aware (`new Clio({ project })`).

### Documentation

- `SPEC.md` revised to v1.7.
  - v1.5 (section 64) added the content filesystem: stable content-entry IDs, a
    filesystem REST API with `/f/{id}` stable URLs, the `attachment` field type,
    native text extraction with a bounded SQLite full-text index, the search and
    enrichment APIs, optional WebDAV at `/dav`, and a ClioJS file explorer at
    `/files`. Sections 3.1, 13, 14.1, 32.3, 34, 35, 36, 41, 43, 46, 47, 49, 52,
    55.1, 56, 57, 58 and 62 were updated accordingly.
  - v1.6 (section 65) added namespaces as logical projects: project-scoped
    groups/tables and content, the projects API and project-scoped route
    prefixes, project-relative paths, reserved `/p`, and the `default` alias for
    existing routes. Sections 8, 9, 13.4, 17, 32.2, 32.3, 36, 47, 49, 58, 62 and
    64 were updated accordingly.
  - v1.7 (section 66) made project scoping mandatory and organized each project
    into `data` and `files` partitions: every application route begins with the
    project, `default` is explicit, bare `/` and `/api/v1` redirect to it, and
    the Directory and Page APIs are folded into the files partition. Sections
    32, 35, 38, 41, 49, 64 and 65 carry supersession notes where their URLs
    changed.
- `SPEC-CONFORMANCE.md` records the v1.5–v1.7 areas as pending coverage.

This is a specification change only; none of sections 64–66 is implemented yet.

## [1.0.1] - 2026-09-29

### Fixed

- Table metadata `PATCH` now merges `fields` by name instead of replacing the
  complete field list. Adding one field no longer removes the other field
  definitions, their stored record values, or their table columns. Empty and
  partial `fields` lists are non-destructive.

### Added

- Explicit `remove_fields` for table metadata `PATCH`, still returning
  `409 Conflict` when a removed field has stored values.
- Build-time version injection (`-ldflags "-X main.version=..."`) with a
  `Makefile` and a Docker build argument, so a binary reports the release tag it
  was built from through `/api/v1/health`.

### Documentation

- `SPEC.md` sections 19 and 19.1, `SPEC-CONFORMANCE.md`, and `/help` now
  describe the additive field contract.

## [1.0.0] - 2026-09-29

### Added

- Initial product release of the Clio v1 contract: metadata-driven groups,
  tables, fields and records; time-series tables; record querying, filtering,
  sorting, paging, grouping and aggregation; the published content tree; the
  Markdown subset and renderer; ClioJS and the read-only data browser; optional
  authentication and transport security.
