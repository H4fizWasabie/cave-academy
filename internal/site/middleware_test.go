package site

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// captureLog redirects the standard logger into the returned buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/", "/tiada-halaman", "/static/css/base.css", "/healthz", "/robots.txt"} {
		rec := request(s, path)
		for k, want := range map[string]string{
			"X-Content-Type-Options": "nosniff", "Referrer-Policy": "strict-origin-when-cross-origin", "X-Frame-Options": "DENY",
		} {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("GET %s: %s = %q, want %q", path, k, got, want)
			}
		}
		if pp := rec.Header().Get("Permissions-Policy"); !strings.Contains(pp, "camera=()") || !strings.Contains(pp, "microphone=()") || !strings.Contains(pp, "geolocation=()") {
			t.Errorf("GET %s: Permissions-Policy = %q", path, pp)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		for _, d := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self' https://fonts.googleapis.com", "font-src https://fonts.gstatic.com",
			"img-src 'self' data:", "connect-src 'self'", "form-action 'self' https://wa.me", "frame-ancestors 'none'", "base-uri 'self'", "object-src 'none'"} {
			if !strings.Contains(csp, d) {
				t.Errorf("GET %s: CSP lacks %q: %s", path, d, csp)
			}
		}
		if strings.Contains(csp, "unsafe") {
			t.Errorf("CSP allows something unsafe: %s", csp)
		}
		if rec.Header().Get("Strict-Transport-Security") != "" {
			t.Errorf("HSTS is Caddy's job, got it from the app")
		}
	}
}

// The CSP has no 'unsafe-inline', so no page may rely on an inline style or script.
func TestPagesHaveNoInlineStyleOrScript(t *testing.T) {
	s := newTestServer(t)
	inline := regexp.MustCompile(`(?i)\sstyle=|<style|\son[a-z]+=`)
	script := regexp.MustCompile(`(?i)<script[^>]*>`)
	for _, path := range []string{"/", "/corporate-training", "/ai-nlp-bots", "/ai-nlp-bots/educators", "/produk-digital", "/tentang-kami",
		"/hubungi-kami", "/trainer-panel", "/daftar", "/log-masuk", "/log-keluar", "/dasar-privasi", "/terma", "/tiada-halaman"} {
		_, body := get(t, s, path)
		if m := inline.FindString(body); m != "" {
			t.Errorf("GET %s has inline %q", path, m)
		}
		for _, tag := range script.FindAllString(body, -1) {
			if !strings.Contains(tag, "src=") {
				t.Errorf("GET %s has inline script %q", path, tag)
			}
		}
	}
}

func TestAdminResponsesAreNotCached(t *testing.T) {
	s := newTestServer(t)
	if got := request(s, "/admin").Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("/admin Cache-Control = %q", got)
	}
}

func TestNotFoundPageIsStyledMalay(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/tiada-halaman", "/ai-nlp-bots/unknown", "/a/b/c"} {
		rec := request(s, path)
		body := rec.Body.String()
		if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s = %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		for _, want := range []string{"Halaman tidak dijumpai", `href="/"`, "https://wa.me/60123947200", "THE CAVE ACADEMY", `name="robots" content="noindex"`} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: 404 page lacks %q", path, want)
			}
		}
		if strings.Contains(body, "rel=\"canonical\"") {
			t.Errorf("GET %s: 404 page has a canonical link", path)
		}
	}
	rec := request(s, "/static/css/tiada.css")
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "THE CAVE ACADEMY") {
		t.Errorf("/static miss = %d, want the plain 404", rec.Code)
	}
}

func TestPanicIsRecoveredAndServes500Page(t *testing.T) {
	s := newTestServer(t)
	s.mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) { panic("kaboom") })
	logs := captureLog(t)
	rec := request(s, "/boom")
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "Sesuatu tidak kena") || !strings.Contains(body, "https://wa.me/60123947200") {
		t.Fatalf("panic response = %d\n%s", rec.Code, body)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("500 page lacks security headers")
	}
	if l := logs.String(); !strings.Contains(l, "kaboom") || !strings.Contains(l, "goroutine") || !strings.Contains(l, `"/boom" 500`) {
		t.Errorf("log lacks the panic, stack or access line:\n%s", l)
	}
	if code, _ := get(t, s, "/"); code != http.StatusOK { // the server keeps serving
		t.Errorf("after panic GET / = %d", code)
	}
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t)
	if code, body := get(t, s, "/healthz"); code != http.StatusOK || body != "ok" {
		t.Errorf("healthz = %d %q", code, body)
	}
	if _, body := get(t, s, "/robots.txt"); !strings.Contains(body, "Disallow: /healthz") {
		t.Error("robots.txt does not disallow /healthz")
	}
	s.db.Close()
	if code, _ := get(t, s, "/healthz"); code != http.StatusServiceUnavailable {
		t.Errorf("healthz with the db closed = %d, want 503", code)
	}
}

