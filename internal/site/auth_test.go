package site

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// post sends a form POST to path, with the given cookies and extra headers.
func authPost(t *testing.T, s *Server, path string, form url.Values, cookies []*http.Cookie, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func sessionOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	return nil
}

func signupForm(email string) url.Values {
	return url.Values{"name": {"Siti Aminah"}, "email": {email}, "password": {"rahsia123"}}
}

// signUp registers a user and returns the session cookie.
func signUp(t *testing.T, s *Server, email string) *http.Cookie {
	t.Helper()
	rec := authPost(t, s, "/daftar", signupForm(email), nil)
	c := sessionOf(rec)
	if rec.Code != http.StatusSeeOther || c == nil {
		t.Fatalf("signup = %d, cookie %v", rec.Code, c)
	}
	return c
}

func request(s *Server, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestSignupCreatesUserAndSession(t *testing.T) {
	s := newTestServer(t)
	rec := authPost(t, s, "/daftar", signupForm("  Siti@Example.COM "), nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ai-nlp-bots" {
		t.Fatalf("signup = %d -> %q, want 303 -> /ai-nlp-bots", rec.Code, rec.Header().Get("Location"))
	}
	c := sessionOf(rec)
	if c == nil || len(c.Value) != 64 || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*24*3600 {
		t.Fatalf("bad session cookie: %+v", c)
	}
	if c.Secure {
		t.Error("cookie Secure over plain HTTP")
	}
	var email, hash, plan string
	if err := s.db.QueryRow(`SELECT email, password_hash, plan FROM users`).Scan(&email, &hash, &plan); err != nil {
		t.Fatal(err)
	}
	if email != "siti@example.com" || plan != "free" || hash == "rahsia123" || !strings.HasPrefix(hash, "$2") {
		t.Errorf("stored user = %q %q %q", email, plan, hash)
	}
	u := s.currentUserFromCookie(t, c)
	if u == nil || u.Name != "Siti Aminah" || u.Email != "siti@example.com" || u.Plan != "free" {
		t.Errorf("currentUser = %+v", u)
	}
}

// currentUserFromCookie is a test shortcut around currentUser.
func (s *Server) currentUserFromCookie(t *testing.T, c *http.Cookie) *User {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return s.currentUser(r)
}

func TestSignupCookieSecureBehindHTTPS(t *testing.T) {
	s := newTestServer(t)
	// Caddy runs on this box, so the peer is loopback.
	rec := postFrom(s, "/daftar", signupForm("a@b.my"), "127.0.0.1:5", "X-Forwarded-Proto", "https")
	if c := sessionOf(rec); c == nil || !c.Secure {
		t.Errorf("cookie = %+v, want Secure", c)
	}
}

func TestSignupErrorsKeepValuesNeverPassword(t *testing.T) {
	s := newTestServer(t)
	signUp(t, s, "siti@example.com")
	cases := map[string]url.Values{
		"Emel ini sudah berdaftar":    signupForm("SITI@example.com"),
		"sekurang-kurangnya 8 aksara": {"name": {"Ali"}, "email": {"ali@example.com"}, "password": {"pendek1"}},
		"Sila masukkan nama":          {"name": {" "}, "email": {"ali@example.com"}, "password": {"rahsia123"}},
		"Sila masukkan emel yang sah": {"name": {"Ali"}, "email": {"bukan-emel"}, "password": {"rahsia123"}},
	}
	for want, form := range cases {
		rec := authPost(t, s, "/daftar", form, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, want) {
			t.Errorf("%q: code %d, body missing message", want, rec.Code)
		}
		if sessionOf(rec) != nil {
			t.Errorf("%q: session set on error", want)
		}
		if !strings.Contains(body, `value="`+form.Get("name")+`"`) && strings.TrimSpace(form.Get("name")) != "" {
			t.Errorf("%q: name not kept", want)
		}
		if strings.Contains(body, form.Get("password")) {
			t.Errorf("%q: password echoed back", want)
		}
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Errorf("%d users, want 1", n)
	}
}

func TestLogin(t *testing.T) {
	s := newTestServer(t)
	signUp(t, s, "siti@example.com")

	rec := authPost(t, s, "/log-masuk", url.Values{"email": {" SITI@example.com"}, "password": {"rahsia123"}}, nil)
	if rec.Code != http.StatusSeeOther || sessionOf(rec) == nil || s.currentUserFromCookie(t, sessionOf(rec)) == nil {
		t.Fatalf("login = %d, cookie %v", rec.Code, sessionOf(rec))
	}

	for _, form := range []url.Values{
		{"email": {"siti@example.com"}, "password": {"salah-salah"}},
		{"email": {"tiada@example.com"}, "password": {"rahsia123"}},
	} {
		rec := authPost(t, s, "/log-masuk", form, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusUnauthorized || !strings.Contains(body, "Emel atau kata laluan tidak betul") ||
			!strings.Contains(body, form.Get("email")) || strings.Contains(body, form.Get("password")) || sessionOf(rec) != nil {
			t.Errorf("bad login %v: code %d, cookie %v", form, rec.Code, sessionOf(rec))
		}
	}
}

func TestLogoutDeletesSessionAndCookie(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")

	rec := authPost(t, s, "/log-keluar", url.Values{}, []*http.Cookie{c})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("logout = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if cc := sessionOf(rec); cc == nil || cc.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", cc)
	}
	if s.currentUserFromCookie(t, c) != nil {
		t.Error("still logged in after logout")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Errorf("%d session rows left", n)
	}
}

func TestLogoutPageAsksForConfirmation(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/log-keluar")
	if !strings.Contains(body, `method="post"`) || !strings.Contains(body, `action="/log-keluar"`) {
		t.Error("GET /log-keluar has no POST form")
	}
}

func TestNextRedirectIsLocalOnly(t *testing.T) {
	s := newTestServer(t)
	i := 0
	for next, want := range map[string]string{
		"/ai-nlp-bots/hr?x=1":   "/ai-nlp-bots/hr?x=1",
		"":                      "/ai-nlp-bots",
		"https://evil.example/": "/ai-nlp-bots",
		"//evil.example":        "/ai-nlp-bots",
		"/\\evil.example":       "/ai-nlp-bots",
		"evil":                  "/ai-nlp-bots",
	} {
		i++
		form := signupForm(fmt.Sprintf("u%d@example.com", i))
		form.Set("next", next)
		rec := postFrom(s, "/daftar", form, fmt.Sprintf("198.51.100.%d:1", i)) // own IP each: signups are rate limited per IP
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("next %q -> %q, want %q", next, got, want)
		}
	}
	// next survives a failed login and is passed on by a good one.
	signUp(t, s, "siti@example.com")
	form := url.Values{"email": {"siti@example.com"}, "password": {"rahsia123"}, "next": {"/produk-digital"}}
	if loc := authPost(t, s, "/log-masuk", form, nil).Header().Get("Location"); loc != "/produk-digital" {
		t.Errorf("login next = %q", loc)
	}
	form.Set("password", "salah-salah")
	if body := authPost(t, s, "/log-masuk", form, nil).Body.String(); !strings.Contains(body, `name="next" value="/produk-digital"`) {
		t.Error("failed login lost next")
	}
}

