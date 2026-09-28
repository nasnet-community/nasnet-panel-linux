.PHONY: build-tool build-agent deps-guard proto tool web build geofiles

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -ldflags "-X github.com/nasnet-community/nasnet-panel-linux/internal/agent/server.Version=$(VERSION) \
	-X github.com/nasnet-community/nasnet-panel-linux/internal/agent/server.Commit=$(COMMIT) \
	-X github.com/nasnet-community/nasnet-panel-linux/internal/agent/server.BuildTime=$(BUILD_TIME)"

GEOFILES_DIR = pkg/geofiles/embedded
IRAN_GEOIP_URL = https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat
IRAN_GEOSITE_URL = https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat

proto:
	@echo "Generating protobuf code..."
	cd proto && protoc --go_out=../pkg/agent/pb --go_opt=paths=source_relative \
		--go-grpc_out=../pkg/agent/pb --go-grpc_opt=paths=source_relative \
		node_agent.proto
	mkdir -p pkg/hub/pb
	cd proto && protoc --go_out=../pkg/hub/pb --go_opt=paths=source_relative \
		--go-grpc_out=../pkg/hub/pb --go-grpc_opt=paths=source_relative \
		agent_hub.proto
	@echo "Proto generation complete."

geofiles:
	@mkdir -p $(GEOFILES_DIR)
	@if [ ! -f $(GEOFILES_DIR)/geoip.dat ] || [ ! -f $(GEOFILES_DIR)/geosite.dat ]; then \
		echo "Downloading Iran geofiles..."; \
		curl -fSL -o $(GEOFILES_DIR)/geoip.dat "$(IRAN_GEOIP_URL)"; \
		curl -fSL -o $(GEOFILES_DIR)/geosite.dat "$(IRAN_GEOSITE_URL)"; \
		echo "Geofiles downloaded."; \
	else \
		echo "Geofiles already present, skipping download."; \
	fi

build-tool:
	go build -o bin/nasnet-tool ./cmd/nasnet-tool

tool:
	@./nasnet-tool.sh

web:
	cd web-panel && pnpm install && pnpm build

build: web geofiles
	go build -o main .

build-agent:
	./scripts/build-agent.sh

# The panel must never link xray-knife's pkg/http or database (second sqlite
# driver panics at start); the agent must never link panel-only packages.
deps-guard:
	@if go list -deps . | grep -E 'xray-knife/v11/(pkg/http|database)$$'; then \
		echo "deps guard: xray-knife pkg/http or database linked into the panel"; exit 1; fi
	@if go list -deps ./cmd/node-agent | grep -E 'nasnet-panel-linux/(internal/network|internal/node|pkg/database)(/|$$)|xray-knife/v11/(pkg/http|database)$$'; then \
		echo "deps guard: panel-only package linked into nasnet-agent"; exit 1; fi
	@echo "deps guard: ok"
