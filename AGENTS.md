# The Cave Academy website

Abah is the user and sole author of this repo. Spec: `docs/spec.md`. Mockups: `docs/mockups/`.

- Go stdlib + `html/template` + `embed`, SQLite via `modernc.org/sqlite`, plain CSS. No new dependencies without a reason.
- Public copy is Malay. Code, comments and commits are English.
- Run `./test.sh` before committing. Run the site with `go run .` (see `docs/spec.md` for env vars).
- Keep each page's handler in its own file and its CSS in `internal/site/static/css/<page>.css` (served at `/static/css/<page>.css`) and its template in `internal/site/templates/<name>.html`, so parallel work doesn't collide on shared files.
- This repo is public: never commit secrets, `.env` or `*.db`.
- Attribution: no agent credit lines (no Co-Authored-By, no "Generated with") in commits, PRs, issues or comments. No emojis.
- Work goes through GitHub issues and PRs on `H4fizWasabie/cave-academy`; one branch per issue, PR body says `Closes #N`.