func TestAccessLogHasNoQueryCookiesOrBodies(t *testing.T) {
	s := newTestServer(t)
	logs := captureLog(t)
	req := httptest.NewRequest(http.MethodGet, "/tentang-kami?token=hunter2", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-secret"})
	req.RemoteAddr = "198.51.100.7:4000"
	s.ServeHTTP(httptest.NewRecorder(), req)
	post(s, "/hubungi-kami", url.Values{"message": {"body-secret"}})
	l := logs.String()
	if !strings.Contains(l, `GET "/tentang-kami" 200`) || !strings.Contains(l, "ip=198.51.100.7") || !strings.Contains(l, `POST "/hubungi-kami"`) {
		t.Errorf("access log incomplete:\n%s", l)
	}
	for _, secret := range []string{"hunter2", "cookie-secret", "body-secret"} {
		if strings.Contains(l, secret) {
			t.Errorf("log leaks %q:\n%s", secret, l)
		}
	}
}

func TestAccessLogQuotesThePath(t *testing.T) {
	s := newTestServer(t)
	logs := captureLog(t)
	request(s, "/x%0aFAKE%20LOG%20LINE")
	if strings.Contains(logs.String(), "\nFAKE") {
		t.Errorf("a newline in the path forged a log line:\n%s", logs.String())
	}
}

func TestBodyCapOnNonGet(t *testing.T) {
	s := newTestServer(t)
	big := signupForm("big@example.com") // valid, only too big
	big.Set("pad", strings.Repeat("x", 100<<10))
	if rec := post(s, "/daftar", big); rec.Code == http.StatusSeeOther { // the capped body parses as empty: refused either way
		t.Errorf("100 KB signup = %d, want a refusal", rec.Code)
	}
	if n := count(t, s, "users"); n != 0 {
		t.Errorf("oversized signup created %d users", n)
	}
	if rec := post(s, "/daftar", signupForm("ok@example.com")); rec.Code != http.StatusSeeOther {
		t.Errorf("normal signup = %d, want 303", rec.Code)
	}
}

func TestLoginWithJunkEmailIsNotKeptInTheLimiter(t *testing.T) {
	s := newTestServer(t)
	junk := strings.Repeat("a", 5000) + "@example.com"
	postFrom(s, "/log-masuk", loginForm(junk, "x"), "198.51.100.1:1")
	if n := len(s.limits.loginEmail.hits); n != 0 {
		t.Errorf("loginEmail holds %d junk keys", n)
	}
	if n := len(s.limits.loginIP.hits); n != 1 {
		t.Errorf("loginIP holds %d keys, want 1", n)
	}
}

func TestForwardedProtoOnlyTrustedFromLoopback(t *testing.T) {
	for _, c := range []struct {
		remote, proto string
		want          bool
	}{
		{"127.0.0.1:5", "https", true},
		{"[::1]:5", "https", true},
		{"198.51.100.1:5", "https", false}, // a direct client cannot claim https
		{"127.0.0.1:5", "http", false},
		{"127.0.0.1:5", "", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		if got := isHTTPS(r); got != c.want {
			t.Errorf("isHTTPS(%s, proto %q) = %v, want %v", c.remote, c.proto, got, c.want)
		}
	}
	s := newTestServer(t)
	rec := postFrom(s, "/daftar", signupForm("direct@example.com"), "198.51.100.1:5", "X-Forwarded-Proto", "https")
	if c := sessionOf(rec); c == nil || c.Secure {
		t.Errorf("spoofed header from a direct client gave cookie %+v, want not Secure", c)
	}
}

func TestClientIPGroupsIPv6By64(t *testing.T) {
	ip := func(remote string) string {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		return clientIP(r)
	}
	if a, b := ip("[2001:db8:1:2::1]:1"), ip("[2001:db8:1:2:ffff::9]:1"); a != b {
		t.Errorf("same /64 gave %q and %q", a, b)
	}
	if a, b := ip("[2001:db8:1:2::1]:1"), ip("[2001:db8:1:3::1]:1"); a == b {
		t.Errorf("different /64s share %q", a)
	}
	if got := ip("198.51.100.1:1"); got != "198.51.100.1" {
		t.Errorf("IPv4 = %q", got)
	}
}

func TestFormsAreRateLimitedPerIPAndOverall(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)
	for i := 0; i < 5; i++ {
		if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
			t.Fatalf("form %d = %d, want 303", i+1, rec.Code)
		}
	}
	sent := strings.Count(mail.String(), "--- email")
	if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.1:1"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("6th form = %d, want 429", rec.Code)
	} else if b := rec.Body.String(); !strings.Contains(b, "Terlalu banyak penghantaran borang") || !strings.Contains(b, "<html") {
		t.Errorf("429 is not the styled error page:\n%s", b)
	}
	for _, path := range []string{"/trainer-panel", "/corporate-training"} { // one budget across the three forms
		if rec := postFrom(s, path, trainerForm, "198.51.100.1:1"); rec.Code != http.StatusTooManyRequests {
			t.Errorf("POST %s over the limit = %d, want 429", path, rec.Code)
		}
	}
	if strings.Count(mail.String(), "--- email") != sent || count(t, s, "contact_messages") != 5 {
		t.Error("a refused form was saved or emailed")
	}
	if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.2:1"); rec.Code != http.StatusSeeOther {
		t.Errorf("other IP = %d, want 303", rec.Code)
	}

	s.abuse.formsDay.limit = 7 // 6 saved so far; one more, then the site-wide cap
	if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.3:1"); rec.Code != http.StatusSeeOther {
		t.Errorf("under the site-wide cap = %d, want 303", rec.Code)
	}
	if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.4:1"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the site-wide cap = %d, want 429", rec.Code)
	}
}