func TestExpiredSessionIsLoggedOut(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	if _, err := s.db.Exec(`UPDATE sessions SET expires_at = datetime('now', '-1 minute')`); err != nil {
		t.Fatal(err)
	}
	if s.currentUserFromCookie(t, c) != nil {
		t.Error("expired session still logs in")
	}
	if s.currentUserFromCookie(t, &http.Cookie{Name: "session", Value: "nope"}) != nil || s.currentUserFromCookie(t, nil) != nil {
		t.Error("unknown or missing cookie logs in")
	}
}

func TestRequireUserRedirectsToLogin(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/ai-nlp-bots/hr?a=1", nil)
	rec := httptest.NewRecorder()
	if u, ok := s.requireUser(rec, req); ok || u != nil {
		t.Fatal("requireUser ok while logged out")
	}
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != "/log-masuk?next=%2Fai-nlp-bots%2Fhr%3Fa%3D1" {
		t.Errorf("requireUser = %d -> %q", rec.Code, loc)
	}

	c := signUp(t, s, "siti@example.com")
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(c)
	if u, ok := s.requireUser(httptest.NewRecorder(), req); !ok || u.Email != "siti@example.com" {
		t.Errorf("requireUser = %+v, %v", u, ok)
	}
}

func TestPostFromOtherOriginIsRejected(t *testing.T) {
	s := newTestServer(t)
	rec := authPost(t, s, "/daftar", signupForm("a@b.my"), nil, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || sessionOf(rec) != nil {
		t.Errorf("cross-origin POST = %d", rec.Code)
	}
	rec = authPost(t, s, "/daftar", signupForm("a@b.my"), nil, "Origin", "http://example.com") // httptest host
	if rec.Code != http.StatusSeeOther {
		t.Errorf("same-origin POST = %d", rec.Code)
	}
}

func TestHeaderShowsAccountLinks(t *testing.T) {
	s := newTestServer(t)
	if _, body := get(t, s, "/"); !strings.Contains(body, `class="nav__login" href="/log-masuk"`) || strings.Contains(body, "Log Keluar") {
		t.Error("logged-out header should show Log Masuk only")
	}
	c := signUp(t, s, "siti@example.com")
	body := request(s, "/", c).Body.String()
	if !strings.Contains(body, "Siti") || !strings.Contains(body, "Log Keluar") || strings.Contains(body, `class="nav__login"`) {
		t.Error("logged-in header should show first name and Log Keluar")
	}
}
