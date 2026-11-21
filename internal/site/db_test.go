package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDBCreatesSchemaAndPragmas(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{"users", "sessions", "chat_messages", "rfq_requests", "trainer_applications", "contact_messages"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d, err=%v)", table, n, err)
		}
	}

	var fk int
	var mode string
	db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if fk != 1 || mode != "wal" {
		t.Errorf("foreign_keys=%d journal_mode=%q, want 1 and wal", fk, mode)
	}

	// New users default to the free plan; email is unique.
	if _, err := db.Exec(`INSERT INTO users (name, email, password_hash) VALUES ('A', 'a@example.com', 'x')`); err != nil {
		t.Fatal(err)
	}
	var plan string
	db.QueryRow(`SELECT plan FROM users WHERE email='a@example.com'`).Scan(&plan)
	if plan != "free" {
		t.Errorf("plan = %q, want free", plan)
	}
	if _, err := db.Exec(`INSERT INTO users (name, email, password_hash) VALUES ('B', 'a@example.com', 'x')`); err == nil {
		t.Error("duplicate email accepted")
	}
	// Sessions need a real user (foreign keys on).
	if _, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES ('t', 999, '2030-01-01 00:00:00')`); err == nil {
		t.Error("session for missing user accepted")
	}
}

func TestOpenDBIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "with space & more", "again.db")
	os.MkdirAll(filepath.Dir(path), 0o755)
	for range 2 {
		db, err := OpenDB(path)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
