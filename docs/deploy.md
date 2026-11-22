# Deploy runbook

Production is https://caveacademy.my/, on the same VPS as cave LMS (ssh host `vps`, root): one Go binary `/usr/local/bin/cave-academy` under systemd (`cave-academy`, port 8097), Caddy in front, SQLite at `/var/lib/cave-academy/cave-academy.db`. Staging is separate (`deploy/staging.sh`, see README).

| What | Where |
| --- | --- |
| Binary, previous binary | `/usr/local/bin/cave-academy`, `.prev` |
| Env file (secrets, mode 600) | `/etc/cave-academy/env` |
| Database | `/var/lib/cave-academy/cave-academy.db` |
| Deployed commit | `/var/lib/cave-academy/deployed` |
| Caddy site | `/etc/caddy/sites/cave-academy.caddy` |
| Backups | `/var/backups/cave-academy/cave-academy-YYYY-MM-DD.db` (14 days) |

## One-time setup

1. **DNS.** A records for `caveacademy.my` and `www.caveacademy.my` to `13.140.65.183`, at whoever hosts the DNS. In Cloudflare set them to DNS only (grey cloud): Caddy gets its certificates from Let's Encrypt and needs to reach the VPS directly. Do this before the first deploy; without it Caddy cannot get a certificate and the smoke test fails.
2. **Brevo domain authentication** (so mail from `noreply@caveacademy.my` is not marked as spam). In Brevo, Senders, Domains & Dedicated IPs, add `caveacademy.my` and create the records it shows: the `brevo-code` TXT, the two DKIM CNAMEs (`brevo1._domainkey`, `brevo2._domainkey`) and the DMARC TXT, with the proxy off (DNS only). Click Authenticate until it is green. Create a transactional API key (Settings, SMTP & API) for `BREVO_API_KEY`.
3. **`sqlite3` and `restic`** are already on the VPS (`which sqlite3 restic`); nothing to install.
4. **Env file.** The first `deploy/production.sh` creates `/etc/cave-academy/env` (mode 600), prints a reminder and stops. Edit it on the VPS (`ssh vps`, `nano /etc/cave-academy/env`) and fill in:

   | Key | Value |
   | --- | --- |
   | `OPENROUTER_API_KEY` | from openrouter.ai, required |
   | `OPENROUTER_MODEL` | model id, e.g. `openai/gpt-6-luna` |
   | `BREVO_API_KEY` | the Brevo key from step 2, required (empty means email only goes to the log) |
   | `MAIL_FROM` | `The Cave Academy <noreply@caveacademy.my>` |
   | `ADMIN_EMAIL` | receives form submissions |
   | `BASE_URL` | `https://caveacademy.my`, required |

   Later deploys refuse to run while `OPENROUTER_API_KEY`, `BREVO_API_KEY` or `BASE_URL` is empty, and the unit sets `APP_ENV=production`, so the app itself refuses to start without them (or with an unparseable `MAIL_FROM`). Optional: `CHAT_DAILY_CAP` (default 2000) caps OpenRouter answers per day across all users; `INTRO_VIDEO_URL` (default empty) is the intro video the "(2 Minit)" hero buttons open, and with it empty those buttons are hidden. `GET /healthz` returns `ok` when the database answers; the app shuts down gracefully on SIGTERM.
5. **First deploy**, then **first admin.** After `deploy/production.sh` has run:

   ```bash
   ssh vps 'runuser -u cave_academy -- env DB_PATH=/var/lib/cave-academy/cave-academy.db /usr/local/bin/cave-academy make-admin you@example.com'
   ```

   `make-admin <email>` is being added by another change; check its usage in the README once it lands. Run it as `cave_academy` (not root) so the database file keeps its owner.
6. **Restic (optional, recommended).** See Backups below.

## Operations

### Deploy

```bash
git checkout main && git pull --ff-only
deploy/production.sh
```

It refuses a dirty tree and anything that is not `origin/main`'s HEAD, runs `./test.sh`, builds a linux/amd64 binary, keeps the old one as `cave-academy.prev`, installs the binary, systemd units and Caddy site, validates Caddy before reloading it, then checks `https://caveacademy.my/` and `/static/css/base.css` for 200. If the install or the smoke test fails it puts `.prev` back and exits non-zero. On success it writes the commit to `/var/lib/cave-academy/deployed`.

