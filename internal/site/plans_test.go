package site

import (
	"net/http"
	"strings"
	"testing"
)

// subscribe gives the user a running (or, with days negative, expired) subscription at the rig's clock.
func (g *bclRig) subscribe(email string, planID int64, fromDays, toDays int, order string) {
	g.t.Helper()
	var uid int64
	if err := g.s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&uid); err != nil {
		g.t.Fatal(err)
	}
	f := func(d int) string { return g.clock.UTC().AddDate(0, 0, d).Format(dbTime) }
	if _, err := g.s.db.Exec(`INSERT INTO subscriptions (user_id, plan_id, starts_at, ends_at, bcl_order) VALUES (?, ?, ?, ?, ?)`,
		uid, planID, f(fromDays), f(toDays), order); err != nil {
		g.t.Fatal(err)
	}
}

func (g *bclRig) user(email string) *User {
	g.t.Helper()
	u := &User{Email: email}
	if err := g.s.db.QueryRow(`SELECT id, plan FROM users WHERE email = ?`, email).Scan(&u.ID, &u.Plan); err != nil {
		g.t.Fatal(err)
	}
	return u
}

func TestDailyLimitFollowsTheBestRunningTier(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	u := g.user("siti@example.com")
	if l := g.s.dailyLimit(u); l != freeDaily {
		t.Fatalf("no subscription: limit %d, want %d", l, freeDaily)
	}
	small := g.plan("Kecil", 900, 10, 30, "https://m.bcl.my/form/kecil")
	big := g.plan("Besar", 2900, 50, 30, "https://m.bcl.my/form/besar")
	all := g.plan("Tanpa Had", 9900, 0, 30, "https://m.bcl.my/form/tanpa-had")
	g.subscribe("siti@example.com", small, -1, 29, "A")
	if l := g.s.dailyLimit(u); l != 10 {
		t.Errorf("one tier: limit %d, want 10", l)
	}
	g.subscribe("siti@example.com", big, -1, 5, "B")
	if l := g.s.dailyLimit(u); l != 50 {
		t.Errorf("two tiers: limit %d, want the best, 50", l)
	}
	g.subscribe("siti@example.com", all, -1, 3, "C")
	if l := g.s.dailyLimit(u); l != -1 || g.s.remaining(u) != -1 {
		t.Errorf("unlimited tier: limit %d, remaining %d, want -1", l, g.s.remaining(u))
	}
	if e := g.s.entitlementOf(u); e.Plan != "Tanpa Had" {
		t.Errorf("entitlement plan = %q", e.Plan)
	}
}

func TestSubscriptionExpiryFallsBackToFree(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	u := g.user("siti@example.com")
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	g.subscribe("siti@example.com", pid, -10, 20, "A")
	if l := g.s.dailyLimit(u); l != 20 {
		t.Fatalf("running: %d", l)
	}
	g.clock = g.clock.AddDate(0, 0, 21)
	if l := g.s.dailyLimit(u); l != freeDaily {
		t.Errorf("after the end: limit %d, want %d", l, freeDaily)
	}
	// A subscription that has not started (a queued purchase) gives nothing yet.
	g.subscribe("siti@example.com", pid, 5, 35, "B")
	if l := g.s.dailyLimit(u); l != freeDaily {
		t.Errorf("not started: limit %d, want %d", l, freeDaily)
	}
}

func TestManualPaidSwitchStaysUnlimited(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.s.db.Exec(`UPDATE users SET plan = 'paid' WHERE email = 'siti@example.com'`)
	if l := g.s.dailyLimit(g.user("siti@example.com")); l != -1 {
		t.Errorf("manual paid: limit %d, want -1", l)
	}
}

