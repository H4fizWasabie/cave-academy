package site

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func planForm1() url.Values {
	return url.Values{"action": {"save"}, "name": {"Pro"}, "price": {"29.90"}, "daily_questions": {"20"}, "days": {"30"},
		"bcl_link": {proLink}, "sort": {"1"}, "active": {"1"}}
}

func TestAdminPelanNeedsAnAdmin(t *testing.T) {
	s := newTestServer(t)
	if rec := request(s, "/admin/pelan"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/log-masuk?next=%2Fadmin%2Fpelan" {
		t.Errorf("logged out: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	c := signUp(t, s, "biasa@example.com")
	if rec := request(s, "/admin/pelan", c); rec.Code != http.StatusNotFound {
		t.Errorf("non-admin GET: %d, want 404", rec.Code)
	}
	if rec := authPost(t, s, "/admin/pelan", planForm1(), []*http.Cookie{c}); rec.Code != http.StatusNotFound || count(t, s, "plans") != 0 {
		t.Errorf("non-admin POST: %d, %d plans", rec.Code, count(t, s, "plans"))
	}
	if rec := authPost(t, s, "/admin/pelan", planForm1(), nil); rec.Code != http.StatusSeeOther || count(t, s, "plans") != 0 {
		t.Errorf("logged-out POST: %d, %d plans", rec.Code, count(t, s, "plans"))
	}
}

func TestAdminPelanRejectsCrossOrigin(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "admin@example.com")
	rec := authPost(t, s, "/admin/pelan", planForm1(), []*http.Cookie{c}, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || count(t, s, "plans") != 0 {
		t.Errorf("cross-origin POST: %d, %d plans", rec.Code, count(t, s, "plans"))
	}
	rec = authPost(t, s, "/admin/pelan", planForm1(), []*http.Cookie{c}, "Origin", "http://example.com")
	if rec.Code != http.StatusSeeOther || count(t, s, "plans") != 1 {
		t.Errorf("same-origin POST: %d, %d plans", rec.Code, count(t, s, "plans"))
	}
}

func TestAdminPelanCreateEditDeactivate(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "admin@example.com")
	ck := []*http.Cookie{c}

	if rec := request(s, "/admin/pelan", c); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belum ada pelan") || !strings.Contains(rec.Body.String(), "Bayaran BCL belum dipadankan") {
		t.Fatalf("empty admin page: %d", rec.Code)
	}

	if rec := authPost(t, s, "/admin/pelan", planForm1(), ck); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/pelan" {
		t.Fatalf("create: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	var id int64
	var name, link string
	var price, daily, days, sortN int
	var active bool
	if err := s.db.QueryRow(`SELECT id, name, price_sen, daily_questions, days, bcl_link, active, sort FROM plans`).Scan(&id, &name, &price, &daily, &days, &link, &active, &sortN); err != nil {
		t.Fatal(err)
	}
	if name != "Pro" || price != 2990 || daily != 20 || days != 30 || link != proLink || !active || sortN != 1 {
		t.Errorf("stored %q %d %d %d %q %v %d", name, price, daily, days, link, active, sortN)
	}
	if body := request(s, "/admin/pelan", c).Body.String(); !strings.Contains(body, "RM29.90") || !strings.Contains(body, proLink) {
		t.Error("admin page does not list the plan")
	}

	edit := planForm1()
	edit.Set("id", "1")
	edit.Set("name", "Pro Plus")
	edit.Set("price", "39")
	edit.Set("daily_questions", "0")
	edit.Del("active") // an unticked box is off
	if rec := authPost(t, s, "/admin/pelan", edit, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit: %d", rec.Code)
	}
	s.db.QueryRow(`SELECT name, price_sen, daily_questions, active FROM plans WHERE id = 1`).Scan(&name, &price, &daily, &active)
	if name != "Pro Plus" || price != 3900 || daily != 0 || active || count(t, s, "plans") != 1 {
		t.Errorf("after edit: %q %d %d %v, %d plans", name, price, daily, active, count(t, s, "plans"))
	}
	if body := request(s, "/pelan").Body.String(); strings.Contains(body, "Pro Plus") {
		t.Error("an inactive plan is on the public page")
	}

	on := url.Values{"action": {"toggle"}, "id": {"1"}, "active": {"1"}}
	if rec := authPost(t, s, "/admin/pelan", on, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("activate: %d", rec.Code)
	}
	if body := request(s, "/pelan").Body.String(); !strings.Contains(body, "Pro Plus") || !strings.Contains(body, "RM39") {
		t.Error("the reactivated plan is not on the public page")
	}
	off := url.Values{"action": {"toggle"}, "id": {"1"}, "active": {"0"}}
	authPost(t, s, "/admin/pelan", off, ck)
	s.db.QueryRow(`SELECT active FROM plans WHERE id = 1`).Scan(&active)
	if active {
		t.Error("deactivate did nothing")
	}
	if rec := authPost(t, s, "/admin/pelan", url.Values{"action": {"toggle"}, "id": {"99"}}, ck); rec.Code != http.StatusNotFound {
		t.Errorf("toggle unknown plan: %d", rec.Code)
	}
	unknown := planForm1()
	unknown.Set("id", "99")
	unknown.Set("bcl_link", "https://m.bcl.my/form/lain")
	if rec := authPost(t, s, "/admin/pelan", unknown, ck); rec.Code != http.StatusNotFound || count(t, s, "plans") != 1 {
		t.Errorf("edit unknown plan: %d", rec.Code)
	}
	if rec := authPost(t, s, "/admin/pelan", url.Values{"action": {"delete"}, "id": {"1"}}, ck); rec.Code != http.StatusBadRequest || count(t, s, "plans") != 1 {
		t.Errorf("unknown action: %d", rec.Code)
	}
}

func TestAdminPelanValidatesInput(t *testing.T) {
	s := newTestServer(t)
	c := adminCookie(t, s, "admin@example.com")
	ck := []*http.Cookie{c}
	authPost(t, s, "/admin/pelan", planForm1(), ck)
	for name, mod := range map[string]func(url.Values){
		"no name":        func(v url.Values) { v.Set("name", " ") },
		"long name":      func(v url.Values) { v.Set("name", strings.Repeat("x", 61)) },
		"zero price":     func(v url.Values) { v.Set("price", "0") },
		"price text":     func(v url.Values) { v.Set("price", "RM29") },
		"huge price":     func(v url.Values) { v.Set("price", "999999") },
		"negative daily": func(v url.Values) { v.Set("daily_questions", "-1") },
		"daily text":     func(v url.Values) { v.Set("daily_questions", "ramai") },
		"zero days":      func(v url.Values) { v.Set("days", "0") },
		"too many days":  func(v url.Values) { v.Set("days", "99999") },
		"bad sort":       func(v url.Values) { v.Set("sort", "x") },
		"http link":      func(v url.Values) { v.Set("bcl_link", "http://m.bcl.my/form/lain") },
		"other host":     func(v url.Values) { v.Set("bcl_link", "https://evil.com/form/lain") },
		"js link":        func(v url.Values) { v.Set("bcl_link", "javascript:alert(1)") },
		"no link":        func(v url.Values) { v.Set("bcl_link", "") },
		"same form":      func(v url.Values) { v.Set("bcl_link", "https://lain.bcl.my/form/Pro-Bulanan/") },
	} {
		v := planForm1()
		v.Set("name", "Lain")
		v.Set("bcl_link", "https://m.bcl.my/form/lain")
		mod(v)
		rec := authPost(t, s, "/admin/pelan", v, ck)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) || count(t, s, "plans") != 1 {
			t.Errorf("%s: %d, %d plans", name, rec.Code, count(t, s, "plans"))
		}
	}
	// A refused new plan keeps what was typed.
	v := planForm1()
	v.Set("name", "Tersimpan")
	v.Set("price", "abc")
	if body := authPost(t, s, "/admin/pelan", v, ck).Body.String(); !strings.Contains(body, `value="Tersimpan"`) {
		t.Error("the rejected form lost the name")
	}
	// Editing a plan may keep its own form.
	edit := planForm1()
	edit.Set("id", "1")
	if rec := authPost(t, s, "/admin/pelan", edit, ck); rec.Code != http.StatusSeeOther {
		t.Errorf("edit with its own link: %d", rec.Code)
	}
}