Deploy to staging first (`deploy/staging.sh`) when a change needs a real run.

### Rollback

```bash
deploy/production.sh rollback
```

Restores `cave-academy.prev` and restarts. It does not undo database changes: restore a backup for that (below). Only one step back exists; a second rollback in a row restores the binary that was just replaced.

### Logs and status

```bash
ssh vps journalctl -u cave-academy -f          # app log; without BREVO_API_KEY form emails show up here
ssh vps systemctl status cave-academy
ssh vps cat /var/lib/cave-academy/deployed     # the running commit
```

### Backups

`cave-academy-backup.timer` runs `/usr/local/bin/cave-academy-backup` daily at 03:30 (VPS time, after the cave LMS backup at 03:00). It makes a consistent copy with `sqlite3 .backup`, checks it with `PRAGMA integrity_check`, writes `/var/backups/cave-academy/cave-academy-YYYY-MM-DD.db` and deletes copies older than 14 days.

```bash
ssh vps systemctl list-timers cave-academy-backup.timer
ssh vps systemctl start cave-academy-backup.service   # run one now
ssh vps ls -l /var/backups/cave-academy
```

These copies sit on the same disk as the database, so they do not survive losing the VPS. cave LMS already ships its backups off the VPS with restic (`/usr/local/bin/cave-lms-backup`, a local repository and one in R2, installed by the cave LMS repo's `deploy/deploy.sh`). To include this site, add `/var/backups/cave-academy` to the two `restic ... backup` lines of that script **in the cave LMS repo** (editing the installed copy is overwritten by its next deploy). Then check with `restic -r /var/backups/cave-lms/restic snapshots --path /var/backups/cave-academy` (load `/etc/cave-lms/backup.env` and set `RESTIC_PASSWORD_FILE=/etc/cave-lms/restic-password` first). That is not set up yet.

### Restore a backup

```bash
ssh vps
systemctl stop cave-academy
cd /var/lib/cave-academy
cp cave-academy.db cave-academy.db.broken        # keep the current file, just in case
rm -f cave-academy.db-wal cave-academy.db-shm
cp /var/backups/cave-academy/cave-academy-YYYY-MM-DD.db cave-academy.db
chown cave_academy:cave_academy cave-academy.db
chmod 600 cave-academy.db
systemctl start cave-academy
```

From restic instead: `restic -r /var/backups/cave-lms/restic restore latest --path /var/backups/cave-academy --target /tmp/restore`, then copy the `.db` from `/tmp/restore/var/backups/cave-academy/` as above. Everything since the backup is lost.

### Rotating keys

Edit `/etc/cave-academy/env` on the VPS, then `systemctl restart cave-academy`. Create the new OpenRouter or Brevo key first, switch, check the site (a chat reply, a contact-form email), then revoke the old one. If a key leaked, revoke first.

## Pre-launch checklist

- [ ] DNS: `dig +short caveacademy.my www.caveacademy.my` both give `13.140.65.183`, not proxied.
- [ ] Brevo: `caveacademy.my` shows Authenticated (DKIM, DMARC); `BREVO_API_KEY` is set in the env file.
- [ ] Env file has all six keys; `BASE_URL=https://caveacademy.my`.
- [ ] `deploy/production.sh` ends with `production: https://caveacademy.my at <commit>`; `https://www.caveacademy.my/` redirects to the apex.
- [ ] `ssh vps cat /var/lib/cave-academy/deployed` equals `git rev-parse origin/main`.
- [ ] Sign up with a real address: the mail arrives (inbox, not spam), the link uses `https://caveacademy.my`.
- [ ] First admin created with `make-admin`; admin pages load.
- [ ] Submit the contact and RFQ forms: `ADMIN_EMAIL` receives both.
- [ ] Chat with each bot once: replies come from the real model, not the "not configured" message.
- [ ] Run the backup once by hand, check the file, and do a restore rehearsal on a copy (`sqlite3 <copy> 'select count(*) from users'`).
- [ ] Restic: `/var/backups/cave-academy` added to the cave LMS backup, or a decision to skip it.
- [ ] Production has no `noindex` header and no basic auth (those are staging only).
- [ ] CI is green on `main`.
