#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

mkdir -p dist

OUTPUT="dist/snail_tool_linux_$(go env GOARCH)"

CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" go build -o "$OUTPUT" ./cmd/snail_tool

echo "Build completed: $OUTPUT"
