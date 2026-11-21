package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "time/tzdata" // Asia/Kuala_Lumpur must load on minimal servers too
)

const (
	freeDaily    = 3 // questions per KL day on the free plan
	planPaid     = "paid"
	maxMsgRunes  = 1000 // longest question
	historyTurns = 10   // stored turns sent to the model
	pageMessages = 50   // stored turns shown on the page
	maxTokens    = 800
	orTimeout    = 30 * time.Second
	orEndpoint   = "https://openrouter.ai/api/v1/chat/completions"
	dbTime       = "2006-01-02 15:04:05"
	waURL        = "https://wa.me/60123947200"
)

// chatOpts holds what tests replace. The zero value is production.
type chatOpts struct {
	URL  string           // OpenRouter endpoint; "" = orEndpoint
	Now  func() time.Time // clock; nil = time.Now
	busy sync.Map         // user ids with a question in flight
}

func (s *Server) now() time.Time {
	if s.chat.Now != nil {
		return s.chat.Now()
	}
	return time.Now()
}

var klZone = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Kuala_Lumpur"); err == nil {
		return l
	}
	return time.FixedZone("MYT", 8*3600) // no DST in Malaysia
}()

// chatMsg is one stored turn, ready to render: Role is "user" or "assistant".
type chatMsg struct {
	Role string
	Body template.HTML
}

// chatResult is the outcome of one send. Err is a user-facing Malay message
// with the HTTP status to answer with; Remaining is -1 on the paid plan.
type chatResult struct {
	Reply     string
	Err       string
	Status    int
	Remaining int
}

var errUpstream = errors.New("openrouter")

// remaining is the free questions the user has left today, -1 when unlimited.
func (s *Server) remaining(u *User) int {
	if u.Plan == planPaid {
		return -1
	}
	n := 0
	n0 := s.now().In(klZone)
	start := time.Date(n0.Year(), n0.Month(), n0.Day(), 0, 0, 0, 0, klZone).UTC().Format(dbTime)
	s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE user_id = ? AND role = 'user' AND created_at >= ?`, u.ID, start).Scan(&n)
	return max(0, freeDaily-n)
}

// chatHistory returns the user's last n stored turns with this bot, oldest first.
func (s *Server) chatHistory(u *User, bot Bot, n int) []orMessage {
	rows, err := s.db.Query(`SELECT role, content FROM (SELECT id, role, content FROM chat_messages
		WHERE user_id = ? AND bot = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, u.ID, bot.Slug, n)
	if err != nil {
		log.Printf("chat history: %v", err)
		return nil
	}
	defer rows.Close()
	var out []orMessage
	for rows.Next() {
		var m orMessage
		if rows.Scan(&m.Role, &m.Content) == nil {
			out = append(out, m)
		}
	}
	return out
}

// pageHistory is chatHistory rendered for the template.
func (s *Server) pageHistory(u *User, bot Bot) []chatMsg {
	var out []chatMsg
	for _, m := range s.chatHistory(u, bot, pageMessages) {
		out = append(out, chatMsg{Role: m.Role, Body: formatBody(m.Content)})
	}
	return out
}

// chatSend validates the question, enforces the daily limit, asks the model
// and stores the exchange. Nothing is stored unless the model answered, so a
// failed call never costs the user a question.
func (s *Server) chatSend(u *User, bot Bot, msg string) chatResult {
	res := func(status int, e string) chatResult {
		return chatResult{Err: e, Status: status, Remaining: s.remaining(u)}
	}
	switch n := utf8.RuneCountInString(msg); {
	case n == 0:
		return res(http.StatusBadRequest, "Sila taip soalan anda.")
	case n > maxMsgRunes:
		return res(http.StatusBadRequest, fmt.Sprintf("Soalan terlalu panjang (maksimum %d aksara).", maxMsgRunes))
	}
	if s.config.OpenRouterKey == "" {
		return res(http.StatusServiceUnavailable, "Maaf, chatbot belum dikonfigurasi. Sila cuba lagi kemudian atau hubungi kami di WhatsApp: "+waURL)
	}
	// One question at a time per user, so parallel requests cannot slip past the limit.
	if _, busy := s.chat.busy.LoadOrStore(u.ID, true); busy {
		return res(http.StatusTooManyRequests, "Sila tunggu jawapan kepada soalan sebelumnya.")
	}
	defer s.chat.busy.Delete(u.ID)
	if s.remaining(u) == 0 {
		return res(http.StatusTooManyRequests, fmt.Sprintf("Anda sudah menggunakan had soalan percuma hari ini (%d soalan sehari). Cuba lagi esok, atau hubungi kami di WhatsApp untuk pelan langganan tanpa had: %s", freeDaily, waURL))
	}

	turns := append([]orMessage{{"system", systemPrompt(bot)}}, s.chatHistory(u, bot, historyTurns)...)
	turns = append(turns, orMessage{"user", msg})
	reply, err := s.askModel(turns)
	if err != nil {
		log.Printf("chat: %v", err) // never includes the key
		return res(http.StatusBadGateway, "Maaf, bot tidak dapat menjawab sekarang. Soalan anda tidak dikira dalam had harian. Sila cuba lagi sebentar lagi.")
	}
	if err := s.storeExchange(u, bot, msg, reply); err != nil {
		log.Printf("chat store: %v", err)
		return res(http.StatusInternalServerError, "Maaf, berlaku ralat dalaman. Sila cuba lagi.")
	}
	return chatResult{Reply: reply, Remaining: s.remaining(u)}
}

