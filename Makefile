# GoClio build and verification.

# Release builds should report the tag they were built from. The leading "v"
# of a SemVer tag is stripped so /api/v1/health reports "1.0.1", matching the
# product version format documented in SPEC.md. Working-tree builds fall back
# to the commit hash (with -dirty when applicable).
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
ifeq ($(VERSION),)
VERSION := 1.0.0
endif
VERSION := $(patsubst v%,%,$(VERSION))

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet check

# Build ./clio with the version derived from the git tag.
build:
	CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags="$(LDFLAGS)" -o clio .

test:
	go test ./...

vet:
	go vet ./...

# The full verification sequence referenced by AGENTS.md.
check: test vet
	CGO_ENABLED=1 go build -buildvcs=false -ldflags="$(LDFLAGS)" -o /tmp/gocl-clio-check .