func TestChatUsesTheTierLimitAndPointsToPelan(t *testing.T) {
	g := newChatRig(t)
	rig := &bclRig{t: t, s: g.s, clock: g.clock}
	c := signUp(t, g.s, "siti@example.com")
	pid := rig.plan("Pro", 2900, 5, 30, proLink)
	rig.subscribe("siti@example.com", pid, -1, 29, "A")
	for i := 0; i < 5; i++ {
		code, out := g.sendJSON("hr", "Soalan", c)
		if code != http.StatusOK || out["remaining"] != float64(4-i) {
			t.Fatalf("question %d: %d %v", i+1, code, out)
		}
	}
	code, out := g.sendJSON("hr", "Keenam", c)
	msg, _ := out["error"].(string)
	if code != http.StatusTooManyRequests || !strings.Contains(msg, "5 soalan sehari") || !strings.Contains(msg, "https://thecaveacademy.com/pelan") || !strings.Contains(msg, "https://wa.me/60123947200") {
		t.Errorf("6th question: %d %q", code, msg)
	}

	// The page shows the real counter and the plan with its end date (KL), not x/3.
	body := request(g.s, "/ai-nlp-bots/hr", c).Body.String()
	for _, want := range []string{"Baki soalan hari ini: 0/5", `data-limit="5"`, "Pelan Pro hingga 08/04/2026"} {
		if !strings.Contains(body, want) {
			t.Errorf("bot page missing %q", want)
		}
	}
}

func TestBotPageForUnlimitedTierHasNoCounter(t *testing.T) {
	g := newBCLRig(t)
	c := signUp(t, g.s, "siti@example.com")
	pid := g.plan("Tanpa Had", 9900, 0, 30, proLink)
	g.subscribe("siti@example.com", pid, -1, 29, "A")
	body := request(g.s, "/ai-nlp-bots/hr", c).Body.String()
	if strings.Contains(body, "data-count") || !strings.Contains(body, "Pelan Tanpa Had hingga") {
		t.Errorf("unlimited tier page: counter or plan note wrong")
	}
	if body := request(g.s, "/ai-nlp-bots/hr", signUp(t, g.s, "ali@example.com")).Body.String(); strings.Contains(body, "chat__plan") {
		t.Error("a free user sees a plan note")
	}
}

func TestPelanPageWithoutPlansSaysComingSoon(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/pelan")
	if code != http.StatusOK {
		t.Fatal(code)
	}
	for _, want := range []string{"Pelan langganan akan datang", "https://wa.me/60123947200", "/static/css/pelan.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty pricing page missing %q", want)
		}
	}
	if strings.Contains(body, "plan__price") {
		t.Error("plan cards shown with no plans")
	}
}

func TestPelanPageShowsActivePlans(t *testing.T) {
	g := newBCLRig(t)
	g.plan("Pro", 2950, 20, 30, proLink)
	g.plan("Tanpa Had", 9900, 0, 90, "https://m.bcl.my/form/tanpa-had")
	off := g.plan("Lama", 100, 1, 1, "https://m.bcl.my/form/lama")
	g.s.db.Exec(`UPDATE plans SET active = 0 WHERE id = ?`, off)

	_, body := get(t, g.s, "/pelan")
	for _, want := range []string{"Pro", "RM29.50", "20 soalan sehari", "30 hari", "Tanpa Had", "RM99", "Soalan tanpa had setiap hari", "90 hari",
		`href="/daftar?next=/pelan"`, "emel yang sama"} {
		if !strings.Contains(body, want) {
			t.Errorf("pricing page missing %q", want)
		}
	}
	if strings.Contains(body, "Lama") || strings.Contains(body, "akan datang") {
		t.Error("inactive plan shown, or the empty notice with plans")
	}
	if strings.Contains(body, proLink) {
		t.Error("a logged-out visitor is sent straight to BCL")
	}

	c := signUp(t, g.s, "siti@example.com")
	body = request(g.s, "/pelan", c).Body.String()
	if !strings.Contains(body, `href="`+proLink+`"`) || !strings.Contains(body, "siti@example.com") || strings.Contains(body, `href="/daftar?next=/pelan"`) {
		t.Error("logged-in pricing page: BCL link, account email or sign-up link wrong")
	}
}

