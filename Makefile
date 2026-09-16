BINARY_NAME=avdslim
BUILD_DIR=bin
VERSION=1.0.5
LDFLAGS=-s -w -X main.version=$(VERSION)

.PHONY: all build clean cross install test

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/avdslim
	@echo "✓ Built $(BUILD_DIR)/$(BINARY_NAME)"

cross:
	@mkdir -p $(BUILD_DIR)
	@echo "--> Building macOS ARM64..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/avdslim
	@echo "--> Building macOS Intel..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/avdslim
	@echo "--> Building Linux x86_64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/avdslim
	@echo "--> Building Windows x86_64..."
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/avdslim
	@echo "✓ Cross-compilation complete! Artifacts in $(BUILD_DIR)/"

# PREFIX may be overridden: make install PREFIX=/opt/homebrew/bin
PREFIX ?= $(HOME)/.local/bin

# The previous version chained three `cp` attempts and then echoed
# "✓ Installed" on a separate line, so it reported success even when every copy
# had failed and the binary was nowhere on PATH. It also never created the target
# directory, so the first cp failed on any machine without ~/.local/bin.
install: build
	@mkdir -p "$(PREFIX)"
	@install -m 0755 $(BUILD_DIR)/$(BINARY_NAME) "$(PREFIX)/$(BINARY_NAME)" || { \
		echo "✗ Could not install to $(PREFIX)."; \
		echo "  Pick a writable directory on your PATH, e.g.:"; \
		echo "    make install PREFIX=/opt/homebrew/bin"; \
		exit 1; \
	}
	@echo "✓ Installed $(BINARY_NAME) to $(PREFIX)/$(BINARY_NAME)"
	@case ":$$PATH:" in \
		*":$(PREFIX):"*) ;; \
		*) echo "⚠️  $(PREFIX) is not on your PATH — add it to your shell profile." ;; \
	esac

clean:
	rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned $(BUILD_DIR)"

test:
	go test ./...
