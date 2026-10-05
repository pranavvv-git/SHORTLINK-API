#!/usr/bin/env bash

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "Run this script with sudo." >&2
  exit 1
fi

if [[ $# -ne 1 ]]; then
  echo "Usage: sudo bash deploy/ec2/install.sh http://YOUR_PUBLIC_IP" >&2
  echo "For a domain with HTTPS, pass https://short.example.com." >&2
  exit 1
fi

BASE_URL="${1%/}"
SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APP_USER="urlshortener"
DATA_DIR="/var/lib/url-shortener"
ENV_FILE="/etc/url-shortener.env"

if [[ ! "$BASE_URL" =~ ^https?://[^/]+$ ]]; then
  echo "BASE_URL must look like http://1.2.3.4 or https://short.example.com." >&2
  exit 1
fi

dnf install -y git golang nginx

if ! id "$APP_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --shell /sbin/nologin "$APP_USER"
fi

install -d -o "$APP_USER" -g "$APP_USER" -m 0750 "$DATA_DIR"

cd "$SOURCE_DIR"
GOTOOLCHAIN=auto go mod download
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go build -trimpath -ldflags="-s -w" \
  -o /usr/local/bin/url-shortener ./cmd/server
chmod 0755 /usr/local/bin/url-shortener

cat >"$ENV_FILE" <<EOF
HTTP_ADDR=127.0.0.1:8080
BASE_URL=$BASE_URL
DATABASE_PATH=$DATA_DIR/urls.db
CODE_LENGTH=7
REQUEST_TIMEOUT=5s
SHUTDOWN_TIMEOUT=10s
MAX_BODY_BYTES=1048576
EOF
chmod 0640 "$ENV_FILE"
chown root:"$APP_USER" "$ENV_FILE"

install -m 0644 "$SOURCE_DIR/deploy/ec2/url-shortener.service" \
  /etc/systemd/system/url-shortener.service
if [[ ! -f /etc/nginx/nginx.conf.before-url-shortener ]]; then
  cp /etc/nginx/nginx.conf /etc/nginx/nginx.conf.before-url-shortener
fi
install -m 0644 "$SOURCE_DIR/deploy/ec2/nginx-main.conf" \
  /etc/nginx/nginx.conf
install -m 0644 "$SOURCE_DIR/deploy/ec2/nginx.conf" \
  /etc/nginx/conf.d/url-shortener.conf

# Amazon Linux enables SELinux policies that can block an Nginx reverse proxy.
if command -v setsebool >/dev/null 2>&1; then
  setsebool -P httpd_can_network_connect 1
fi

systemctl daemon-reload
systemctl enable --now url-shortener
nginx -t
systemctl enable --now nginx
systemctl reload nginx

systemctl --no-pager --full status url-shortener
echo
echo "Deployment complete: $BASE_URL"
