package site

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOpenRouter stands in for openrouter.ai: it records every request and
// answers with reply, or with status when that is not 200.
type fakeOpenRouter struct {
	mu     sync.Mutex
	calls  []map[string]any
	header http.Header
	status int
	reply  string
}

func (f *fakeOpenRouter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeOpenRouter) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// chatRig is a test server wired to a fake OpenRouter and a controllable clock.
type chatRig struct {
	t     *testing.T
	s     *Server
	or    *fakeOpenRouter
	clock time.Time
}

var testKL = time.FixedZone("MYT", 8*3600)

func newChatRig(t *testing.T) *chatRig {
	t.Helper()
	rig := &chatRig{t: t, s: newTestServer(t), or: &fakeOpenRouter{status: 200, reply: "Jawapan ujian."},
		clock: time.Date(2026, 3, 10, 10, 0, 0, 0, testKL)}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		rig.or.mu.Lock()
		rig.or.calls = append(rig.or.calls, body)
		rig.or.header = r.Header.Clone()
		status, reply := rig.or.status, rig.or.reply
		rig.or.mu.Unlock()
		if status != http.StatusOK {
			http.Error(w, `{"error":{"message":"boom"}}`, status)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": reply}}}})
	}))
	t.Cleanup(fake.Close)
	rig.s.config.OpenRouterKey = "sk-test-key"
	rig.s.config.OpenRouterModel = "test/model"
	rig.s.chat.URL = fake.URL
	rig.s.chat.Now = func() time.Time { return rig.clock }
	return rig
}

// send posts a chat message as the cookie's user (form post, no JS).
func (g *chatRig) send(slug, msg string, c *http.Cookie, hdr ...string) *httptest.ResponseRecorder {
	return authPost(g.t, g.s, "/ai-nlp-bots/"+slug, url.Values{"message": {msg}}, []*http.Cookie{c}, hdr...)
}

