#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

mkdir -p dist

OUTPUT="dist/snailtool_linux_$(go env GOARCH)"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X snail_tool/internal/version.Version=$VERSION -X snail_tool/internal/version.Commit=$COMMIT -X snail_tool/internal/version.BuildDate=$BUILD_DATE"

CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" go build -ldflags="$LDFLAGS" -o "$OUTPUT" ./cmd/snail_tool

echo "Build completed: $OUTPUT"
