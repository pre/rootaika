# Makefile — build and flash the RP2040 rootaika lock button firmware.
#
#   make build        compile the sketch only (no board needed)
#   make flash        upload the last build to the board (no recompile)
#   make              build, then flash (default target)
#   make upload       alias for flash
#
# The board is the iLabs Challenger RP2040 WiFi. See README.md for the
# toolchain (arduino-cli + rp2040 core + libraries) and the required wifi.h.
#
# Overridable variables:
#   make flash PORT=/dev/ttyACM0     pin the serial port (else autodetected)
#   make build VERBOSE=1             pass --verbose to arduino-cli

SKETCH    := RootaikaButton
FQBN      := rp2040:rp2040:challenger_2040_wifi
BUILD_DIR := build

# Resolve arduino-cli from PATH, else the common Homebrew prefixes — a
# non-login shell (e.g. ssh "make build") often lacks /opt/homebrew/bin on PATH.
ARDUINO_CLI := $(shell command -v arduino-cli 2>/dev/null || \
	for p in /opt/homebrew/bin/arduino-cli /usr/local/bin/arduino-cli; do \
		[ -x "$$p" ] && { echo "$$p"; break; }; done)
PORT        ?=
VERBOSE     ?=
V_FLAG      := $(if $(VERBOSE),--verbose,)

.PHONY: all build flash upload clean help

all: build flash

build: $(SKETCH)/wifi.h
	@test -n "$(ARDUINO_CLI)" && command -v $(ARDUINO_CLI) >/dev/null 2>&1 || { \
		echo "error: arduino-cli not found on PATH or in /opt/homebrew/bin, /usr/local/bin (see README.md)" >&2; exit 1; }
	$(ARDUINO_CLI) compile $(V_FLAG) \
		--fqbn "$(FQBN)" \
		--build-path "$(BUILD_DIR)" \
		"$(SKETCH)"

# Upload the already-built binary. Does NOT recompile, so `make flash` after a
# `make build` reflashes the exact bytes you compiled.
flash: $(SKETCH)/wifi.h
	@test -n "$(ARDUINO_CLI)" && command -v $(ARDUINO_CLI) >/dev/null 2>&1 || { \
		echo "error: arduino-cli not found on PATH or in /opt/homebrew/bin, /usr/local/bin (see README.md)" >&2; exit 1; }
	@test -d "$(BUILD_DIR)" || { echo "no $(BUILD_DIR)/ yet; run 'make build' first" >&2; exit 1; }
	$(eval RESOLVED_PORT := $(if $(PORT),$(PORT),$(shell $(ARDUINO_CLI) board list 2>/dev/null | awk '/challenger|rp2040|RP2040|Pico/ {print $$1; exit}')))
	@test -n "$(RESOLVED_PORT)" || { \
		echo "error: no board port found. Plug in the board, or pass PORT=/dev/..." >&2; \
		echo "       (list candidates with: $(ARDUINO_CLI) board list)" >&2; exit 1; }
	@echo "flashing to $(RESOLVED_PORT)"
	$(ARDUINO_CLI) upload $(V_FLAG) \
		--fqbn "$(FQBN)" \
		--input-dir "$(BUILD_DIR)" \
		-p "$(RESOLVED_PORT)" \
		"$(SKETCH)"

upload: flash

$(SKETCH)/wifi.h:
	@echo "error: $(SKETCH)/wifi.h missing." >&2
	@echo "       cp $(SKETCH)/wifi.h.example $(SKETCH)/wifi.h  and fill in WiFi creds + server address." >&2
	@exit 1

clean:
	rm -rf "$(BUILD_DIR)"

help:
	@sed -n '1,14p' $(MAKEFILE_LIST)
