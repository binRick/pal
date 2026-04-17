#!/usr/bin/env bash
set -euo pipefail

IMAGE="golang:1.21-alpine"
OUTPUT="pal"

echo "🐳 Building static pal binary via Docker..."

docker run --rm \
  -v "$(pwd)":/src \
  -w /src \
  -e CGO_ENABLED=0 \
  -e GOOS=linux \
  -e GOARCH=amd64 \
  "$IMAGE" \
  go build \
    -trimpath \
    -ldflags="-s -w -extldflags=-static" \
    -o "$OUTPUT" \
    .

echo "✅ Built: $(pwd)/$OUTPUT"
echo "   Size:  $(du -sh "$OUTPUT" | cut -f1)"
file "$OUTPUT"
