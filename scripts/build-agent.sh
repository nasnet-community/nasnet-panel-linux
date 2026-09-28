#!/usr/bin/env bash
# Builds the remote node agent for the architectures the panel can deploy.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
mkdir -p bin/agent
for arch in amd64 arm64; do
	GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-w -s -X main.version=$VERSION" \
		-o "bin/agent/nasnet-agent-linux-$arch" ./cmd/node-agent
done
printf '%s\n' "$VERSION" > bin/agent/VERSION
echo "built nasnet-agent $VERSION"
