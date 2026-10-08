# API and publishing examples

These payloads can be posted directly to a running Clio instance. The examples
use `http://localhost:8080` and the `default` project; set `CLIO_BASE_URL` and
replace the URL if your instance uses a different canonical origin, and replace
`default` with a project name to target another project.

Every application route is project-scoped (spec v1.7 section 66). Bare `/`
redirects to `/{default}/` and bare `/api/v1` redirects to `/api/v1/default`.

## Create example tables

Create their groups first:

```sh
curl -X POST http://localhost:8080/api/v1/default/data/groups \
  -H 'Content-Type: application/json' \
  --data '{"name":"vehicle","label":"Vehicle"}'

curl -X POST http://localhost:8080/api/v1/default/data/groups \
  -H 'Content-Type: application/json' \
  --data '{"name":"pool","label":"Pool"}'
```

Create a record table and a time-series table from the included metadata:

```sh
curl -X POST http://localhost:8080/api/v1/default/data/groups/vehicle/tables \
  -H 'Content-Type: application/json' \
  --data-binary @examples/record-table.json

curl -X POST http://localhost:8080/api/v1/default/data/groups/pool/tables \
  -H 'Content-Type: application/json' \
  --data-binary @examples/timeseries-table.json
```

Insert records (decimal values are JSON strings):

```sh
curl -X POST http://localhost:8080/api/v1/default/data/groups/vehicle/tables/service/records \
  -H 'Content-Type: application/json' \
  --data '{"service_date":"2026-09-20","odometer_km":42000,"category":"service","cost":"320.50","notes":"Oil and filter"}'

curl -X POST http://localhost:8080/api/v1/default/data/groups/pool/tables/measurements/records \
  -H 'Content-Type: application/json' \
  --data '{"measured_at":"2026-09-20T08:00:00Z","ph":"7.4","free_chlorine":"2.1","temperature_c":"27.5"}'
```

Try an aggregate query and open the generated table views:

```sh
curl 'http://localhost:8080/api/v1/default/data/groups/vehicle/tables/service/records?aggregate=count,cost:sum'
curl 'http://localhost:8080/api/v1/default/data/groups/pool/tables/measurements/records?bucket=day&aggregate=temperature_c:avg'
```

Open `/default/data/vehicle/service` and `/default/data/pool/measurements` to use
the browser table views and their query controls. The read-only data browser is
at `/default/data`, and metadata is discoverable under
`/api/v1/default/data/metadata`.

## Publish the Markdown and browser example

Publish the Markdown source and HTML client example as files. The page is
created or replaced with `PUT /files?path=` and the raw file as the body:

```sh
curl -X PUT 'http://localhost:8080/api/v1/default/files?path=%2Fexamples%2Freport.md' \
  -H 'Content-Type: text/markdown' --data-binary @examples/report.md

curl -X PUT 'http://localhost:8080/api/v1/default/files?path=%2Fexamples%2Fclient-rendering.html' \
  -H 'Content-Type: text/html' --data-binary @examples/client-rendering.html
```

Visit `/default/files/examples/client-rendering.html`. It resolves the Markdown
entry through the files API, fetches its stored source from the stable content
URL, and renders it with `/assets/clio-markdown.js`. Published HTML is trusted
executable content, so only publish HTML you control.

The file explorer is at `/default/files`; list the catalog with
`GET /api/v1/default/files` and read a single entry with
`GET /api/v1/default/files/{id}` or its bytes from
`GET /api/v1/default/files/{id}/content` (the stable human link is
`/default/files/id/{id}`).

## Search and WebDAV

Native text from the published Markdown and HTML is searchable:

```sh
curl 'http://localhost:8080/api/v1/default/search?q=chlorine'
```

Set `CLIO_WEBDAV_ENABLED=true` to mount the project content tree read/write at
`/default/files/dav` (or `/api/v1/default/files/dav`), for example:

```sh
curl -T examples/report.md http://localhost:8080/api/v1/default/files/dav/examples/report-copy.md
```

## Backup and restore

Stop Clio, or use the consistent one-command snapshot:

```sh
CLIO_DATA_DIR=./data clio backup /backups/2026-10-08
CLIO_DATA_DIR=./data clio restore /backups/2026-10-08 --force
```

The database is mandatory: it preserves content-entry IDs, timestamps and
agent-supplied extraction. See [`README.md`](../README.md) for the stop-copy
procedure and the manifest layout.
