package site

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var resetLink = regexp.MustCompile(`/reset-kata-laluan\?token=([A-Za-z0-9_-]+)`)

// resetServer is a test server whose mail lands in the returned buffer.
func resetServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	s := newTestServer(t)
	out := &bytes.Buffer{}
	s.mailer.Out = out
	s.config.BaseURL = "http://site.test"
	return s, out
}

func askReset(t *testing.T, s *Server, email string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	return authPost(t, s, "/lupa-kata-laluan", url.Values{"email": {email}}, nil, hdr...)
}

func tokenFrom(t *testing.T, out *bytes.Buffer) string {
	t.Helper()
	m := resetLink.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no reset link in mail: %q", out.String())
	}
	return m[1]
}

func setPassword(t *testing.T, s *Server, token, pw string) *httptest.ResponseRecorder {
	t.Helper()
	return authPost(t, s, "/reset-kata-laluan", url.Values{"token": {token}, "password": {pw}}, nil)
}

func TestResetUnknownEmailSameResponseNoMail(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	known := askReset(t, s, "siti@example.com")
	out.Reset()
	unknown := askReset(t, s, "tiada@example.com")
	if unknown.Code != known.Code || unknown.Body.String() != known.Body.String() {
		t.Errorf("responses differ: %d vs %d", unknown.Code, known.Code)
	}
	if !strings.Contains(unknown.Body.String(), "Jika e-mel itu berdaftar") {
		t.Error("generic message missing")
	}
	if out.Len() != 0 {
		t.Errorf("mail sent to unknown address: %q", out.String())
	}
}

func TestResetLinkWorksOnce(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	askReset(t, s, "SITI@example.com")
	if !strings.Contains(out.String(), "To: siti@example.com") || !strings.Contains(out.String(), "1 jam") {
		t.Fatalf("bad mail: %q", out.String())
	}
	if !strings.Contains(out.String(), "http://site.test/reset-kata-laluan?token=") {
		t.Fatalf("link not absolute: %q", out.String())
	}
	token := tokenFrom(t, out)

	rec := request(s, "/reset-kata-laluan?token="+token)
	if rec.Code != 200 || rec.Header().Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(rec.Body.String(), `name="token" value="`+token+`"`) {
		t.Fatalf("form: %d %q", rec.Code, rec.Header())
	}

	if rec := setPassword(t, s, token, "pendek"); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "sekurang-kurangnya 8 aksara") {
		t.Fatalf("short password: %d", rec.Code)
	}
	rec = setPassword(t, s, token, "kata-baharu-1")
	if rec.Code != http.StatusSeeOther || sessionOf(rec) == nil {
		t.Fatalf("reset = %d, cookie %v", rec.Code, sessionOf(rec))
	}
	// New password works, old one does not.
	if r := authPost(t, s, "/log-masuk", url.Values{"email": {"siti@example.com"}, "password": {"kata-baharu-1"}}, nil); r.Code != http.StatusSeeOther {
		t.Errorf("login with new password = %d", r.Code)
	}
	if r := authPost(t, s, "/log-masuk", url.Values{"email": {"siti@example.com"}, "password": {"rahsia123"}}, nil); r.Code != http.StatusUnauthorized {
		t.Errorf("login with old password = %d", r.Code)
	}
	// Used token is refused, on GET and POST.
	if rec := request(s, "/reset-kata-laluan?token="+token); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "/lupa-kata-laluan") {
		t.Errorf("used token GET = %d", rec.Code)
	}
	if rec := setPassword(t, s, token, "lagi-satu-1"); rec.Code != http.StatusGone || sessionOf(rec) != nil {
		t.Errorf("used token POST = %d", rec.Code)
	}
}