func TestBotsBannerDoesNotPromiseASubscription(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/ai-nlp-bots")
	if strings.Contains(body, `href="/pelan"`) || !strings.Contains(body, "Daftar Akaun Percuma") {
		t.Error("logged-out banner should point to free sign-up, not /pelan")
	}
	c := signUp(t, s, "siti@example.com")
	body = request(s, "/ai-nlp-bots", c).Body.String()
	if !strings.Contains(body, "Tanya Pelan di WhatsApp") || strings.Contains(body, `href="/pelan"`) || strings.Contains(body, "Daftar akaun percuma") {
		t.Error("logged-in banner should point to WhatsApp and hide the sign-up note")
	}
}

func TestSitemapListsPelan(t *testing.T) {
	_, body := get(t, newTestServer(t), "/sitemap.xml")
	if !strings.Contains(body, "/pelan</loc>") {
		t.Error("sitemap misses /pelan")
	}
}

func TestPlanHelpers(t *testing.T) {
	for in, want := range map[string]int{"29": 2900, "29.5": 2950, "29.50": 2950, " 0.99 ": 99} {
		if got, ok := parseRM(in); !ok || got != want {
			t.Errorf("parseRM(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-1", "1,000", "1.234", "1.", "RM5", "abc"} {
		if _, ok := parseRM(in); ok {
			t.Errorf("parseRM(%q) accepted", in)
		}
	}
	for link, want := range map[string]bool{
		"https://merchant.bcl.my/form/pro": true, "https://bcl.my/form/pro/": true, "http://m.bcl.my/form/x": false,
		"https://evil.com/form/x": false, "https://bcl.my.evil.com/form/x": false, "https://bcl.my/": false, "https://u:p@m.bcl.my/form/x": false, "javascript:alert(1)": false,
	} {
		if validBCLLink(link) != want {
			t.Errorf("validBCLLink(%q) = %v, want %v", link, !want, want)
		}
	}
	if bclSlug("https://M.bcl.my/form/Pro-Bulanan/?a=1") != "pro-bulanan" {
		t.Error("bclSlug")
	}
}

func TestConfigValidateBCL(t *testing.T) {
	ok := Config{Env: "production", OpenRouterKey: "k", BrevoKey: "k", BaseURL: "https://x.my", MailFrom: "A <a@b.my>"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("no BCL settings: %v", err)
	}
	both := ok
	both.BCLWebhookKey, both.BCLAPIToken = strings.Repeat("k", 32), "t"
	if err := both.Validate(); err != nil {
		t.Errorf("both set: %v", err)
	}
	for name, mod := range map[string]func(*Config){
		"key only":   func(c *Config) { c.BCLWebhookKey = strings.Repeat("k", 32) },
		"token only": func(c *Config) { c.BCLAPIToken = "t" },
		"short key":  func(c *Config) { c.BCLWebhookKey, c.BCLAPIToken = "short", "t" },
	} {
		c := ok
		mod(&c)
		if c.Validate() == nil {
			t.Errorf("%s: production accepted it", name)
		}
	}
	dev := Config{BCLWebhookKey: "short"}
	if dev.Validate() != nil {
		t.Error("development refuses a BCL setting")
	}
	t.Setenv("BCL_WEBHOOK_KEY", "abc")
	t.Setenv("BCL_API_TOKEN", "tok")
	if c := ConfigFromEnv(); c.BCLWebhookKey != "abc" || c.BCLAPIToken != "tok" {
		t.Errorf("env not read: %+v", c)
	}
}

func TestEntitlementShowsTheFurthestStackedEnd(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	u := g.user("siti@example.com")
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	g.subscribe("siti@example.com", pid, -10, 20, "A")
	g.subscribe("siti@example.com", pid, 20, 50, "B") // renewal, not running yet
	want := g.clock.UTC().AddDate(0, 0, 50).Format(dbTime)
	if got := g.s.entitlementOf(u).Ends.Format(dbTime); got != want {
		t.Errorf("ends = %s, want %s", got, want)
	}
}
