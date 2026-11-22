package site

import (
	"database/sql"
	"net/url"

	_ "modernc.org/sqlite"
)

// OpenDB opens the SQLite file (WAL, foreign keys on) and creates every v1
// table. All tables live here so page work never needs to touch this file.
//
// Timestamps are UTC text, "YYYY-MM-DD HH:MM:SS" (SQLite datetime('now')), so
// they compare correctly as strings, e.g. `expires_at > datetime('now')`.
func OpenDB(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrate adds columns that CREATE TABLE IF NOT EXISTS cannot add to a table
// that already exists. Each step checks PRAGMA table_info first, so it is safe
// on fresh and old databases and on every start.
func migrate(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info('users')`)
	if err != nil {
		return err
	}
	has := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		has[n] = true
	}
	rows.Close()
	if !has["is_admin"] {
		_, err = db.Exec(`ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0`)
	}
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	plan          TEXT NOT NULL DEFAULT 'free',
	created_at    TEXT NOT NULL DEFAULT (datetime('now')),
	is_admin      INTEGER NOT NULL DEFAULT 0 -- set only by the make-admin CLI (admin.go)
);

CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL
);

-- Only the SHA-256 of a reset token is stored, so a leaked database cannot be used to reset passwords.
CREATE TABLE IF NOT EXISTS password_resets (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL,
	used_at    TEXT
);

CREATE TABLE IF NOT EXISTS chat_messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	bot        TEXT NOT NULL,
	role       TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS chat_messages_user_bot ON chat_messages (user_id, bot, id);

CREATE TABLE IF NOT EXISTS rfq_requests (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	organisation  TEXT NOT NULL,
	contact_name  TEXT NOT NULL,
	email         TEXT NOT NULL,
	phone         TEXT NOT NULL,
	training_area TEXT NOT NULL,
	participants  INTEGER NOT NULL DEFAULT 0,
	preferred_date TEXT NOT NULL DEFAULT '',
	notes         TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS trainer_applications (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	name             TEXT NOT NULL,
	email            TEXT NOT NULL,
	phone            TEXT NOT NULL,
	expertise        TEXT NOT NULL,
	years_experience INTEGER NOT NULL DEFAULT 0,
	bio              TEXT NOT NULL DEFAULT '',
	created_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS contact_messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT NOT NULL,
	email      TEXT NOT NULL,
	message    TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Paid tiers (plans.go, bcl.go). Plans are data: admins edit them at /admin/pelan and never delete one, only
-- deactivate it, so a subscription always points at a real plan.
CREATE TABLE IF NOT EXISTS plans (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	name            TEXT NOT NULL,
	price_sen       INTEGER NOT NULL,
	daily_questions INTEGER NOT NULL DEFAULT 0, -- 0 = unlimited
	days            INTEGER NOT NULL,
	bcl_link        TEXT NOT NULL DEFAULT '',   -- the BCL payment form; its last path part (slug) identifies the plan
	active          INTEGER NOT NULL DEFAULT 1,
	sort            INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	plan_id   INTEGER NOT NULL REFERENCES plans(id),
	starts_at TEXT NOT NULL,
	ends_at   TEXT NOT NULL,
	bcl_order TEXT NOT NULL UNIQUE -- one grant per BCL order, so a webhook retry is a no-op
);
CREATE INDEX IF NOT EXISTS subscriptions_user ON subscriptions (user_id, ends_at);

-- Paid BCL orders that gave nothing (no account with the payer's email, an unknown form, too little paid), kept for an admin.
CREATE TABLE IF NOT EXISTS unmatched_payments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	bcl_order  TEXT NOT NULL UNIQUE,
	email      TEXT NOT NULL,
	plan_id    INTEGER REFERENCES plans(id), -- NULL when no plan matched the form
	paid_sen   INTEGER NOT NULL,
	reason     TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Trainer profiles (trainers.go): one per account. status moves member -> cave_trainer
-- only by an admin; a profile is public only when published = 1 AND status = 'cave_trainer'.
CREATE TABLE IF NOT EXISTS trainer_profiles (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
	slug         TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL,
	headline     TEXT NOT NULL DEFAULT '',
	bio          TEXT NOT NULL DEFAULT '',
	expertise    TEXT NOT NULL DEFAULT '', -- comma separated
	years        INTEGER NOT NULL DEFAULT 0,
	phone        TEXT NOT NULL DEFAULT '',
	location     TEXT NOT NULL DEFAULT '',
	linkedin_url TEXT NOT NULL DEFAULT '',
	template     TEXT NOT NULL DEFAULT 'klasik',
	status       TEXT NOT NULL DEFAULT 'member' CHECK (status IN ('member', 'cave_trainer')),
	published    INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Emails an admin sent to a trainer (admin_trainers.go).
CREATE TABLE IF NOT EXISTS trainer_messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id INTEGER NOT NULL REFERENCES trainer_profiles(id) ON DELETE CASCADE,
	subject    TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- RAG material uploaded by admins (knowledge.go). bot is a bot slug or 'all'.
CREATE TABLE IF NOT EXISTS knowledge_docs (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	bot        TEXT NOT NULL,
	title      TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
-- Searchable chunks of those documents; doc_id and bot are filters, not indexed text.
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunks USING fts5(doc_id UNINDEXED, bot UNINDEXED, title, body, tokenize = 'unicode61 remove_diacritics 2');
`
