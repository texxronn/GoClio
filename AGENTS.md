# Repository guidance

GoClio implements the Clio v1 contract documented in `SPEC.md`. Keep the code
small, use the standard library where practical, and avoid changing the public
contract without updating the specification deliberately.

## Development

- Use the Go version declared in `go.mod` (Go 1.25).
- The SQLite driver uses cgo; building locally requires a C toolchain.
- Keep SQLite as the single persistent data store and published content in the
  configured content directory.
- Keep handlers consistent with the existing API error format and status codes.
- Do not log request bodies, credentials, query strings, or published content.

## Verification

Run the relevant checks after code changes:

```sh
go test -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
go build -tags sqlite_fts5 -buildvcs=false -o /tmp/gocl-clio-check .
```

`make check` runs the same sequence and injects the git-derived product version.

The `sqlite_fts5` tag is mandatory for running: the full-text index uses SQLite
FTS5 (section 64.6), which the cgo driver only compiles in under that tag. A
binary built without it still compiles but fails at startup when it creates the
`content_search` table.

Tests should exercise behavior through HTTP where practical and use temporary
SQLite databases and content directories. Add regression tests for fixes and
new behavior without relying on external services. Specification areas are
mapped to tests in `SPEC-CONFORMANCE.md`; keep that mapping current when adding
coverage.

## Examples and deployment

- Keep example API payloads and publishing samples under `examples/`.
- Keep the Docker image non-root and persist `/var/lib/clio` as one volume.
- Keep configuration examples limited to settings implemented by this
  executable; defaults and supported environment variables are listed in
  `README.md`.