// sendJSON posts like chat.js does.
func (g *chatRig) sendJSON(slug, msg string, c *http.Cookie) (int, map[string]any) {
	rec := g.send(slug, msg, c, "Accept", "application/json")
	out := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (g *chatRig) rows(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := g.s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (g *chatRig) setPlan(t *testing.T, email, plan string) {
	t.Helper()
	if _, err := g.s.db.Exec(`UPDATE users SET plan = ? WHERE email = ?`, plan, email); err != nil {
		t.Fatal(err)
	}
}

func (g *chatRig) userID(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	if err := g.s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBotPageLoggedOutShowsBotAndSignupCard(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/ai-nlp-bots/educators")
	if code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	for _, want := range []string{
		"NLP Bot Educators", "Sokongan AI untuk guru", "bot-educators.jpg",
		"Daftar percuma untuk bertanya 3 soalan sehari",
		`href="/daftar?next=%2fai-nlp-bots%2feducators"`, `href="/log-masuk?next=%2fai-nlp-bots%2feducators"`,
	} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Errorf("logged-out bot page missing %q", want)
		}
	}
	b, _ := findBot("educators")
	if len(b.Examples) != 3 {
		t.Fatalf("educators has %d example questions, want 3", len(b.Examples))
	}
	for _, q := range b.Examples {
		if !strings.Contains(body, html.EscapeString(q)) {
			t.Errorf("example question %q not shown", q)
		}
	}
	if strings.Contains(body, "<textarea") {
		t.Error("logged-out visitor sees a chat box")
	}
}

func TestChatHistoryIsPerUserAndPerBot(t *testing.T) {
	g := newChatRig(t)
	siti := signUp(t, g.s, "siti@example.com")
	ali := signUp(t, g.s, "ali@example.com")
	sid := g.userID(t, "siti@example.com")
	for i, m := range [][2]string{{"user", "Soalan rahsia Siti"}, {"assistant", "Jawapan untuk Siti"}} {
		g.s.db.Exec(`INSERT INTO chat_messages (user_id, bot, role, content, created_at) VALUES (?, 'educators', ?, ?, ?)`,
			sid, m[0], m[1], fmt.Sprintf("2026-03-0%d 01:00:00", i+1))
	}

	_, body := get(t, g.s, "/ai-nlp-bots/educators") // logged out
	if strings.Contains(body, "Soalan rahsia Siti") {
		t.Error("history visible to a visitor")
	}
	if body = request(g.s, "/ai-nlp-bots/educators", siti).Body.String(); !strings.Contains(body, "Soalan rahsia Siti") || !strings.Contains(body, "Jawapan untuk Siti") {
		t.Error("own history not shown")
	}
	if !strings.Contains(request(g.s, "/ai-nlp-bots/educators", siti).Body.String(), "Baki soalan hari ini: 3/3") {
		t.Error("old messages count against today")
	}
	for _, other := range []struct {
		slug string
		c    *http.Cookie
	}{{"educators", ali}, {"hr", siti}} {
		if body := request(g.s, "/ai-nlp-bots/"+other.slug, other.c).Body.String(); strings.Contains(body, "Siti") && strings.Contains(body, "rahsia") {
			t.Errorf("history leaked into %s for another user or bot", other.slug)
		}
	}
}

func TestSendStoresAndAnswers(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")

	rec := g.send("educators", "  Macam mana nak tenangkan kelas?  ", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ai-nlp-bots/educators#terkini" {
		t.Fatalf("send = %d -> %q, want 303 -> /ai-nlp-bots/educators#terkini", rec.Code, rec.Header().Get("Location"))
	}
	var role, content string
	rows, _ := g.s.db.Query(`SELECT role, content FROM chat_messages ORDER BY id`)
	var got []string
	for rows.Next() {
		rows.Scan(&role, &content)
		got = append(got, role+": "+content)
	}
	rows.Close()
	if strings.Join(got, "|") != "user: Macam mana nak tenangkan kelas?|assistant: Jawapan ujian." {
		t.Errorf("stored = %q", got)
	}
	body := request(g.s, "/ai-nlp-bots/educators", c).Body.String()
	if !strings.Contains(body, "Macam mana nak tenangkan kelas?") || !strings.Contains(body, "Jawapan ujian.") ||
		!strings.Contains(body, `id="terkini"`) || !strings.Contains(body, "Baki soalan hari ini: 2/3") {
		t.Error("page after sending does not show the exchange, the anchor and the new counter")
	}
}

func TestRequestSentToOpenRouter(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	uid := g.userID(t, "siti@example.com")
	// 7 old exchanges = 14 stored turns; only the last 10 may be sent.
	for i := 1; i <= 7; i++ {
		for _, role := range []string{"user", "assistant"} {
			g.s.db.Exec(`INSERT INTO chat_messages (user_id, bot, role, content, created_at) VALUES (?, 'educators', ?, ?, '2026-03-01 00:00:00')`,
				uid, role, fmt.Sprintf("%s %d", role, i))
		}
	}
	g.s.db.Exec(`INSERT INTO chat_messages (user_id, bot, role, content) VALUES (?, 'hr', 'user', 'dari bot lain')`, uid)

	if rec := g.send("educators", "Soalan baharu", c); rec.Code != http.StatusSeeOther {
		t.Fatalf("send = %d", rec.Code)
	}
	if h := g.or.header; h.Get("Authorization") != "Bearer sk-test-key" || h.Get("HTTP-Referer") != "https://caveacademy.my" ||
		h.Get("X-Title") != "The Cave Academy" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}
	req := g.or.last()
	if req["model"] != "test/model" || req["max_tokens"] != float64(800) || req["stream"] == true {
		t.Errorf("model/max_tokens/stream = %v %v %v", req["model"], req["max_tokens"], req["stream"])
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 12 { // system + 10 history + new
		t.Fatalf("%d messages sent, want 12", len(msgs))
	}
	first := msgs[0].(map[string]any)
	sys, _ := first["content"].(string)
	if first["role"] != "system" || !strings.Contains(sys, "NLP") || !strings.Contains(sys, "Educators") ||
		!strings.Contains(sys, "/corporate-training") || !strings.Contains(sys, "/produk-digital") || !strings.Contains(sys, "https://wa.me/60123947200") {
		t.Errorf("system prompt = %.200q", sys)
	}
	text := func(i int) string {
		m := msgs[i].(map[string]any)
		return m["role"].(string) + ": " + m["content"].(string)
	}
	if text(1) != "user: user 3" || text(10) != "assistant: assistant 7" || text(11) != "user: Soalan baharu" {
		t.Errorf("history window wrong: %q ... %q ... %q", text(1), text(10), text(11))
	}
}

func TestEachBotHasItsOwnPrompt(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	seen := map[string]string{}
	for slug, want := range map[string]string{"educators": "guru", "sales-marketing": "jualan", "hr": "pasukan"} {
		g.send(slug, "Hai", c)
		sys := g.or.last()["messages"].([]any)[0].(map[string]any)["content"].(string)
		if !strings.Contains(sys, want) {
			t.Errorf("%s prompt lacks %q", slug, want)
		}
		seen[sys] = slug
	}
	if len(seen) != 3 {
		t.Error("bots share a system prompt")
	}
}

func TestFreeUserLimitedToThreePerKLDay(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.clock = time.Date(2026, 3, 10, 23, 58, 0, 0, testKL) // two minutes before KL midnight

	for i, slug := range []string{"educators", "hr", "sales-marketing"} { // the limit is across bots
		code, out := g.sendJSON(slug, "Soalan", c)
		if code != http.StatusOK || out["remaining"] != float64(2-i) {
			t.Fatalf("question %d: %d %v", i+1, code, out)
		}
	}
	code, out := g.sendJSON("educators", "Soalan keempat", c)
	msg, _ := out["error"].(string)
	if code != http.StatusTooManyRequests || !strings.Contains(msg, "had soalan") || !strings.Contains(msg, "https://wa.me/60123947200") || out["remaining"] != float64(0) {
		t.Errorf("4th question: %d %v", code, out)
	}
	if g.or.count() != 3 || g.rows(t, `1=1`) != 6 {
		t.Errorf("4th question reached OpenRouter or was stored: %d calls, %d rows", g.or.count(), g.rows(t, `1=1`))
	}
	if body := g.send("hr", "Soalan keempat", c).Body.String(); !strings.Contains(body, "had soalan") || !strings.Contains(body, "Soalan keempat") {
		t.Error("no-JS refusal does not show the message and keep the draft")
	}

	g.clock = time.Date(2026, 3, 11, 0, 0, 1, 0, testKL) // next KL day
	if code, out := g.sendJSON("educators", "Hari baharu", c); code != http.StatusOK || out["remaining"] != float64(2) {
		t.Errorf("after KL midnight: %d %v", code, out)
	}
}

func TestOtherUsersDoNotShareTheLimit(t *testing.T) {
	g := newChatRig(t)
	siti, ali := signUp(t, g.s, "siti@example.com"), signUp(t, g.s, "ali@example.com")
	for i := 0; i < 3; i++ {
		g.sendJSON("hr", "Soalan", siti)
	}
	if code, _ := g.sendJSON("hr", "Soalan", ali); code != http.StatusOK {
		t.Errorf("Ali refused because of Siti's questions: %d", code)
	}
}

func TestPaidUserIsUnlimited(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.setPlan(t, "siti@example.com", "paid")
	for i := 0; i < 5; i++ {
		if code, out := g.sendJSON("hr", "Soalan", c); code != http.StatusOK || out["remaining"] != float64(-1) {
			t.Fatalf("paid question %d: %d %v", i+1, code, out)
		}
	}
	if body := request(g.s, "/ai-nlp-bots/hr", c).Body.String(); strings.Contains(body, "Baki soalan hari ini") {
		t.Error("paid user sees the free counter")
	}
}

func TestOpenRouterFailureDoesNotConsumeAQuestion(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.or.status = http.StatusInternalServerError

	code, out := g.sendJSON("hr", "Soalan penting", c)
	msg, _ := out["error"].(string)
	if code != http.StatusBadGateway || !strings.Contains(msg, "Maaf") || out["remaining"] != float64(3) {
		t.Errorf("failure: %d %v", code, out)
	}
	if strings.Contains(msg, "boom") || strings.Contains(msg, "sk-test-key") {
		t.Errorf("upstream detail leaked: %q", msg)
	}
	if n := g.rows(t, `1=1`); n != 0 {
		t.Errorf("%d rows stored after a failed call", n)
	}
	rec := g.send("hr", "Soalan penting", c) // no-JS: error shown, draft kept
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Soalan penting") || !strings.Contains(rec.Body.String(), "Maaf") {
		t.Errorf("no-JS failure = %d", rec.Code)
	}
	g.or.status = http.StatusOK
	for i := 0; i < 3; i++ { // all three questions are still available
		if code, _ := g.sendJSON("hr", "Soalan", c); code != http.StatusOK {
			t.Fatalf("question %d after failures refused: %d", i+1, code)
		}
	}
}

