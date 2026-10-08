# GoClio

GoClio is a Go implementation of the frozen Clio v1 contract in `SPEC.md`
(specification revision 1.7; product version 2.0.0; API version v1). It uses Go's
standard HTTP server, one SQLite database in WAL mode, and the filesystem for
published content. `SPEC-CONFORMANCE.md` maps specification areas to automated
tests.

Feature summary:

- Metadata-driven groups, tables, fields and records, including record and
  time-series tables, with filtering, sorting, paging, grouping and aggregation.
- Named **projects** that scope their own structured data and content; the
  implicit `default` project always exists.
- Two partitions per project: `data` (groups, tables, records, metadata) and
  `files` (unified directories, pages and files with stable IDs).
- Optional full-text search over extracted page/file text (SQLite FTS5, native
  text and PDF text extraction) with an agent enrichment write-back API.
- `attachment` fields that link records to files by stable ID.
- Optional WebDAV, an opt-in one-command backup/restore path, an interactive
  data browser and file explorer, and a dependency-free browser client.

## Build and run

Requires Go 1.25 and a C toolchain for `github.com/mattn/go-sqlite3`. Builds and
tests must pass `-tags sqlite_fts5`: the full-text index (section 64.6) uses
SQLite FTS5, which the driver only compiles in under that tag. `make` applies it
for you; a binary built without it fails at startup when it creates the search
index.

```sh
make build            # builds ./clio, version injected from the git tag
CLIO_ADDR=127.0.0.1:8080 \
CLIO_DATA_DIR=./data \
CLIO_BASE_URL=http://localhost:8080 \
./clio
```

`make check` runs the tests, `go vet`, and a build (all with the tag). Without
`make`, build directly and pass the version yourself:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -buildvcs=false -ldflags="-X main.version=2.0.0" -o clio .
```

The default listener is `0.0.0.0:8080`; the default data directory is
`./data`. The SQLite database defaults to `$CLIO_DATA_DIR/clio.db`, and
published pages, files and directories live under `$CLIO_DATA_DIR/content`.

## Docker

Build the image from the repository root and run it with a persistent named
volume:

```sh
# Tag the image with the release it contains and bake that version into the binary.
VERSION=$(git describe --tags --always | sed 's/^v//')
docker build --build-arg VERSION="$VERSION" -t gocl.io:"$VERSION" .
docker run --rm --name clio \
  --publish 8080:8080 \
  --env-file examples/clio.env.example \
  --volume clio-data:/var/lib/clio \
  gocl.io:"$VERSION"
```

For a Compose build, set `CLIO_VERSION` to select both the image tag and the
injected version; it defaults to `local` / `2.0.0` when unset. The image runs as
an unprivileged `clio` user. The named volume stores both the
SQLite database and published content under `/var/lib/clio`. The container
listens on port 8080; open <http://localhost:8080/help> after startup. For a
public deployment, set `CLIO_BASE_URL` to the canonical HTTPS URL and terminate
TLS at a trusted reverse proxy. Stop the container with `docker stop clio`;
the named volume remains available for backup and restore.

Configuration:

| Variable | Default | Purpose |
| --- | --- | --- |
| `CLIO_ADDR` | `0.0.0.0:8080` | HTTP bind address in `host:port` form |
| `CLIO_DATA_DIR` | `./data` | Data and content directory |
| `CLIO_DB` | `$CLIO_DATA_DIR/clio.db` | SQLite database path |
| `CLIO_BASE_URL` | `http://localhost:8080` | Canonical public HTTP(S) URL |
| `CLIO_AUTH_ENABLED` | `false` | Require HTTP Basic Authentication |
| `CLIO_AUTH_USER` | `admin` | The single configured username |
| `CLIO_AUTH_PASSWORD_HASH` | unset | Required when auth is enabled; bcrypt hash only |
| `CLIO_REQUIRE_HTTPS` | `true` | Require HTTPS or an allowed trusted HTTP source when auth is enabled |
| `CLIO_TRUSTED_HTTP_NETWORKS` | `127.0.0.0/8,::1/128` | Comma-separated CIDRs permitted to use HTTP; localhost is always allowed |
| `CLIO_TRUST_PROXY` | `false` | Trust forwarded protocol only from configured proxy networks |
| `CLIO_TRUSTED_PROXY_NETWORKS` | empty | CIDRs of trusted immediate reverse-proxy peers; required if proxy trust is enabled |
| `CLIO_TLS_CERT` | unset | TLS certificate for direct HTTPS; configure with `CLIO_TLS_KEY` |
| `CLIO_TLS_KEY` | unset | TLS private key for direct HTTPS; configure with `CLIO_TLS_CERT` |
| `CLIO_WEBDAV_ENABLED` | `false` | Expose the project content tree over WebDAV at `/{project}/files/dav` and `/api/v1/{project}/files/dav` |

