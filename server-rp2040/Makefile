# Makefile — build and flash the RP2040 rootaika server firmware.
#
# Build and flash are SEPARATE actions; run either or both:
#   make build        compile the sketch only (no board needed)
#   make flash        upload the last build to the board (no recompile)
#   make              build, then flash (default target)
#   make upload       alias for flash
#
# The board is the iLabs Challenger RP2040 WiFi with a LittleFS partition.
# See README.md for the toolchain (arduino-cli + rp2040 core + libraries) and
# the required wifi.h. seed.py / reset.py handle the LittleFS data image; this
# Makefile only deals with the firmware.
#
# Overridable variables:
#   make flash PORT=/dev/ttyACM0     pin the serial port (else autodetected)
#   make FS=2097152                  pick a different LittleFS size in bytes
#   make build VERBOSE=1             pass --verbose to arduino-cli

SKETCH    := RootaikaServer
FLASH     := 8388608            # total flash addressed by the layout (8 MB, see README)
FS        := 4194304            # LittleFS partition size in bytes (4 MB, see README)
FQBN      := rp2040:rp2040:challenger_2040_wifi:flash=$(FLASH)_$(FS)
BUILD_DIR := build

ARDUINO_CLI := arduino-cli
PORT        ?=
VERBOSE     ?=
V_FLAG      := $(if $(VERBOSE),--verbose,)

.PHONY: all build flash upload clean help

all: build flash

build: $(SKETCH)/wifi.h
	@command -v $(ARDUINO_CLI) >/dev/null 2>&1 || { \
		echo "error: arduino-cli not found in PATH (see README.md)" >&2; exit 1; }
	$(ARDUINO_CLI) compile $(V_FLAG) \
		--fqbn "$(FQBN)" \
		--build-path "$(BUILD_DIR)" \
		"$(SKETCH)"

# Upload the already-built binary. Does NOT recompile, so `make flash` after a
# `make build` reflashes the exact bytes you compiled. Falls back to building if
# nothing has been built yet.
flash: $(SKETCH)/wifi.h
	@command -v $(ARDUINO_CLI) >/dev/null 2>&1 || { \
		echo "error: arduino-cli not found in PATH (see README.md)" >&2; exit 1; }
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
	@echo "       cp $(SKETCH)/wifi.h.example $(SKETCH)/wifi.h  and fill in WiFi creds." >&2
	@exit 1

clean:
	rm -rf "$(BUILD_DIR)"

help:
	@sed -n '1,30p' $(MAKEFILE_LIST)
