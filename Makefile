# LMVPN Client — Makefile

APP_NAME    = LMVPN
BUNDLE_ID   = com.lmvpn.client
BINARY      = lmvpn
BUILD_DIR   = build
APP_BUNDLE  = $(APP_NAME).app

GO          = go
CGO_ENABLED = 1
LDFLAGS     = -s -w

.PHONY: all build app run daemon clean vet tidy fmt

## all: build the .app bundle (default)
all: app

## build: compile the binary
build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .

## app: build binary and assemble .app bundle
app: build
	rm -rf $(APP_BUNDLE)
	mkdir -p $(APP_BUNDLE)/Contents/MacOS
	mkdir -p $(APP_BUNDLE)/Contents/Resources
	cp $(BUILD_DIR)/$(BINARY) $(APP_BUNDLE)/Contents/MacOS/$(BINARY)
	cp resources/Info.plist $(APP_BUNDLE)/Contents/Info.plist
	@if [ -f resources/icon.icns ]; then \
		cp resources/icon.icns $(APP_BUNDLE)/Contents/Resources/icon.icns; \
	else echo "  (no icon.icns found, skipping icon)"; fi
	@echo "Built $(APP_BUNDLE)"

## icon: generate icon.icns from resources/icon.png
icon:
	@if [ ! -f resources/icon.png ]; then echo "resources/icon.png not found"; exit 1; fi
	mkdir -p resources/icon.iconset
	@for size in 16 32 64 128 256 512 1024; do \
		sips -z $$size $$size resources/icon.png --out resources/icon.iconset/icon_$${size}x$${size}.png >/dev/null 2>&1; \
	done
	@for pair in "16 32" "32 64" "128 256" "256 512"; do \
		set -- $$pair; \
		sips -z $$2 $$2 resources/icon.png --out resources/icon.iconset/icon_$${1}x$${1}@2x.png >/dev/null 2>&1; \
	done
	cp resources/icon.png resources/icon.iconset/icon_512x512@2x.png
	iconutil -c icns resources/icon.iconset -o resources/icon.icns
	rm -rf resources/icon.iconset
	@echo "Generated resources/icon.icns"

## run: build and run the GUI
run: build
	./$(BUILD_DIR)/$(BINARY)

## daemon: run the daemon (needs root)
daemon: build
	sudo ./$(BUILD_DIR)/$(BINARY) daemon

## vet: run go vet
vet:
	$(GO) vet ./...

## tidy: run go mod tidy
tidy:
	$(GO) mod tidy

## fmt: format Go code
fmt:
	$(GO) fmt ./...

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR) $(APP_BUNDLE)
