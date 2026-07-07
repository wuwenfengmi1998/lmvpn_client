# LMVPN Client — Makefile

APP_NAME    = LMVPN
BUNDLE_ID   = com.lmvpn.client
GUI_BIN     = lmvpn
DAEMON_BIN  = lmvpnd
BUILD_DIR   = build
APP_BUNDLE  = $(APP_NAME).app

GO          = go
CGO_ENABLED = 1
GIT_HASH    = $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION     = 0.3.5-$(GIT_HASH)
LDFLAGS     = -s -w -X lmvpn/internal/version.Version=$(VERSION)

.PHONY: all build app run daemon clean vet tidy fmt icon build-windows

## all: build the .app bundle (default)
all: app

## build: compile both the GUI and daemon binaries
build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(GUI_BIN) ./cmd/lmvpn
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(DAEMON_BIN) ./cmd/lmvpnd

## app: build binaries and assemble .app bundle
app: build
	rm -rf $(APP_BUNDLE)
	mkdir -p $(APP_BUNDLE)/Contents/MacOS
	mkdir -p $(APP_BUNDLE)/Contents/Resources
	cp $(BUILD_DIR)/$(GUI_BIN) $(APP_BUNDLE)/Contents/MacOS/$(GUI_BIN)
	cp $(BUILD_DIR)/$(DAEMON_BIN) $(APP_BUNDLE)/Contents/MacOS/$(DAEMON_BIN)
	cp resources/Info.plist $(APP_BUNDLE)/Contents/Info.plist
	@if [ -f resources/icon.icns ]; then \
		cp resources/icon.icns $(APP_BUNDLE)/Contents/Resources/icon.icns; \
	else echo "  (no icon.icns found, skipping icon)"; fi
	@echo "Built $(APP_BUNDLE)"

## icon: generate icon.icns from resources/icon.png (or resources/logo.svg)
icon:
	@if [ -f resources/logo.svg ] && [ ! -f resources/icon.png -o resources/logo.svg -nt resources/icon.png ]; then \
		echo "Converting resources/logo.svg -> resources/icon.png"; \
		sips -z 1024 1024 -s format png resources/logo.svg --out resources/icon.png >/dev/null 2>&1; \
	fi
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
	./$(BUILD_DIR)/$(GUI_BIN)

## daemon: run the daemon directly (needs root)
daemon: build
	sudo ./$(BUILD_DIR)/$(DAEMON_BIN)

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

## build-windows: cross-compile Windows x86_64 exes (requires mingw-w64)
build-windows:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		$(GO) build -ldflags "$(LDFLAGS) -H windowsgui" -o $(BUILD_DIR)/$(GUI_BIN).exe ./cmd/lmvpn
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(DAEMON_BIN).exe ./cmd/lmvpnd
	@echo "Built $(BUILD_DIR)/$(GUI_BIN).exe and $(BUILD_DIR)/$(DAEMON_BIN).exe"
