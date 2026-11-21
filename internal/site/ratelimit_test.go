package site

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeClock drives every limiter in s so tests can skip the window.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func limitedServer(t *testing.T) (*Server, *fakeClock) {
	t.Helper()
	s := newTestServer(t)
	c := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	for _, l := range []*limiter{s.limits.loginEmail, s.limits.loginIP, s.limits.signupIP} {
		l.now = c.now
	}
	return s, c
}

// postFrom is authPost with an explicit RemoteAddr and extra headers.
func postFrom(s *Server, path string, form url.Values, remote string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func loginForm(email, pw string) url.Values {
	return url.Values{"email": {email}, "password": {pw}}
}

func TestLoginEmailLimit(t *testing.T) {
	s, clock := limitedServer(t)
	signUp(t, s, "a@example.com")
	for i := 0; i < 5; i++ {
		if rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "salah"), "198.51.100.1:1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rec.Code)
		}
	}
	rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "rahsia123"), "198.51.100.1:1")
	if rec.Code != http.StatusTooManyRequests || sessionOf(rec) != nil {
		t.Fatalf("6th attempt = %d (cookie %v), want 429 and no session", rec.Code, sessionOf(rec))
	}
	if body := rec.Body.String(); !strings.Contains(body, "Terlalu banyak cubaan. Sila cuba lagi dalam beberapa minit.") || !strings.Contains(body, `value="a@example.com"`) {
		t.Errorf("429 body lacks message or email:\n%s", body)
	}
	clock.t = clock.t.Add(15*time.Minute + time.Second)
	if rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "rahsia123"), "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("after window = %d, want 303", rec.Code)
	}
}

func TestLoginSuccessResetsEmailCounter(t *testing.T) {
	s, _ := limitedServer(t)
	signUp(t, s, "a@example.com")
	for round := 0; round < 3; round++ { // 12 failures in total, never 5 in a row
		for i := 0; i < 4; i++ {
			postFrom(s, "/log-masuk", loginForm("a@example.com", "salah"), "198.51.100.1:1")
		}
		if rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "rahsia123"), "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
			t.Fatalf("round %d success = %d, want 303", round, rec.Code)
		}
	}
}

func TestLoginIPLimitAcrossEmails(t *testing.T) {
	s, _ := limitedServer(t)
	signUp(t, s, "a@example.com")
	for i := 0; i < 20; i++ {
		email := string(rune('a'+i)) + "x@example.com" // 20 different emails, none over its own limit
		if rec := postFrom(s, "/log-masuk", loginForm(email, "salah"), "198.51.100.1:1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "rahsia123"), "198.51.100.1:2"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st = %d, want 429 (same IP, other port)", rec.Code)
	}
	if rec := postFrom(s, "/log-masuk", loginForm("a@example.com", "rahsia123"), "198.51.100.2:1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("other IP = %d, want 303", rec.Code)
	}
}

func TestSignupLimit(t *testing.T) {
	s, clock := limitedServer(t)
	for i := 0; i < 5; i++ {
		email := string(rune('a'+i)) + "@example.com"
		if rec := postFrom(s, "/daftar", signupForm(email), "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
			t.Fatalf("signup %d = %d, want 303", i+1, rec.Code)
		}
	}
	rec := postFrom(s, "/daftar", signupForm("z@example.com"), "198.51.100.1:1")
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Terlalu banyak cubaan") {
		t.Fatalf("6th signup = %d, want 429 with message", rec.Code)
	}
	if rec := postFrom(s, "/daftar", signupForm("z@example.com"), "198.51.100.2:1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("other IP = %d, want 303", rec.Code)
	}
	clock.t = clock.t.Add(time.Hour + time.Second)
	if rec := postFrom(s, "/daftar", signupForm("y@example.com"), "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("after window = %d, want 303", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	for _, c := range []struct{ remote, xff, want string }{
		{"198.51.100.1:5", "", "198.51.100.1"},
		{"198.51.100.1:5", "203.0.113.9", "198.51.100.1"}, // not loopback: header ignored
		{"127.0.0.1:5", "203.0.113.9", "203.0.113.9"},
		{"[::1]:5", "1.1.1.1, 203.0.113.9", "203.0.113.9"}, // last entry
		{"127.0.0.1:5", "", "127.0.0.1"},
		{"127.0.0.1:5", "junk", "127.0.0.1"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, xff %q) = %q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestLimiterPrunes(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	l := newLimiter(2, time.Minute)
	l.now = c.now
	for i := 0; i < 100; i++ {
		l.add(string(rune('a' + i)))
	}
	c.t = c.t.Add(2 * time.Minute)
	l.add("fresh")
	if len(l.hits) != 1 {
		t.Errorf("map holds %d keys after the window, want 1", len(l.hits))
	}
}
