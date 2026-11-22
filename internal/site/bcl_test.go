package site

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testBCLKey   = "0123456789abcdef0123456789abcdef" // 32 chars
	testBCLToken = "bcl-token-secret"
	proLink      = "https://merchant.bcl.my/form/pro-bulanan"
)

// bclRig is a Server wired to a fake BCL API. orders and forms are what the fake knows.
type bclRig struct {
	t      *testing.T
	s      *Server
	srv    *httptest.Server
	clock  time.Time
	mu     sync.Mutex
	orders map[string]map[string]any
	forms  []map[string]any
	reqs   int
}

func newBCLRig(t *testing.T) *bclRig {
	t.Helper()
	g := &bclRig{t: t, s: newTestServer(t), orders: map[string]map[string]any{}, clock: time.Date(2026, 3, 10, 10, 0, 0, 0, testKL),
		forms: []map[string]any{{"form_id": 7, "slug": "pro-bulanan"}, {"form_id": 8, "slug": "lain"}}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reqs++
		if r.Header.Get("Authorization") != "Bearer "+testBCLToken {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/forms":
			json.NewEncoder(w).Encode(map[string]any{"data": g.forms})
		case strings.HasPrefix(r.URL.Path, "/transactions/"):
			o, ok := g.orders[strings.TrimPrefix(r.URL.Path, "/transactions/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(o)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.srv.Close)
	g.s.config.BCLWebhookKey, g.s.config.BCLAPIToken, g.s.config.BCLAPIURL = testBCLKey, testBCLToken, g.srv.URL
	g.s.chat.Now = func() time.Time { return g.clock }
	return g
}

// order registers what BCL says about an order; fields override the paid, live RM29 default on form 7.
func (g *bclRig) order(number, email string, over map[string]any) {
	m := map[string]any{"order_number": number, "payer_email": email, "transaction_type": "live",
		"subtotal_amount": "29.00", "amount": "30.50", "is_paid": 1, "status": 3}
	for k, v := range over {
		m[k] = v
	}
	var form any = 7
	if f, ok := m["form"]; ok {
		form = f
		delete(m, "form")
	}
	g.orders[number] = map[string]any{"data": map[string]any{"formable_id": form, "main_data": m}}
}

func (g *bclRig) plan(name string, priceSen, daily, days int, link string) int64 {
	g.t.Helper()
	res, err := g.s.db.Exec(`INSERT INTO plans (name, price_sen, daily_questions, days, bcl_link) VALUES (?, ?, ?, ?, ?)`, name, priceSen, daily, days, link)
	if err != nil {
		g.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// hook posts a webhook body for order to /bcl/webhook/<key>.
func (g *bclRig) hook(key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/bcl/webhook/"+key, strings.NewReader(body))
	rec := httptest.NewRecorder()
	g.s.ServeHTTP(rec, req)
	return rec
}

func hookBody(order string) string {
	return `{"event":"payment-success","data":{"main_data":{"order_number":"` + order + `"}}}`
}

func (g *bclRig) subs(where string, args ...any) int {
	g.t.Helper()
	var n int
	if err := g.s.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE `+where, args...).Scan(&n); err != nil {
		g.t.Fatal(err)
	}
	return n
}

func (g *bclRig) unmatched() (n int, reason string, plan any) {
	g.t.Helper()
	g.s.db.QueryRow(`SELECT COUNT(*) FROM unmatched_payments`).Scan(&n)
	g.s.db.QueryRow(`SELECT reason, plan_id FROM unmatched_payments ORDER BY id DESC LIMIT 1`).Scan(&reason, &plan)
	return
}

func TestBCLWebhookNeedsTheRightKey(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "siti@example.com", nil)
	for name, key := range map[string]string{"wrong": strings.Repeat("x", 32), "empty tail": "", "short": "short"} {
		if rec := g.hook(key, hookBody("ORD1")); rec.Code != http.StatusNotFound {
			t.Errorf("%s key: %d, want 404", name, rec.Code)
		}
	}
	g.s.config.BCLWebhookKey = ""
	if rec := g.hook("", hookBody("ORD1")); rec.Code != http.StatusNotFound {
		t.Errorf("unset key: %d, want 404", rec.Code)
	}
	g.s.config.BCLWebhookKey, g.s.config.BCLAPIToken = "short", "tok" // under 32 characters is off even when it matches
	if rec := g.hook("short", hookBody("ORD1")); rec.Code != http.StatusNotFound {
		t.Errorf("short key matching: %d, want 404", rec.Code)
	}
	if g.reqs != 0 || g.subs("1=1") != 0 {
		t.Errorf("a refused call reached BCL (%d) or granted (%d)", g.reqs, g.subs("1=1"))
	}
	if rec := request(g.s, "/bcl/webhook/"+testBCLKey); rec.Code != http.StatusNotFound {
		t.Errorf("GET on the webhook: %d, want 404", rec.Code)
	}
}

func TestBCLPaidOrderGrantsOnceAndRepeatIsNoOp(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	signUp(t, g.s, "attacker@example.com")
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "  Siti@Example.COM ", nil)

	// The body claims another payer and a bigger sum: only BCL's own detail counts.
	body := `{"event":"payment-success","data":{"main_data":{"order_number":"ORD1","payer_email":"attacker@example.com","amount":"999"}}}`
	if rec := g.hook(testBCLKey, body); rec.Code != http.StatusOK {
		t.Fatalf("first webhook = %d", rec.Code)
	}
	var uid int64
	var starts, ends string
	var plan int64
	if err := g.s.db.QueryRow(`SELECT user_id, plan_id, starts_at, ends_at FROM subscriptions WHERE bcl_order = 'ORD1'`).Scan(&uid, &plan, &starts, &ends); err != nil {
		t.Fatalf("no subscription: %v", err)
	}
	var siti int64
	g.s.db.QueryRow(`SELECT id FROM users WHERE email = 'siti@example.com'`).Scan(&siti)
	if uid != siti || plan != pid || starts != "2026-03-10 02:00:00" || ends != "2026-04-09 02:00:00" {
		t.Errorf("granted to user %d plan %d %s..%s", uid, plan, starts, ends)
	}
	if rec := g.hook(testBCLKey, hookBody("ORD1")); rec.Code != http.StatusOK || g.subs("1=1") != 1 {
		t.Errorf("repeat: %d, %d subscriptions, want 200 and 1", rec.Code, g.subs("1=1"))
	}
	if n, _, _ := g.unmatched(); n != 0 {
		t.Errorf("%d unmatched payments", n)
	}
}

func TestBCLSecondPurchaseOfSamePlanRunsOn(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "siti@example.com", nil)
	g.order("ORD2", "siti@example.com", nil)
	g.hook(testBCLKey, hookBody("ORD1"))
	g.clock = g.clock.AddDate(0, 0, 10)
	g.hook(testBCLKey, hookBody("ORD2"))
	var ends string
	g.s.db.QueryRow(`SELECT ends_at FROM subscriptions WHERE bcl_order = 'ORD2'`).Scan(&ends)
	if ends != "2026-05-09 02:00:00" { // 30 days after the first one's end, 9 Apr
		t.Errorf("second ends_at = %s, want 2026-05-09 02:00:00", ends)
	}
}

func TestBCLOrdersThatAreNotConfirmedAreRetried(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("UNPAID", "siti@example.com", map[string]any{"is_paid": 0})
	g.order("PENDING", "siti@example.com", map[string]any{"status": 1})
	g.order("TEST", "siti@example.com", map[string]any{"transaction_type": "test"})
	g.order("OTHER", "siti@example.com", nil)
	g.orders["MISMATCH"] = g.orders["OTHER"] // BCL's detail names another order number than the webhook's
	g.order("NOFORM", "siti@example.com", map[string]any{"form": ""})
	for _, o := range []string{"UNPAID", "PENDING", "TEST", "MISMATCH", "NOFORM", "UNKNOWN"} {
		if rec := g.hook(testBCLKey, hookBody(o)); rec.Code != http.StatusInternalServerError {
			t.Errorf("order %s: %d, want 500", o, rec.Code)
		}
	}
	g.srv.Close() // BCL unreachable
	if rec := g.hook(testBCLKey, hookBody("OTHER")); rec.Code != http.StatusInternalServerError {
		t.Errorf("BCL down: %d, want 500", rec.Code)
	}
	if g.subs("1=1") != 0 {
		t.Error("an unconfirmed order granted a subscription")
	}
}

func TestBCLBadTokenIsRetried(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "siti@example.com", nil)
	g.s.config.BCLAPIToken = "wrong"
	if rec := g.hook(testBCLKey, hookBody("ORD1")); rec.Code != http.StatusInternalServerError || g.subs("1=1") != 0 {
		t.Errorf("refused token: %d", rec.Code)
	}
}

func TestBCLIgnoresOtherEventsAndTestModeBodies(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "siti@example.com", nil)
	for _, body := range []string{
		`{"event":"payment-failed","data":{"main_data":{"order_number":"ORD1"}}}`,
		`{"event":"payment-success","data":{"main_data":{"order_number":"ORD1","transaction_type":"test"}}}`,
	} {
		if rec := g.hook(testBCLKey, body); rec.Code != http.StatusOK {
			t.Errorf("%s: %d, want 200", body, rec.Code)
		}
	}
	if g.reqs != 0 || g.subs("1=1") != 0 {
		t.Errorf("ignored events reached BCL (%d) or granted", g.reqs)
	}
	for _, body := range []string{"not json", `{"event":"payment-success","data":{"main_data":{}}}`} {
		if rec := g.hook(testBCLKey, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", body, rec.Code)
		}
	}
}

func TestBCLUnderpaidAndUnknownFormAreKeptForAnAdmin(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)

	g.order("LOW", "siti@example.com", map[string]any{"subtotal_amount": "10.00", "amount": "10.50"})
	if rec := g.hook(testBCLKey, hookBody("LOW")); rec.Code != http.StatusOK {
		t.Fatalf("underpaid: %d", rec.Code)
	}
	if n, reason, plan := g.unmatched(); n != 1 || !strings.Contains(reason, "kurang") || plan == nil {
		t.Errorf("underpaid kept as %d %q plan %v", n, reason, plan)
	}
	// A coupon: the subtotal is enough but the discount takes it under the price.
	g.order("COUPON", "siti@example.com", map[string]any{"subtotal_amount": "29.00", "discount_amount": "5.00", "amount": "24.50"})
	g.hook(testBCLKey, hookBody("COUPON"))

	g.order("FORM8", "siti@example.com", map[string]any{"form": 8})
	g.order("FORM99", "siti@example.com", map[string]any{"form": 99})
	for _, o := range []string{"FORM8", "FORM99"} {
		if rec := g.hook(testBCLKey, hookBody(o)); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", o, rec.Code)
		}
	}
	if n, reason, plan := g.unmatched(); n != 4 || !strings.Contains(reason, "bukan pelan") || plan != nil {
		t.Errorf("unknown form kept as %d %q plan %v", n, reason, plan)
	}
	if g.subs("1=1") != 0 {
		t.Error("an underpaid or unknown-form order granted a subscription")
	}
}

func TestBCLPayerWithoutAccountIsRecordedAndGrantedLater(t *testing.T) {
	g := newBCLRig(t)
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "Baru@Example.com", nil)
	if rec := g.hook(testBCLKey, hookBody("ORD1")); rec.Code != http.StatusOK {
		t.Fatalf("no account: %d", rec.Code)
	}
	var email string
	var sen int
	if err := g.s.db.QueryRow(`SELECT email, paid_sen FROM unmatched_payments WHERE bcl_order = 'ORD1'`).Scan(&email, &sen); err != nil || email != "baru@example.com" || sen != 2900 {
		t.Fatalf("unmatched = %q %d, %v", email, sen, err)
	}
	if g.subs("1=1") != 0 {
		t.Error("granted to nobody")
	}
	// A retry after signing up grants it and clears the unmatched row.
	signUp(t, g.s, "baru@example.com")
	if rec := g.hook(testBCLKey, hookBody("ORD1")); rec.Code != http.StatusOK || g.subs("bcl_order = 'ORD1'") != 1 {
		t.Errorf("retry after signup: %d, %d subs", rec.Code, g.subs("bcl_order = 'ORD1'"))
	}
	if n, _, _ := g.unmatched(); n != 0 {
		t.Errorf("%d unmatched left after the grant", n)
	}
}

func TestBCLInactivePlanStillHonoursAPaidOrder(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	pid := g.plan("Pro", 2900, 20, 30, proLink)
	g.s.db.Exec(`UPDATE plans SET active = 0 WHERE id = ?`, pid)
	g.order("ORD1", "siti@example.com", nil)
	g.hook(testBCLKey, hookBody("ORD1"))
	if g.subs("plan_id = ?", pid) != 1 {
		t.Error("a payment on a just-deactivated plan was dropped")
	}
}

func TestBCLWebhookKeyAndTokenNeverReachTheLog(t *testing.T) {
	g := newBCLRig(t)
	signUp(t, g.s, "siti@example.com")
	g.plan("Pro", 2900, 20, 30, proLink)
	g.order("ORD1", "siti@example.com", nil)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(testLogOut) })
	g.hook(testBCLKey, hookBody("ORD1"))
	g.hook(strings.Repeat("z", 32), hookBody("ORD1"))
	g.s.config.BCLAPIToken = "wrong"
	g.hook(testBCLKey, hookBody("ORD1"))
	out := buf.String()
	if !strings.Contains(out, "/bcl/webhook/") || strings.Contains(out, testBCLKey) || strings.Contains(out, strings.Repeat("z", 32)) ||
		strings.Contains(out, testBCLToken) || strings.Contains(out, "wrong") || strings.Contains(out, "siti@example.com") {
		t.Errorf("log leaks a secret or an email:\n%s", out)
	}
}

var testLogOut = log.Writer()
