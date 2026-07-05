# Makefile — build, test, and release the rootaika Windows client.
#
# Thin wrappers over scripts/build.sh and scripts/release.sh so there is one
# source of truth for the build flags. See README.md for the full workflow.
#
#   make build                 cross-compile dist/rootaika.exe (version "dev")
#   make build VERSION=v1.2.0  cross-compile with a stamped version
#   make docker-build VERSION=v1.2.0  same build inside the pinned Go image
#   make docker-test           go test inside the pinned Go image
#   make release VERSION=v1.2.0  build + publish a GitHub release (needs gh)
#   make test                  go test with race detector + coverage
#   make                       same as build
#   make clean                 remove dist/

VERSION ?= dev
SCRIPTS := scripts

# The pinned build image, shared with .github/workflows/release.yml: building at
# a release tag with docker-build reproduces the released exe bit-for-bit, so its
# sha256 can be verified independently. Bump together with go.mod's Go version.
GO_IMAGE ?= golang:1.26.4-bookworm

.PHONY: all build release test clean help

all: build

build:
	$(SCRIPTS)/build.sh "$(VERSION)"

# release.sh requires a real version tag (it refuses without one); guard here so
# `make release` without VERSION fails fast with a useful message.
release:
	@test "$(VERSION)" != "dev" || { \
		echo "error: set a version, e.g. make release VERSION=v1.2.0" >&2; exit 2; }
	$(SCRIPTS)/release.sh "$(VERSION)"

test:
	go test -race -cover ./...

# Run any target inside the pinned build image: make docker-build, docker-test...
# The named volume caches Go modules between runs.
docker-%:
	docker run --rm -v "$(CURDIR)":/src -w /src -v rootaika-gomod:/go/pkg/mod \
		$(GO_IMAGE) make $* VERSION="$(VERSION)"

clean:
	rm -rf dist

help:
	@sed -n '1,14p' $(MAKEFILE_LIST)
