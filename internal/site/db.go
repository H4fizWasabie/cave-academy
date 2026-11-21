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
	return db, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	plan          TEXT NOT NULL DEFAULT 'free',
	created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL
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
`
