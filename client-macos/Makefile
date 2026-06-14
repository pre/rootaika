# Makefile — build, test, and release the rootaika macOS client.
#
# Thin wrappers over scripts/build.sh and scripts/release.sh so there is one
# source of truth for the build flags. Builds run on macOS only (SwiftPM cannot
# cross-compile from Linux). See README.md for the full workflow.
#
#   make build                 build dist/rootaika-mac release binary (version "dev")
#   make build VERSION=v1.2.0  build with a version label
#   make release VERSION=v1.2.0  build + publish a GitHub release (needs gh)
#   make test                  swift test
#   make                       same as build
#   make clean                 remove dist/ and .build/

VERSION ?= dev
SCRIPTS := scripts

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
	swift test

clean:
	rm -rf dist .build

help:
	@sed -n '1,12p' $(MAKEFILE_LIST)
