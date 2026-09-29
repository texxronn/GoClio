# Changelog

Notable changes to GoClio are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and product versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Version identifiers are independent; see "Document, product and API versioning"
in [`SPEC.md`](SPEC.md). The API version (`v1`) changes only for incompatible
API changes, and the specification revision tracks the contract document rather
than the product.

## [Unreleased]

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
