ARDUINO_CLI ?= arduino-cli
SKETCH_DIR := RootaikaBoard
BUILD_DIR ?= build
FQBN ?= esp32:esp32:esp32s3:CDCOnBoot=cdc,PSRAM=opi,FlashSize=8M
PORT ?= $(shell $(ARDUINO_CLI) board list 2>/dev/null | awk 'NR > 1 && ($$1 ~ /^\/dev\/(cu|tty)\.usb/ || $$1 ~ /^\/dev\/tty(ACM|USB)/ || $$1 ~ /^COM[0-9]+/) { print $$1; exit }')

.DEFAULT_GOAL := all
.PHONY: all build flash clean help require-port

all: build
	$(MAKE) flash

build:
	$(ARDUINO_CLI) compile --fqbn "$(FQBN)" --build-path "$(BUILD_DIR)" "$(SKETCH_DIR)"

flash: require-port
	$(ARDUINO_CLI) upload -p "$(PORT)" --fqbn "$(FQBN)" --build-path "$(BUILD_DIR)" "$(SKETCH_DIR)"

clean:
	rm -rf "$(BUILD_DIR)"

help:
	@printf '%s\n' \
		'Usage:' \
		'  make                         Build and flash' \
		'  make build                   Build firmware only' \
		'  make flash                   Flash previously built firmware' \
		'  make clean                   Remove build artifacts' \
		'  make help                    Show this help' \
		'' \
		'Options:' \
		'  PORT=/dev/cu.usbmodem1101    Override detected upload port' \
		'  FQBN=esp32:esp32:esp32s3...  Override board target' \
		'  BUILD_DIR=build              Override build artifact directory'

require-port:
	@if [ -z "$(PORT)" ]; then \
		echo "No USB serial port found."; \
		echo "Run 'arduino-cli board list' and retry with PORT=/dev/cu.usbmodemXXXX make flash."; \
		exit 1; \
	fi