func TestInvalidFormsDoNotUseTheFormBudget(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 8; i++ {
		if rec := postFrom(s, "/hubungi-kami", with(contactForm, "email", "nope"), "198.51.100.1:1"); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid form %d = %d, want 422", i+1, rec.Code)
		}
	}
	if rec := postFrom(s, "/hubungi-kami", contactForm, "198.51.100.1:1"); rec.Code != http.StatusSeeOther {
		t.Errorf("valid form after invalid ones = %d, want 303", rec.Code)
	}
}

func TestChatPerIPLimit(t *testing.T) {
	g := newChatRig(t)
	a := signUp(t, g.s, "a@example.com")
	g.setPlan(t, "a@example.com", planPaid) // the daily free limit is not what is under test
	for i := 0; i < 30; i++ {
		if code, out := g.sendJSON("educators", "Soalan", a); code != http.StatusOK {
			t.Fatalf("post %d = %d %v", i+1, code, out)
		}
	}
	logs := captureLog(t)
	code, out := g.sendJSON("educators", "Soalan", a)
	if msg, _ := out["error"].(string); code != http.StatusTooManyRequests || msg != tooManyAttempts {
		t.Fatalf("31st post = %d %v, want 429", code, out)
	}
	if g.or.count() != 30 || !strings.Contains(logs.String(), "per-IP limit") {
		t.Errorf("limited post reached OpenRouter (%d calls) or was not logged:\n%s", g.or.count(), logs.String())
	}
	if body := g.send("educators", "Soalan", a).Body.String(); !strings.Contains(body, "Terlalu banyak cubaan") {
		t.Error("no-JS page does not show the limit message")
	}

	b := signUp(t, g.s, "b@example.com") // same default test IP as a, so the budget is shared
	if code, _ := g.sendJSON("educators", "Soalan", b); code != http.StatusTooManyRequests {
		t.Errorf("same IP, other user = %d, want 429", code)
	}
	g.clock = g.clock.Add(10*time.Minute + time.Second)
	if code, _ := g.sendJSON("educators", "Soalan", a); code != http.StatusOK {
		t.Errorf("after the window = %d, want 200", code)
	}
}

