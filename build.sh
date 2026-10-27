#!/bin/sh
# Cross-compiles OfflineSim.exe for 64-bit Windows.
set -e
cd "$(dirname "$0")"
VERSION=${VERSION:-1.0.0}
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -H windowsgui -X main.appVersion=$VERSION" \
  -o dist/OfflineSim.exe .
echo "built dist/OfflineSim.exe"
