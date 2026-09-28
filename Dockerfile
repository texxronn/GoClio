FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/clio .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /var/lib/clio --create-home clio

COPY --from=build /out/clio /usr/local/bin/clio

ENV CLIO_ADDR=0.0.0.0:8080 \
    CLIO_DATA_DIR=/var/lib/clio \
    CLIO_DB=/var/lib/clio/clio.db \
    CLIO_BASE_URL=http://localhost:8080

VOLUME ["/var/lib/clio"]
EXPOSE 8080
USER clio
ENTRYPOINT ["/usr/local/bin/clio"]
