package site

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// adminCookie signs up an account and makes it admin through the same
// function the CLI uses.
func adminCookie(t *testing.T, s *Server, email string) *http.Cookie {
	t.Helper()
	c := signUp(t, s, email)
	if err := SetAdmin(s.db, email, true); err != nil {
		t.Fatal(err)
	}
	return c
}

func seedForms(t *testing.T, s *Server) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO rfq_requests (organisation, contact_name, email, phone, training_area, participants, preferred_date, notes, created_at)
			VALUES ('Syarikat Maju', 'Ali Bin Abu', 'ali@maju.my', '012-345 6789', 'AI untuk HR', 25, '2026-12-01', 'Perlu <b>sijil</b>', '2020-11-20 16:30:00')`,
		`INSERT INTO trainer_applications (name, email, phone, expertise, years_experience, bio, created_at)
			VALUES ('Aminah', 'aminah@x.my', '+60 11-2233 4455', 'NLP', 7, 'Bio panjang', datetime('now'))`,
		`INSERT INTO contact_messages (name, email, message, created_at) VALUES ('Budi', 'budi@x.my', '=HYPERLINK("http://evil")', '2020-01-01 00:00:00')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAdminLoggedOutRedirectsToLogin(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"/admin", "/admin/rfq", "/admin/pengguna?format=csv"} {
		rec := request(s, p)
		want := "/log-masuk?next=" + url.QueryEscape(p)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("GET %s = %d -> %q, want 303 -> %q", p, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestAdminNonAdminGets404(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "biasa@example.com")
	for _, p := range []string{"/admin", "/admin/rfq", "/admin/trainer", "/admin/mesej", "/admin/pengguna", "/admin/rfq?format=csv"} {
		if rec := request(s, p, c); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
	rec := authPost(t, s, "/admin/pengguna", url.Values{"id": {"1"}, "plan": {"paid"}}, []*http.Cookie{c})
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST by non-admin = %d, want 404", rec.Code)
	}
	var plan string
	s.db.QueryRow(`SELECT plan FROM users`).Scan(&plan)
	if plan != "free" {
		t.Errorf("non-admin changed a plan to %q", plan)
	}
}

func TestSignupNeverMakesAdmin(t *testing.T) {
	s := newTestServer(t)
	rec := authPost(t, s, "/daftar", url.Values{"name": {"X"}, "email": {"admin@example.com"}, "password": {"rahsia123"}, "is_admin": {"1"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	if code := request(s, "/admin", sessionOf(rec)).Code; code != http.StatusNotFound {
		t.Errorf("fresh signup reaches /admin: %d", code)
	}
}

func TestAdminOverviewCounts(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	seedForms(t, s)
	s.db.Exec(`UPDATE users SET plan='paid'`)
	rec := request(s, "/admin", c)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `name="robots" content="noindex"`) {
		t.Fatalf("overview = %d, noindex missing", rec.Code)
	}
	for _, want := range []string{`href="/admin/rfq"`, `href="/admin/trainer"`, `href="/admin/mesej"`, `href="/admin/pengguna"`, "1 berbayar", "7 hari"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing %q", want)
		}
	}
	// The trainer application and the user are recent, the old message is not.
	if !strings.Contains(body, `data-week="1"`) || !strings.Contains(body, `data-week="0"`) {
		t.Errorf("overview week counts wrong:\n%s", body)
	}
}

func TestAdminListsShowRowsInKLTime(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	seedForms(t, s)
	_, rfq := get2(s, "/admin/rfq", c)
	for _, want := range []string{
		"Syarikat Maju", "Ali Bin Abu", "AI untuk HR", "2026-12-01",
		`href="mailto:ali@maju.my"`, `href="tel:0123456789"`, `href="https://wa.me/60123456789"`,
		"21/11/2020 00:30", // 16:30 UTC is 00:30 the next day in Kuala Lumpur
		"Perlu &lt;b&gt;sijil&lt;/b&gt;",
	} {
		if !strings.Contains(rfq, want) {
			t.Errorf("rfq list missing %q", want)
		}
	}
	_, tr := get2(s, "/admin/trainer", c)
	for _, want := range []string{"Aminah", "NLP", "Bio panjang", `href="https://wa.me/601122334455"`, `href="mailto:aminah@x.my"`} {
		if !strings.Contains(tr, want) {
			t.Errorf("trainer list missing %q", want)
		}
	}
	_, ms := get2(s, "/admin/mesej", c)
	if !strings.Contains(ms, "Budi") || !strings.Contains(ms, "01/01/2020 08:00") {
		t.Errorf("mesej list wrong")
	}
	if code, _ := get2(s, "/admin/tiada", c); code != 404 {
		t.Errorf("unknown admin page = %d, want 404", code)
	}
}

func TestAdminListNewestFirstAndCapped(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	for i := 0; i < adminLimit+5; i++ {
		s.db.Exec(`INSERT INTO contact_messages (name, email, message) VALUES (?, 'a@b.my', 'm')`, "orang"+string(rune('A'+i%26))+strings.Repeat("x", i/26))
	}
	_, body := get2(s, "/admin/mesej", c)
	if n := strings.Count(body, `<tr class="adm-row"`); n != adminLimit {
		t.Errorf("rows = %d, want %d", n, adminLimit)
	}
	_, csv := get2(s, "/admin/mesej?format=csv", c)
	if n := strings.Count(csv, "\n"); n != adminLimit+5+1 {
		t.Errorf("csv lines = %d, want every row plus header", n)
	}
}

func TestAdminUsersListAndPlanToggle(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	signUp(t, s, "siti@example.com")
	_, body := get2(s, "/admin/pengguna", c)
	for _, want := range []string{"siti@example.com", "boss@example.com", "adm-badge", `name="plan" value="paid"`} {
		if !strings.Contains(body, want) {
			t.Errorf("users list missing %q", want)
		}
	}
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='siti@example.com'`).Scan(&id)
	form := func(plan string) url.Values { return url.Values{"id": {strconv.FormatInt(id, 10)}, "plan": {plan}} }
	plan := func() (p string) { s.db.QueryRow(`SELECT plan FROM users WHERE id=?`, id).Scan(&p); return }

	rec := authPost(t, s, "/admin/pengguna", form("paid"), []*http.Cookie{c})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/pengguna" || plan() != "paid" {
		t.Fatalf("toggle = %d -> %q, plan %q", rec.Code, rec.Header().Get("Location"), plan())
	}
	if rec := authPost(t, s, "/admin/pengguna", form("free"), []*http.Cookie{c}, "Origin", "http://example.com"); rec.Code != 303 || plan() != "free" {
		t.Errorf("same-origin toggle back = %d, plan %q", rec.Code, plan())
	}
	s.db.Exec(`UPDATE users SET plan='paid' WHERE id=?`, id)
	if rec := authPost(t, s, "/admin/pengguna", form("free"), []*http.Cookie{c}, "Origin", "https://evil.example"); rec.Code != http.StatusForbidden || plan() != "paid" {
		t.Errorf("cross-origin = %d, plan %q, want 403 and unchanged", rec.Code, plan())
	}
	for _, bad := range []string{"admin", "PAID", "", "paid "} {
		if rec := authPost(t, s, "/admin/pengguna", form(bad), []*http.Cookie{c}); rec.Code != http.StatusBadRequest || plan() != "paid" {
			t.Errorf("plan %q = %d, plan now %q, want 400 and unchanged", bad, rec.Code, plan())
		}
	}
	if rec := authPost(t, s, "/admin/pengguna", url.Values{"id": {"9999"}, "plan": {"paid"}}, []*http.Cookie{c}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", rec.Code)
	}
	// There is no way to change the admin flag from the web.
	authPost(t, s, "/admin/pengguna", url.Values{"id": {strconv.FormatInt(id, 10)}, "plan": {"free"}, "is_admin": {"1"}}, []*http.Cookie{c})
	var adm int
	s.db.QueryRow(`SELECT is_admin FROM users WHERE id=?`, id).Scan(&adm)
	if adm != 0 {
		t.Error("is_admin set through the web form")
	}
}

func TestAdminUsersShowsQuestionsToday(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	s.chat.Now = func() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, klZone) }
	s.db.Exec(`INSERT INTO chat_messages (user_id, bot, role, content, created_at) VALUES (1, 'hr', 'user', 'q', '2026-11-22 03:00:00'), (1, 'hr', 'user', 'q', '2026-11-22 04:00:00'), (1, 'hr', 'assistant', 'a', '2026-11-22 04:00:00'), (1, 'hr', 'user', 'old', '2026-11-20 04:00:00')`)
	_, csv := get2(s, "/admin/pengguna?format=csv", c)
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], ",2,Ya") {
		t.Errorf("users csv = %q, want 2 questions today and admin Ya", csv)
	}
}

func TestAdminCSV(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "boss@example.com")
	seedForms(t, s)
	rec := request(s, "/admin/mesej?format=csv", c)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("csv headers: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Error("csv has no UTF-8 BOM")
	}
	if !strings.Contains(body, `'=HYPERLINK`) || strings.Contains(body, `,"=HYPERLINK`) || strings.Contains(body, ",=HYPERLINK") {
		t.Errorf("formula cell not neutralised: %q", body)
	}
	for _, cell := range []string{"=1+1", "+60123", "-5", "@SUM(A1)", "\t=x"} {
		if got := csvSafe(cell); !strings.HasPrefix(got, "'") {
			t.Errorf("csvSafe(%q) = %q", cell, got)
		}
	}
	if csvSafe("Ali") != "Ali" || csvSafe("") != "" {
		t.Error("csvSafe changed a plain cell")
	}
	_, rfq := get2(s, "/admin/rfq?format=csv", c)
	if !strings.Contains(rfq, "Organisasi") || !strings.Contains(rfq, "2020-11-21 00:30") {
		t.Errorf("rfq csv missing header or KL date: %q", rfq)
	}
}

func TestAdminLinkOnlyForAdmins(t *testing.T) {
	s := newTestServer(t)
	user := signUp(t, s, "biasa@example.com")
	boss := adminCookie(t, s, "boss@example.com")
	if _, body := get2(s, "/", user); strings.Contains(body, `href="/admin"`) {
		t.Error("Admin link shown to a normal user")
	}
	if _, body := get2(s, "/", nil); strings.Contains(body, `href="/admin"`) {
		t.Error("Admin link shown when logged out")
	}
	if _, body := get2(s, "/", boss); !strings.Contains(body, `href="/admin"`) {
		t.Error("Admin link missing for an admin")
	}
}

func TestSetAdmin(t *testing.T) {
	s := newTestServer(t)
	signUp(t, s, "siti@example.com")
	flag := func() (n int) { s.db.QueryRow(`SELECT is_admin FROM users`).Scan(&n); return }
	if err := SetAdmin(s.db, "  Siti@Example.com ", true); err != nil || flag() != 1 {
		t.Fatalf("make-admin: err %v, flag %d", err, flag())
	}
	if err := SetAdmin(s.db, "siti@example.com", false); err != nil || flag() != 0 {
		t.Fatalf("revoke-admin: err %v, flag %d", err, flag())
	}
	if err := SetAdmin(s.db, "tiada@example.com", true); err == nil {
		t.Error("unknown account did not fail")
	}
}

func TestOpenDBAddsIsAdminToOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild users the way it was before is_admin existed.
	for _, q := range []string{
		`PRAGMA foreign_keys=OFF`,
		`DROP TABLE users`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL, plan TEXT NOT NULL DEFAULT 'free', created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`INSERT INTO users (name, email, password_hash) VALUES ('Lama', 'lama@example.com', 'x')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err = OpenDB(path) // reopening is the migration
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT is_admin FROM users WHERE email='lama@example.com'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("is_admin after migration: %d, %v", n, err)
	}
	if db2, err := OpenDB(path); err != nil { // and it is idempotent
		t.Fatal(err)
	} else {
		db2.Close()
	}
}

func TestWhatsAppDigits(t *testing.T) {
	for in, want := range map[string]string{
		"012-345 6789": "60123456789", "+60 12-345 6789": "60123456789", "60123456789": "60123456789", "(03) 1234 5678": "60312345678",
	} {
		if got := waDigits(in); got != want {
			t.Errorf("waDigits(%q) = %q, want %q", in, got, want)
		}
	}
}

// get2 is request() returning status and body.
func get2(s *Server, path string, cookies ...*http.Cookie) (int, string) {
	var cs []*http.Cookie
	for _, c := range cookies {
		if c != nil {
			cs = append(cs, c)
		}
	}
	rec := request(s, path, cs...)
	return rec.Code, rec.Body.String()
}