Authentication is disabled by default. When enabled, all routes—including
health, help, published pages, API endpoints, and static assets—require Basic
Authentication. Generate a bcrypt password hash using `clio hash-password`
(the password is entered without terminal echo), then provide the output as
`CLIO_AUTH_PASSWORD_HASH` through a secret manager or protected environment
file. Never store a plaintext password in configuration.

For a TLS-terminating reverse proxy, configure Clio with its trusted peer
network, for example:

```sh
CLIO_AUTH_ENABLED=true
CLIO_AUTH_USER=admin
CLIO_AUTH_PASSWORD_HASH='<bcrypt hash from clio hash-password>'
CLIO_REQUIRE_HTTPS=true
CLIO_TRUST_PROXY=true
CLIO_TRUSTED_PROXY_NETWORKS=10.10.10.0/24
```

HTTPS is required by default when authentication is enabled. Loopback and
explicitly configured `CLIO_TRUSTED_HTTP_NETWORKS` may use HTTP; private
networks are not trusted automatically. To terminate TLS at a reverse proxy,
set `CLIO_TRUST_PROXY=true` and list its immediate peer CIDRs in
`CLIO_TRUSTED_PROXY_NETWORKS`. Clio honors `X-Forwarded-Proto` only from those
trusted peers. Alternatively, configure both `CLIO_TLS_CERT` and
`CLIO_TLS_KEY` to terminate TLS directly in Clio. Setting
`CLIO_REQUIRE_HTTPS=false` explicitly allows HTTP, but Basic Authentication
without TLS exposes credentials to network observers. Use HTTPS on untrusted
networks.

Published `.html` pages are served as trusted executable HTML. Only trusted
publishers should be allowed to create or replace them.

The human write routes (record and folder creation) reject a request whose
present `Origin` or `Referer` host does not match the request host, so a browser
with a cached Basic credential cannot be driven from a cross-site form. Requests
without either header, and the JSON API, are unaffected.

## API and content

Open `/help` for human-readable usage or `/api/v1/help` for machine-oriented
documentation (the format LLMs should consume). `/health` and `/api/v1/health`
report service status, including the project count. The browser Markdown
renderer is `/assets/clio-markdown.js`; the optional browser client is served at
`/assets/clio.js` (also `/assets/clio/v1/clio.js`). Load the script directly in
a page and use `new Clio()` for same-origin API access. When authentication is
enabled, clients use standard HTTP Basic Authentication on every route.

Every application route is project-scoped. Bare `/` redirects to `/{default}/`
and bare `/api/v1` redirects to `/api/v1/default`. The only instance-level
routes are `/health`, `/help`, `/api/v1/health`, `/api/v1/help`,
`/api/v1/projects`, `/assets/...` and `/favicon.svg`.

Each project is divided into two partitions:

- **`data`** — structured data. The API lives under
  `/api/v1/{project}/data/...` (metadata, groups, tables, fields and records);
  the server-rendered table UI and forms live under
  `/{project}/data/{group}/{table}`, and the read-only data browser is at
  `/{project}/data` (group, table and page in the query string).
- **`files`** — the content tree. The API lives under
  `/api/v1/{project}/files...` (list, read, create/replace, move, copy, delete,
  rescan, search, extraction and WebDAV); the human file explorer is at
  `/{project}/files`, rendered pages and downloads are at their path URLs, and
  `/{project}/files/id/{id}` is the stable ID link. A human search page is at
  `/{project}/search`.

Structured tables therefore use
`/api/v1/{project}/data/groups/{group}/tables/{table}` in the API and
`/{project}/data/{group}/{table}` in the browser. Content pages and files use
the files partition. Refer to `SPEC.md` and `/api/v1/help` for the complete API
and behavior contract.

The full-text search index uses SQLite FTS5 and is exposed at
`GET /api/v1/{project}/search?q=...`. Native text-like formats and PDF text
layers are extracted automatically; OCR is out of scope and sidecar agents
write text back through `PUT /api/v1/{project}/files/extraction` with a
fingerprint check. WebDAV is opt-in with `CLIO_WEBDAV_ENABLED=true` and mounts a
project's content tree at `/{project}/files/dav` and `/api/v1/{project}/files/dav`.

