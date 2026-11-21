#!/usr/bin/env bash
# Deploys the current commit to staging on the VPS (ssh host "vps", root).
# Usage: deploy/staging.sh   (from a clean, pushed checkout)
set -euo pipefail
cd "$(dirname "$0")/.."
[ -z "$(git status --porcelain)" ] || { echo "working tree not clean" >&2; exit 1; }
commit=$(git rev-parse HEAD)
git branch -r --contains "$commit" | grep -q . || { echo "commit not pushed" >&2; exit 1; }

./test.sh >/dev/null
bin=$(mktemp)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$bin" .
scp -q "$bin" vps:/tmp/cave-academy-staging.new
scp -q deploy/cave-academy-staging.service deploy/cave-academy-staging.caddy vps:/tmp/
rm -f "$bin"

ssh vps "set -e
id cave_academy_staging >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin cave_academy_staging
mkdir -p /etc/cave-academy
[ -f /etc/cave-academy/staging.env ] || install -m 600 /dev/null /etc/cave-academy/staging.env
install -m 755 /tmp/cave-academy-staging.new /usr/local/bin/cave-academy-staging
install -m 644 /tmp/cave-academy-staging.service /etc/systemd/system/cave-academy-staging.service
install -m 644 /tmp/cave-academy-staging.caddy /etc/caddy/sites/cave-academy-staging.caddy
rm -f /tmp/cave-academy-staging.new /tmp/cave-academy-staging.service /tmp/cave-academy-staging.caddy
systemctl daemon-reload
systemctl enable -q cave-academy-staging
systemctl restart cave-academy-staging
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1
systemctl reload caddy
sleep 2
curl -fsS -o /dev/null http://127.0.0.1:8096/
echo $commit > /var/lib/cave-academy-staging/deployed"

code=$(curl -s -o /dev/null -w '%{http_code}' https://cave-academy.13-140-65-183.sslip.io/)
[ "$code" = 401 ] || { echo "smoke: expected 401 from the staging login, got $code" >&2; exit 1; }
echo "staging: https://cave-academy.13-140-65-183.sslip.io/ at $commit"
