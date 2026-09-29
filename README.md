# GoClio

GoClio is a Go implementation of the frozen Clio v1 contract in `SPEC.md`
(specification revision 1.4; product version 1.0.0; API version v1). It uses Go's
standard HTTP server, one SQLite database in WAL mode, and the filesystem for
published content. `SPEC-CONFORMANCE.md` maps specification areas to automated
tests.

## Build and run

Requires Go 1.25 and a C toolchain for `github.com/mattn/go-sqlite3`.

```sh
make build            # builds ./clio, version injected from the git tag
CLIO_ADDR=127.0.0.1:8080 \
CLIO_DATA_DIR=./data \
CLIO_BASE_URL=http://localhost:8080 \
./clio
```

`make check` runs the tests, `go vet ./...`, and a build. Without `make`, build
directly and pass the version yourself:

```sh
CGO_ENABLED=1 go build -buildvcs=false -ldflags="-X main.version=1.0.0" -o clio .
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
injected version; it defaults to `local` / `1.0.0` when unset. The image runs as
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

## API and content

Open `/help` for human-readable usage or `/api/v1/help` for machine-oriented
documentation. `/health` and `/api/v1/health` report service status. The
browser Markdown renderer is `/assets/clio-markdown.js`; the optional browser
client is served at `/assets/clio.js` (also `/assets/clio/v1/clio.js`). Load the
script directly in a page and use `new Clio()` for same-origin API access. When
authentication is enabled, clients use standard HTTP Basic Authentication on
every route.

Structured tables use `/t/{group}/{table}` in the browser and
`/api/v1/groups/{group}/tables/{table}` in the API. Content pages and
directories use the root URL namespace. Refer to `SPEC.md` for the complete
API and behavior contract.

Ready-to-post record/time-series table metadata and Markdown publishing
examples are in [`examples/`](examples/README.md).

## Versioning and releases

GoClio tracks three independent identifiers, described in `SPEC.md`:

| Identifier | Current | Where it appears |
| --- | --- | --- |
| Specification revision | `1.4` | `SPEC.md` header |
| Product version | `1.0.0` (source default) | `version` in `/api/v1/health` |
| API version | `v1` | `/api/v1/` route prefix |

Releases are tagged `v<MAJOR>.<MINOR>.<PATCH>` following Semantic Versioning.
The product version reported by a binary is injected at build time from the
nearest git tag (without the leading `v`), so a release build reports its actual
tag. The literal `1.0.0` in `main.go` is only the default used when building
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

Stop Clio before copying the data directory so SQLite's WAL contents and the
published content tree are captured together. Back up the complete
`CLIO_DATA_DIR`, including `clio.db`, any `clio.db-wal` / `clio.db-shm` files,
and `content/`. If `CLIO_DB` points outside `CLIO_DATA_DIR`, back up that
database and its WAL/SHM files as well. Restore by stopping Clio, replacing
the data directory and any separately located database with the backup, then
starting Clio with the same `CLIO_DB` and `CLIO_DATA_DIR` configuration.
Confirm recovery through `/health`; SQLite runs `quick_check` as part of that
endpoint.