func (s *Server) storeExchange(u *User, bot Bot, msg, reply string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := s.now().UTC().Format(dbTime)
	for _, m := range [][2]string{{"user", msg}, {"assistant", reply}} {
		if _, err := tx.Exec(`INSERT INTO chat_messages (user_id, bot, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			u.ID, bot.Slug, m[0], m[1], at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// orMessage is one chat completion message.
type orMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var orClient = &http.Client{Timeout: orTimeout}

// askModel makes one non-streaming OpenRouter chat completion and returns the
// assistant's text.
func (s *Server) askModel(messages []orMessage) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": s.config.OpenRouterModel, "messages": messages, "max_tokens": maxTokens})
	url := s.chat.URL
	if url == "" {
		url = orEndpoint
	}
	ctx, cancel := context.WithTimeout(context.Background(), orTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.OpenRouterKey)
	req.Header.Set("HTTP-Referer", "https://caveacademy.my")
	req.Header.Set("X-Title", "The Cave Academy")
	resp, err := orClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: request failed", errUpstream) // not err: its text must stay free of the URL/key
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d: %.200s", errUpstream, resp.StatusCode, raw)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: unreadable answer: %.200s", errUpstream, raw)
	}
	reply := strings.TrimSpace(out.Choices[0].Message.Content)
	if reply == "" {
		return "", fmt.Errorf("%w: empty answer", errUpstream)
	}
	return reply, nil
}

// chatPost handles POST /ai-nlp-bots/{slug}. A form post (no JS) is answered
// with a redirect back to the page, or the page again with the error and the
// draft; chat.js asks for JSON with Accept: application/json.
func (s *Server) chatPost(w http.ResponseWriter, r *http.Request, bot Bot) {
	if !sameOrigin(r) {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return
	}
	wantJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	u := s.currentUser(r)
	if u == nil {
		if wantJSON {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Sila log masuk untuk bertanya.", "login": "/log-masuk?next=" + r.URL.Path})
			return
		}
		s.requireUser(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Permintaan ditolak", http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(r.PostFormValue("message"))
	res := s.chatSend(u, bot, msg)
	switch {
	case wantJSON && res.Err != "":
		writeJSON(w, res.Status, map[string]any{"error": res.Err, "remaining": res.Remaining})
	case wantJSON:
		writeJSON(w, http.StatusOK, map[string]any{"reply": res.Reply, "remaining": res.Remaining})
	case res.Err != "":
		s.renderBot(w, r, res.Status, bot, u, res.Err, msg)
	default:
		http.Redirect(w, r, r.URL.Path+"#terkini", http.StatusSeeOther)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Reply text is shown escaped, with its line breaks kept (CSS white-space:
// pre-wrap), **bold** and bare http(s) links. chat.js does the same in the
// browser with textContent, so keep the two regexps in step.
var inlineRe = regexp.MustCompile(`\*\*[^*\n]+\*\*|https?://[^\s<>"'*]+`)

func formatBody(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range inlineRe.FindAllStringIndex(text, -1) {
		b.WriteString(html.EscapeString(text[last:m[0]]))
		tok := text[m[0]:m[1]]
		last = m[1]
		if strings.HasPrefix(tok, "**") {
			b.WriteString("<strong>" + html.EscapeString(tok[2:len(tok)-2]) + "</strong>")
			continue
		}
		link := strings.TrimRight(tok, ".,;:!?)]")
		last = m[0] + len(link) // punctuation after the link is plain text
		b.WriteString(`<a href="` + html.EscapeString(link) + `" target="_blank" rel="noopener noreferrer nofollow">` + html.EscapeString(link) + `</a>`)
	}
	b.WriteString(html.EscapeString(text[last:]))
	return template.HTML(b.String())
}