func TestChatGlobalDailyCap(t *testing.T) {
	g := newChatRig(t)
	g.s.config.ChatDailyCap = 3
	free1, free2, paid := signUp(t, g.s, "f1@example.com"), signUp(t, g.s, "f2@example.com"), signUp(t, g.s, "p@example.com")
	g.setPlan(t, "p@example.com", planPaid)

	for _, c := range []*http.Cookie{free1, free2, paid} { // paid users count toward the cap too
		if code, out := g.sendJSON("hr", "Soalan", c); code != http.StatusOK {
			t.Fatalf("under the cap: %d %v", code, out)
		}
	}
	logs := captureLog(t)
	for _, c := range []*http.Cookie{paid, free1} {
		code, out := g.sendJSON("hr", "Soalan", c)
		if msg, _ := out["error"].(string); code != http.StatusServiceUnavailable || !strings.Contains(msg, "sedang sibuk") || !strings.Contains(msg, "cuba lagi kemudian") {
			t.Errorf("over the cap = %d %v", code, out)
		}
	}
	if g.or.count() != 3 || g.rows(t, `1=1`) != 6 {
		t.Errorf("capped question reached OpenRouter or was stored: %d calls, %d rows", g.or.count(), g.rows(t, `1=1`))
	}
	if !strings.Contains(logs.String(), "daily cap of 3 reached") {
		t.Errorf("cap hit not logged:\n%s", logs.String())
	}
	if body := g.send("hr", "Soalan", paid).Body.String(); !strings.Contains(body, "sedang sibuk") {
		t.Error("no-JS page does not show the busy message")
	}

	g.clock = g.clock.Add(24 * time.Hour) // next KL day
	if code, _ := g.sendJSON("hr", "Soalan", paid); code != http.StatusOK {
		t.Errorf("next day = %d, want 200", code)
	}
}

func TestChatCapDefaultsTo2000(t *testing.T) {
	if got := newTestServer(t).chatCap(); got != 2000 {
		t.Errorf("default cap = %d", got)
	}
	t.Setenv("CHAT_DAILY_CAP", "50")
	if c := ConfigFromEnv(); c.ChatDailyCap != 50 {
		t.Errorf("CHAT_DAILY_CAP=50 gave %d", c.ChatDailyCap)
	}
	t.Setenv("CHAT_DAILY_CAP", "junk")
	if c := ConfigFromEnv(); c.ChatDailyCap != 2000 {
		t.Errorf("CHAT_DAILY_CAP=junk gave %d", c.ChatDailyCap)
	}
}

func TestProductionConfigValidation(t *testing.T) {
	ok := Config{Env: "production", OpenRouterKey: "k", BrevoKey: "b", BaseURL: "https://thecaveacademy.com", MailFrom: "The Cave Academy <noreply@thecaveacademy.com>"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid production config refused: %v", err)
	}
	for name, mut := range map[string]func(*Config){
		"OPENROUTER_API_KEY": func(c *Config) { c.OpenRouterKey = "" },
		"BREVO_API_KEY":      func(c *Config) { c.BrevoKey = "" },
		"BASE_URL":           func(c *Config) { c.BaseURL = "" },
		"MAIL_FROM":          func(c *Config) { c.MailFrom = "not an address" },
		"APP_ENV":            func(c *Config) { c.Env = "prod" },
	} {
		c := ok
		mut(&c)
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("breaking %s: err = %v", name, err)
		}
	}
	if err := (Config{Env: "development"}).Validate(); err != nil {
		t.Errorf("development with nothing set: %v", err)
	}
	if err := (Config{}).Validate(); err != nil {
		t.Errorf("zero config: %v", err)
	}
}

func TestConfigEnvFromEnvironment(t *testing.T) {
	for _, k := range []string{"APP_ENV", "BASE_URL", "OPENROUTER_API_KEY", "BREVO_API_KEY", "MAIL_FROM"} {
		t.Setenv(k, "")
	}
	if c := ConfigFromEnv(); c.Env != "development" || c.BaseURL != "https://thecaveacademy.com" || c.Validate() != nil {
		t.Errorf("development defaults = %+v", c)
	}
	t.Setenv("APP_ENV", "production")
	err := ConfigFromEnv().Validate()
	if err == nil || !strings.Contains(fmt.Sprint(err), "OPENROUTER_API_KEY, BREVO_API_KEY, BASE_URL") {
		t.Errorf("empty production env: err = %v", err)
	}
}
