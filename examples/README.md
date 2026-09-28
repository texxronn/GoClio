# API and publishing examples

These payloads can be posted directly to a running Clio instance. The examples
use `http://localhost:8080`; set `CLIO_BASE_URL` and replace the URL if your
instance uses a different canonical origin.

## Create example tables

Create their groups first:

```sh
curl -X POST http://localhost:8080/api/v1/groups \
  -H 'Content-Type: application/json' \
  --data '{"name":"vehicle","label":"Vehicle"}'

curl -X POST http://localhost:8080/api/v1/groups \
  -H 'Content-Type: application/json' \
  --data '{"name":"pool","label":"Pool"}'
```

Create a record table and a time-series table from the included metadata:

```sh
curl -X POST http://localhost:8080/api/v1/groups/vehicle/tables \
  -H 'Content-Type: application/json' \
  --data-binary @examples/record-table.json

curl -X POST http://localhost:8080/api/v1/groups/pool/tables \
  -H 'Content-Type: application/json' \
  --data-binary @examples/timeseries-table.json
```

Insert records (decimal values are JSON strings):

```sh
curl -X POST http://localhost:8080/api/v1/groups/vehicle/tables/service/records \
  -H 'Content-Type: application/json' \
  --data '{"service_date":"2026-09-20","odometer_km":42000,"category":"service","cost":"320.50","notes":"Oil and filter"}'

curl -X POST http://localhost:8080/api/v1/groups/pool/tables/measurements/records \
  -H 'Content-Type: application/json' \
  --data '{"measured_at":"2026-09-20T08:00:00Z","ph":"7.4","free_chlorine":"2.1","temperature_c":"27.5"}'
```

Try an aggregate query and open the generated table views:

```sh
curl 'http://localhost:8080/api/v1/groups/vehicle/tables/service/records?aggregate=count,cost:sum'
curl 'http://localhost:8080/api/v1/groups/pool/tables/measurements/records?bucket=day&aggregate=temperature_c:avg'
```

Open `/t/vehicle/service` and `/t/pool/measurements` to use the browser table
views and their query controls.

## Publish the Markdown and browser example

Publish the Markdown source and HTML client example as pages. This uses `jq` to
JSON-encode the file contents:

```sh
jq -n --rawfile content examples/report.md \
  '{path:"/examples/report.md",content_type:"text/markdown",content:$content}' |
  curl -X POST http://localhost:8080/api/v1/pages \
    -H 'Content-Type: application/json' --data-binary @-

jq -n --rawfile content examples/client-rendering.html \
  '{path:"/examples/client-rendering.html",content_type:"text/html",content:$content}' |
  curl -X POST http://localhost:8080/api/v1/pages \
    -H 'Content-Type: application/json' --data-binary @-
```

Visit `/examples/client-rendering.html`. It fetches the Markdown source through
the page API and renders it with `/assets/clio-markdown.js`. Published HTML is
trusted executable content, so only publish HTML you control.