func TestAdminPelanGrantsAnUnmatchedPayment(t *testing.T) {
	g := newBCLRig(t)
	c := adminCookie(t, g.s, "admin@example.com")
	ck := []*http.Cookie{c}
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "baru@example.com", nil)
	g.hook(testBCLKey, hookBody("ORD1")) // no account yet
	body := request(g.s, "/admin/pelan", c).Body.String()
	if !strings.Contains(body, "ORD1") || !strings.Contains(body, "baru@example.com") || !strings.Contains(body, "RM29") || !strings.Contains(body, "tiada akaun") {
		t.Fatal("the unmatched payment is not listed")
	}
	grant := url.Values{"action": {"grant"}, "id": {"1"}, "email": {"baru@example.com"}, "plan_id": {"1"}}
	if rec := authPost(t, g.s, "/admin/pelan", grant, ck); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Tiada akaun") || g.subs("1=1") != 0 {
		t.Errorf("grant without an account: %d", rec.Code)
	}
	signUp(t, g.s, "baru@example.com")
	if rec := authPost(t, g.s, "/admin/pelan", grant, []*http.Cookie{c}, "Origin", "https://evil.example"); rec.Code != http.StatusForbidden || g.subs("1=1") != 0 {
		t.Errorf("cross-origin grant: %d", rec.Code)
	}
	if rec := authPost(t, g.s, "/admin/pelan", grant, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("grant: %d", rec.Code)
	}
	if g.subs("bcl_order = 'ORD1' AND plan_id = ?", pid) != 1 || count(t, g.s, "unmatched_payments") != 0 {
		t.Error("grant did not create the subscription and clear the row")
	}
	if rec := authPost(t, g.s, "/admin/pelan", grant, ck); rec.Code != http.StatusUnprocessableEntity || g.subs("1=1") != 1 {
		t.Errorf("granting twice: %d, %d subs", rec.Code, g.subs("1=1"))
	}
}

func TestAdminUsersListShowsTheSubscription(t *testing.T) {
	g := newBCLRig(t)
	c := adminCookie(t, g.s, "admin@example.com")
	signUp(t, g.s, "siti@example.com")
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	// The users list reads SQL's own clock, so build the dates from the real now.
	g.s.db.Exec(`INSERT INTO subscriptions (user_id, plan_id, starts_at, ends_at, bcl_order)
		SELECT id, ?, datetime('now', '-1 day'), datetime('now', '+9 days'), 'A' FROM users WHERE email = 'siti@example.com'`, pid)
	body := request(g.s, "/admin/pengguna", c).Body.String()
	if !strings.Contains(body, "Pro hingga 20") || !strings.Contains(body, "Langganan") {
		t.Error("users list does not show the active subscription")
	}
}
