#!/usr/bin/env bash

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "Run this script with sudo from the repository directory." >&2
  exit 1
fi

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$SOURCE_DIR"
GOTOOLCHAIN=auto go mod download
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go build -trimpath -ldflags="-s -w" \
  -o /usr/local/bin/url-shortener.new ./cmd/server

chmod 0755 /usr/local/bin/url-shortener.new
mv /usr/local/bin/url-shortener.new /usr/local/bin/url-shortener
systemctl restart url-shortener
systemctl --no-pager --full status url-shortener
