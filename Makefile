.PHONY: build clean test iso iso-prep

BINARY_NAME := backup-agent
BUILD_DIR := ./build
AGENT_SRC := ./cmd/backup-agent
ISO_AGENT_PATH := ./archlive/airootfs/usr/local/bin/$(BINARY_NAME)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	@echo "==> Building $(BINARY_NAME)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(AGENT_SRC)
	@echo "==> Built: $(BUILD_DIR)/$(BINARY_NAME)"

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(ISO_AGENT_PATH)
	rm -rf ./archlive/out

test:
	go test ./...

# Build and copy the binary into the archlive airootfs for ISO creation.
iso-prep: build
	@echo "==> Copying agent binary to archlive..."
	cp $(BUILD_DIR)/$(BINARY_NAME) $(ISO_AGENT_PATH)
	chmod +x $(ISO_AGENT_PATH)
	@echo "==> Ready. Run 'sudo ./archlive/build.sh' to create the ISO."

# Full ISO build (requires root and archiso installed).
iso: iso-prep
	sudo ./archlive/build.sh