Ready-to-post record/time-series table metadata and Markdown publishing
examples are in [`examples/`](examples/README.md).

## Versioning and releases

GoClio tracks three independent identifiers, described in `SPEC.md`:

| Identifier | Current | Where it appears |
| --- | --- | --- |
| Specification revision | `1.7` | `SPEC.md` header |
| Product version | `2.0.0` (source default) | `version` in `/api/v1/health` |
| API version | `v1` | `/api/v1/` route prefix |

Releases are tagged `v<MAJOR>.<MINOR>.<PATCH>` following Semantic Versioning.
The product version reported by a binary is injected at build time from the
nearest git tag (without the leading `v`), so a release build reports its actual
tag. The literal `2.0.0` in `main.go` is only the default used when building
without a tag, such as inside the Docker build context where `.git` is
excluded. The specification header records the documented product version and is
updated deliberately when the product version is bumped; at release time the tag
and the header should agree.

- `make build` injects `git describe`.
- Docker accepts `--build-arg VERSION=<version>`; Compose reads `CLIO_VERSION`.
- Add an entry to [`CHANGELOG.md`](CHANGELOG.md) for each release.
- Push a `v<version>` tag to publish a GitHub release automatically. The
  `Release` workflow uses the matching `CHANGELOG.md` section as the notes; run
  it manually from the Actions tab to publish a release for a tag that already
  exists.

The API version changes only for incompatible API changes; additive,
backward-compatible changes stay under `/api/v1/`.

## Backup and restore

A backup captures the SQLite database and the content tree together. The
database is mandatory: it holds durable state the filesystem cannot reproduce —
content-entry IDs, entry timestamps, and agent-supplied extracted text
(sections 57 and 64.11).

The always-correct procedure is stop-copy: stop Clio, then copy the complete
`CLIO_DATA_DIR`, including `clio.db`, any `clio.db-wal` / `clio.db-shm` files,
and `content/`. If `CLIO_DB` points outside `CLIO_DATA_DIR`, back up that
database and its WAL/SHM files as well. Restore by stopping Clio, replacing the
data directory and any separately located database with the backup, then
starting Clio with the same `CLIO_DB` and `CLIO_DATA_DIR` configuration.
Confirm recovery through `/health`; SQLite runs `quick_check` as part of that
endpoint.

A one-command path is also available. It is **not** coordinated with live
writers: stop Clio before running `clio backup`, exactly as for the stop-copy
procedure, so the content copy and the database snapshot describe the same
point in time.

```sh
# Stop Clio first; then snapshot the configured database and content tree.
CLIO_DATA_DIR=./data clio backup /backups/2026-10-08

# Restore into the configured data directory. Refuses to overwrite existing
# data unless --force is given.
CLIO_DATA_DIR=./data clio restore /backups/2026-10-08 --force
```

`clio backup <dest>` creates `dest` (it must not exist or must be empty),
snapshots the database with SQLite `VACUUM INTO` — one consistent, compact file
that includes the FTS5 full-text index, so no `-wal`/`-shm` sidecars are needed
— copies the content tree, and writes `manifest.json`. `VACUUM INTO` alone makes
the database file internally consistent, but the copy of the content tree is not
transactionally coordinated with it; run the command with the service stopped so
neither the database nor the content tree changes during the backup. It fails
safely when `dest` is not empty, and requires the database to exist.

`clio restore <src>` validates `manifest.json`, replaces the configured
database and content directory, then reconciles the catalog with the restored
files: entry IDs, timestamps and agent text are preserved, a raw file present
before the backup is adopted, and an entry whose file vanished is dropped.
Restore refuses a non-empty `CLIO_DATA_DIR` unless `--force` is given, and must
also be run with Clio stopped.

A backup directory contains:

| Entry | Purpose |
| --- | --- |
| `manifest.json` | `format` (`clio-backup`), `format_version`, `product_version`, `created_at`, and the database/content names |
| `clio.db` | `VACUUM INTO` snapshot of the SQLite database: IDs, timestamps, enrichment and the FTS index |
| `content/` | Copy of the content tree, one subtree per project |

Inside the container, the data volume is `/var/lib/clio`; back it up with
`docker run --rm -v clio-data:/var/lib/clio gocl.io:"$VERSION" backup /backup`
after mounting a destination, or simply copy the named volume while the
container is stopped.
