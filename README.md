# The Cave Academy

Website for The Cave Academy (caveacademy.my): marketing pages, accounts, and the AI NLP Bots chat. Spec: `docs/spec.md`.

## Run locally

```bash
cp .env.example .env   # then fill in OPENROUTER_API_KEY (and BREVO_API_KEY to send real email)
set -a; . ./.env; set +a; go run .
```

Open http://localhost:8080. Without `BREVO_API_KEY`, form emails are printed to the log. Without `OPENROUTER_API_KEY`, the bots reply that they are not configured.

To give a user unlimited chat (until paid plans land): `sqlite3 cave-academy.db "UPDATE users SET plan='paid' WHERE email='...'"`.

## Test

```bash
./test.sh
```

## Staging

```bash
deploy/staging.sh
```

From a clean, pushed `main`. Builds a Linux binary, installs it on the VPS as `cave-academy-staging` (systemd, port 8096) behind Caddy at https://cave-academy.13-140-65-183.sslip.io/ (the cave LMS staging login guards it, `noindex`). Secrets live only in `/etc/cave-academy/staging.env` on the VPS (`OPENROUTER_API_KEY`, `BREVO_API_KEY`, `MAIL_FROM`); after editing it, `systemctl restart cave-academy-staging`. Without `BREVO_API_KEY`, form emails go to `journalctl -u cave-academy-staging`.

## Production

```bash
deploy/production.sh            # from a clean checkout at origin/main's HEAD
deploy/production.sh rollback   # previous binary
```

Runs the tests, builds, installs on the VPS behind Caddy at https://caveacademy.my/ and checks it; a failed smoke test rolls back. Daily SQLite backups run from a systemd timer. One-time setup (DNS, Brevo, env file, first admin), backups, restore and the pre-launch checklist: `docs/deploy.md`.
