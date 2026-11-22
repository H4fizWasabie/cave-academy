#!/usr/bin/env bash
# Daily consistent copy of the SQLite database, 14 days kept. Installed to /usr/local/bin/cave-academy-backup by
# deploy/production.sh and run by cave-academy-backup.timer. See docs/deploy.md, Backups.
set -euo pipefail
db=/var/lib/cave-academy/cave-academy.db
dir=/var/backups/cave-academy
out="$dir/cave-academy-$(date +%F).db"
trap 'rm -f "$out.tmp"' EXIT

# .backup is safe while the app writes, unlike cp.
sqlite3 "$db" ".backup '$out.tmp'"
[ "$(sqlite3 "$out.tmp" 'PRAGMA integrity_check')" = ok ] || { echo "backup failed the integrity check" >&2; exit 1; }
mv "$out.tmp" "$out"
find "$dir" -name 'cave-academy-*.db' -mtime +14 -delete
echo "backup done: $out"
