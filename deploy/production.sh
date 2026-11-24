#!/usr/bin/env bash
# Deploys origin/main to production on the VPS (ssh host "vps", root): https://thecaveacademy.com/. See docs/deploy.md.
# Usage: deploy/production.sh            (from a clean checkout at origin/main's HEAD)
#        deploy/production.sh rollback   (put the previous binary back)
set -euo pipefail
cd "$(dirname "$0")/.."

bin=/usr/local/bin/cave-academy
url=https://thecaveacademy.com

rollback() {
	ssh vps bash -s -- "$bin" <<'REMOTE'
set -euo pipefail
bin=$1
[ -f "$bin.prev" ] || { echo "no previous binary at $bin.prev" >&2; exit 1; }
install -m 755 "$bin.prev" "$bin"
systemctl restart cave-academy
REMOTE
}

if [ "${1:-}" = rollback ]; then
	rollback
	ssh vps 'echo "unknown (rolled back to .prev)" > /var/lib/cave-academy/deployed'
	echo "rolled back to the previous binary"
	exit 0
fi

[ -z "$(git status --porcelain)" ] || { echo "working tree not clean" >&2; exit 1; }
git fetch -q origin main
commit=$(git rev-parse HEAD)
[ "$commit" = "$(git rev-parse origin/main)" ] || { echo "HEAD is not origin/main's HEAD; check out and pull main" >&2; exit 1; }

./test.sh

# First: the service user, directories and env file, before anything is installed.
rc=0
ssh vps bash -s <<'REMOTE' || rc=$?
set -euo pipefail
id cave_academy >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin cave_academy
install -d -m 755 /etc/cave-academy
install -d -m 700 -o cave_academy -g cave_academy /var/backups/cave-academy
if [ ! -f /etc/cave-academy/env ]; then
	install -m 600 /dev/null /etc/cave-academy/env
	cat > /etc/cave-academy/env <<'ENV'
OPENROUTER_API_KEY=
OPENROUTER_MODEL=openai/gpt-6-luna
BREVO_API_KEY=
MAIL_FROM=The Cave Academy <noreply@thecaveacademy.com>
ADMIN_EMAIL=adam.caveconsult@gmail.com
BASE_URL=https://thecaveacademy.com
ENV
	exit 3
fi
for k in OPENROUTER_API_KEY BREVO_API_KEY BASE_URL; do
	grep -q "^$k=." /etc/cave-academy/env || { echo "$k is empty in /etc/cave-academy/env" >&2; exit 4; }
done
REMOTE
if [ "$rc" = 3 ]; then
	echo "Created /etc/cave-academy/env on the VPS (mode 600). Fill in OPENROUTER_API_KEY, OPENROUTER_MODEL, BREVO_API_KEY," >&2
	echo "MAIL_FROM, ADMIN_EMAIL and BASE_URL (ssh vps, edit it), then run this again." >&2
	exit 1
fi
[ "$rc" = 0 ] || exit "$rc"

# ponytail: main.go has no version variable yet; add `-X main.version=$commit` to -ldflags when it does.
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$build/cave-academy" .
stage=$(ssh vps mktemp -d)
scp -q "$build/cave-academy" deploy/cave-academy.service deploy/cave-academy.caddy \
	deploy/cave-academy-backup.sh deploy/cave-academy-backup.service deploy/cave-academy-backup.timer "vps:$stage/"

rc=0
ssh vps bash -s -- "$stage" "$bin" <<'REMOTE' || rc=$?
set -euo pipefail
stage=$1
bin=$2
site=/etc/caddy/sites/cave-academy.caddy
[ ! -f "$bin" ] || cp -p "$bin" "$bin.prev"
install -m 755 "$stage/cave-academy" "$bin"
install -m 755 "$stage/cave-academy-backup.sh" /usr/local/bin/cave-academy-backup
install -m 644 "$stage/cave-academy.service" "$stage/cave-academy-backup.service" "$stage/cave-academy-backup.timer" /etc/systemd/system/
systemctl daemon-reload
systemctl enable -q cave-academy
systemctl enable -q --now cave-academy-backup.timer
systemctl restart cave-academy
sleep 2
curl -fsS -o /dev/null http://127.0.0.1:8097/

[ ! -f "$site" ] || cp -p "$site" "$site.prev"
install -m 644 "$stage/cave-academy.caddy" "$site"
if ! caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; then
	echo "caddy validate failed; keeping the old site file" >&2
	if [ -f "$site.prev" ]; then mv "$site.prev" "$site"; else rm -f "$site"; fi
	exit 1
fi
systemctl reload caddy
rm -rf "$stage"
REMOTE

smoke() {
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		if [ "$(curl -s -m 10 -o /dev/null -w '%{http_code}' "$url/")" = 200 ] &&
			[ "$(curl -s -m 10 -o /dev/null -w '%{http_code}' "$url/static/css/base.css")" = 200 ]; then
			return 0
		fi
		sleep 3
	done
	return 1
}

if [ "$rc" = 0 ] && smoke; then
	# shellcheck disable=SC2029 # $commit expands here on purpose
	ssh vps "echo $commit > /var/lib/cave-academy/deployed"
	echo "production: $url at $commit"
	exit 0
fi
echo "deploy failed (install exit $rc or smoke test); rolling back" >&2
rollback || echo "rollback failed: no previous binary, service left as is" >&2
exit 1