func TestMalformedUpstreamAnswerIsAFailure(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.or.reply = "   "
	if code, _ := g.sendJSON("hr", "Soalan", c); code != http.StatusBadGateway || g.rows(t, `1=1`) != 0 {
		t.Errorf("empty answer: %d, %d rows", code, g.rows(t, `1=1`))
	}
}

func TestMissingKeyAnswersNotConfigured(t *testing.T) {
	g := newChatRig(t)
	g.s.config.OpenRouterKey = ""
	c := signUp(t, g.s, "siti@example.com")
	code, out := g.sendJSON("hr", "Soalan", c)
	msg, _ := out["error"].(string)
	if code != http.StatusServiceUnavailable || !strings.Contains(msg, "belum dikonfigurasi") || out["remaining"] != float64(3) {
		t.Errorf("no key: %d %v", code, out)
	}
	if g.or.count() != 0 || g.rows(t, `1=1`) != 0 {
		t.Error("no key still called the API or stored the question")
	}
}

func TestCrossOriginPostIsRefused(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	rec := g.send("hr", "Soalan", c, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || g.or.count() != 0 || g.rows(t, `1=1`) != 0 {
		t.Errorf("cross-origin = %d, %d calls, %d rows", rec.Code, g.or.count(), g.rows(t, `1=1`))
	}
	if rec := g.send("hr", "Soalan", c, "Origin", "http://example.com"); rec.Code != http.StatusSeeOther {
		t.Errorf("same-origin = %d", rec.Code)
	}
}

func TestLoggedOutPostIsNotAnswered(t *testing.T) {
	g := newChatRig(t)
	rec := authPost(t, g.s, "/ai-nlp-bots/hr", url.Values{"message": {"Hai"}}, nil)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/log-masuk?next=") {
		t.Errorf("form post logged out = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = authPost(t, g.s, "/ai-nlp-bots/hr", url.Values{"message": {"Hai"}}, nil, "Accept", "application/json")
	if rec.Code != http.StatusUnauthorized || g.or.count() != 0 {
		t.Errorf("json post logged out = %d", rec.Code)
	}
}

func TestMessageIsValidated(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	for name, msg := range map[string]string{"empty": "", "blank": " \n\t ", "too long": strings.Repeat("a", 1001)} {
		if code, out := g.sendJSON("hr", msg, c); code != http.StatusBadRequest || out["error"] == nil || out["remaining"] != float64(3) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if g.or.count() != 0 || g.rows(t, `1=1`) != 0 {
		t.Error("invalid message reached OpenRouter or the DB")
	}
	if code, _ := g.sendJSON("hr", strings.Repeat("é", 1000), c); code != http.StatusOK { // 1000 characters, 2000 bytes
		t.Errorf("1000-character message refused: %d", code)
	}
}

func TestUnknownBotPostIs404(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	if rec := g.send("tiada", "Hai", c); rec.Code != http.StatusNotFound {
		t.Errorf("unknown bot POST = %d", rec.Code)
	}
}

func TestRepliesAreEscapedNeverRawHTML(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.or.reply = "<script>alert(1)</script> **tebal** lihat https://example.com/a?b=1&c=2. [x](javascript:alert(1))\nbaris dua"
	g.send("hr", `<img src=x onerror=alert(2)>`, c)
	body := request(g.s, "/ai-nlp-bots/hr", c).Body.String()
	for _, bad := range []string{"<script>alert(1)", "<img src=x", `href="javascript:`} {
		if strings.Contains(body, bad) {
			t.Errorf("page contains raw %q", bad)
		}
	}
	for _, want := range []string{
		"&lt;script&gt;alert(1)&lt;/script&gt;", "<strong>tebal</strong>", "&lt;img src=x onerror=alert(2)&gt;",
		`<a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer nofollow">https://example.com/a?b=1&amp;c=2</a>.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestChatPageHasNoSecrets(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	g.send("hr", "Hai", c)
	for _, path := range []string{"/ai-nlp-bots/hr", "/static/js/chat.js", "/static/css/chat.css"} {
		rec := request(g.s, path, c)
		b, _ := io.ReadAll(rec.Body)
		if rec.Code != http.StatusOK || strings.Contains(string(b), "sk-test-key") || strings.Contains(string(b), "Anda ialah") {
			t.Errorf("%s: %d, leaks a secret or the system prompt", path, rec.Code)
		}
	}
}

func TestChatPageWiring(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	body := request(g.s, "/ai-nlp-bots/sales-marketing", c).Body.String()
	for _, want := range []string{
		`<form`, `method="post"`, `<textarea`, `name="message"`, `maxlength="1000"`, "Baki soalan hari ini: 3/3",
		"/static/css/chat.css", "/static/js/chat.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
}
