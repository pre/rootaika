# Makefile — build, test, and release the rootaika Windows client.
#
# Thin wrappers over the scripts/ shell scripts so there is one source of truth
# for the build flags. See README.md for the full workflow.
#
#   make build                 cross-compile dist/rootaika.exe (version "dev")
#   make build VERSION=v1.2.0  cross-compile with a stamped version
#   make docker-build VERSION=v1.2.0  same build inside the pinned Go image
#   make docker-test           go test inside the pinned Go image
#   make github-release VERSION=v1.2.3  draft a GitHub release with generated notes
#   make test                  go test with race detector + coverage
#   make                       same as build
#   make clean                 remove dist/

VERSION ?= dev
SCRIPTS := scripts

# The pinned build image, shared with .github/workflows/release.yml: building at
# a release tag with docker-build reproduces the released exe bit-for-bit, so its
# sha256 can be verified independently. Bump together with go.mod's Go version.
GO_IMAGE ?= golang:1.26.4-bookworm

.PHONY: all build github-release test clean help

all: build

build:
	$(SCRIPTS)/build.sh "$(VERSION)"

# Creates a DRAFT release with notes generated from git history; publishing it in
# the browser creates the tag and triggers the build workflow. Guard VERSION so
# the target fails fast with a useful message when it is missing.
github-release:
	@test "$(VERSION)" != "dev" || { \
		echo "error: set a version, e.g. make github-release VERSION=v1.2.3" >&2; exit 2; }
	$(SCRIPTS)/github-release.sh "$(VERSION)"

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
	@sed -n '1,13p' $(MAKEFILE_LIST)