func TestResetExpiredTokenRefused(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	askReset(t, s, "siti@example.com")
	token := tokenFrom(t, out)
	if _, err := s.db.Exec(`UPDATE password_resets SET expires_at = datetime('now', '-1 minute')`); err != nil {
		t.Fatal(err)
	}
	if rec := request(s, "/reset-kata-laluan?token="+token); rec.Code != http.StatusGone {
		t.Errorf("GET = %d", rec.Code)
	}
	if rec := setPassword(t, s, token, "kata-baharu-1"); rec.Code != http.StatusGone {
		t.Errorf("POST = %d", rec.Code)
	}
	if rec := request(s, "/reset-kata-laluan?token=bukan-token"); rec.Code != http.StatusGone {
		t.Errorf("garbage token GET = %d", rec.Code)
	}
}

func TestResetNewRequestInvalidatesOlderToken(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	askReset(t, s, "siti@example.com")
	old := tokenFrom(t, out)
	out.Reset()
	askReset(t, s, "siti@example.com")
	if tokenFrom(t, out) == old {
		t.Fatal("same token twice")
	}
	if rec := setPassword(t, s, old, "kata-baharu-1"); rec.Code != http.StatusGone {
		t.Errorf("older token = %d", rec.Code)
	}
}

func TestResetRevokesOldSessions(t *testing.T) {
	s, out := resetServer(t)
	old := signUp(t, s, "siti@example.com")
	askReset(t, s, "siti@example.com")
	rec := setPassword(t, s, tokenFrom(t, out), "kata-baharu-1")
	if s.currentUserFromCookie(t, old) != nil {
		t.Error("old session still valid")
	}
	if c := sessionOf(rec); c == nil || c.Value == old.Value || s.currentUserFromCookie(t, c) == nil {
		t.Errorf("no fresh session: %+v", c)
	}
}

func TestResetRateLimit(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	for i := 0; i < 3; i++ {
		askReset(t, s, "siti@example.com")
	}
	out.Reset()
	rec := askReset(t, s, "siti@example.com")
	if out.Len() != 0 || rec.Code != 200 || !strings.Contains(rec.Body.String(), "Jika e-mel itu berdaftar") {
		t.Errorf("4th request: mail %q, code %d", out.String(), rec.Code)
	}
	// Per IP: 10 per hour across different emails.
	s, out = resetServer(t)
	signUp(t, s, "a@example.com")
	for i := 0; i < 10; i++ {
		askReset(t, s, "x"+string(rune('a'+i))+"@example.com")
	}
	askReset(t, s, "a@example.com")
	if out.Len() != 0 {
		t.Errorf("mail sent past the IP limit: %q", out.String())
	}
}

func TestResetCrossOriginRefused(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	if rec := askReset(t, s, "siti@example.com", "Origin", "https://evil.test"); rec.Code != http.StatusForbidden || out.Len() != 0 {
		t.Errorf("request: %d, mail %q", rec.Code, out.String())
	}
	askReset(t, s, "siti@example.com")
	token := tokenFrom(t, out)
	rec := authPost(t, s, "/reset-kata-laluan", url.Values{"token": {token}, "password": {"kata-baharu-1"}}, nil, "Origin", "https://evil.test")
	if rec.Code != http.StatusForbidden {
		t.Errorf("set password: %d", rec.Code)
	}
}

func TestResetTokenStoredHashed(t *testing.T) {
	s, out := resetServer(t)
	signUp(t, s, "siti@example.com")
	askReset(t, s, "siti@example.com")
	token := tokenFrom(t, out)
	var stored string
	if err := s.db.QueryRow(`SELECT token_hash FROM password_resets`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token || strings.Contains(stored, token) || stored != hashToken(token) {
		t.Errorf("stored %q for token %q", stored, token)
	}
}

func TestLoginFormLinksToReset(t *testing.T) {
	s := newTestServer(t)
	if _, body := get(t, s, "/log-masuk"); !strings.Contains(body, `href="/lupa-kata-laluan"`) {
		t.Error("login form has no forgot-password link")
	}
}
