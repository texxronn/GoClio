# Run GoClio on a home lab

GoClio runs in Docker and stores its SQLite database and published content in
one persistent Docker volume. The Compose configuration is in
[`docker-compose.yml`](docker-compose.yml).

## Get the image onto the home-lab host

There is not currently a published GoClio image registry configured for this
repository. The simplest option is to clone the repository on the Docker host
and let Compose build the image there:

```sh
git clone https://github.com/texxronn/GoClio.git
cd GoClio
docker compose build
```

Alternatively, build it on a machine with Docker, save and transfer it, then
load it on the home-lab host. Run these commands from the repository root:

```sh
docker build -t gocl.io:local .
docker save gocl.io:local | gzip > gocl.io.tar.gz
scp gocl.io.tar.gz USER@HOME_LAB_HOST:/tmp/
```

On the home-lab host:

```sh
gunzip -c /tmp/gocl.io.tar.gz | docker load
```

Keep the image tag `gocl.io:local`, which is the tag Compose expects. For
future updates, pull the latest source (or copy a newly built image), rebuild
or load it, then run `docker compose up -d` again.

## Configure and start

On the home-lab host, create a `.env` file next to `docker-compose.yml`:

```dotenv
CLIO_BASE_URL=http://clio.home:8080
```

Change this to the URL clients will use. If you enable authentication, do not
expose the service directly over plain HTTP. Use a trusted TLS reverse proxy;
configure proxy trust only for the proxy's immediate network address range.
Set `CLIO_AUTH_PASSWORD_HASH` to a bcrypt hash (never a plaintext password):

```dotenv
CLIO_BASE_URL=https://clio.example.net
CLIO_AUTH_ENABLED=true
CLIO_AUTH_USER=admin
CLIO_AUTH_PASSWORD_HASH='$2a$...'
CLIO_REQUIRE_HTTPS=true
CLIO_TRUST_PROXY=true
CLIO_TRUSTED_PROXY_NETWORKS=10.10.0.0/24
```

For large scanned PDFs and lossless audio (`.flac`/`.wav`), raise the upload
limit above the 100 MiB default:

```dotenv
CLIO_MAX_UPLOAD_BYTES=1073741824
```

A reverse proxy in front of GoClio enforces its own request-body limit; raise it
too, for example nginx `client_max_body_size 1g;`, or the proxy rejects a large
upload before it reaches GoClio.

Generate the hash with the GoClio `clio hash-password` command. For example,
after loading the image, it can be run in a temporary container:

```sh
docker run --rm -it --entrypoint clio gocl.io:local hash-password
```

Protect the `.env` file (`chmod 600 .env`) and do not commit it. Start GoClio:

```sh
docker compose up -d
docker compose ps
docker compose logs -f clio
```

The service listens on port `8080` on the Docker host. Check
`http://HOME_LAB_HOST:8080/health` (or the configured HTTPS URL through your
proxy). Allow that port through the host firewall only if it should be
reachable from your LAN. The Compose volume `clio-data` holds both the
database and published content and survives container replacement.

## Stop, update, and back up

Stop the service without deleting data:

```sh
docker compose down
```

After updating the source or loading a new image, restart it with:

```sh
docker compose up -d --build
```

For a consistent backup, stop GoClio first, then archive the persistent
volume. Replace `gocl-data-backup.tar.gz` with the desired host backup path:

```sh
docker compose down
docker run --rm \
  -v GoClio_clio-data:/data:ro \
  -v "$PWD":/backup \
  debian:bookworm-slim \
  tar -czf /backup/gocl-data-backup.tar.gz -C /data .
```

Compose prefixes volume names with the project name; if the directory is not
named `GoClio`, find the actual volume name with `docker volume ls` and use it
instead of `GoClio_clio-data`.
