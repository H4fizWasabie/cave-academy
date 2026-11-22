package site

import (
	"html/template"
	"net/http"
)

// Bot is one of the three NLP chatbots. Slug is the URL segment, Name the
// short name ("Educators"); the full title is "NLP Bot " + Name. Desc, Image,
// Icon (symbol id in partials.html) and CTA are for the landing page cards.
// Examples are the three starter questions on the bot's page. The bot's system
// prompt is server-side only (chat_prompts.go).
type Bot struct {
	Slug     string
	Name     string
	Desc     string
	Image    string
	Icon     string
	CTA      string
	Examples []string
}

var bots = []Bot{
	{Slug: "educators", Name: "Educators",
		Desc:  "Sokongan AI untuk guru dalam komunikasi, pengurusan kelas, motivasi dan kesejahteraan diri.",
		Image: "bot-educators.jpg", Icon: "i-mortarboard", CTA: "Terokai Educators Bot",
		Examples: []string{
			"Macam mana nak pastikan murid fokus dalam kelas?",
			"Bagaimana nak berkomunikasi dengan ibu bapa yang sukar?",
			"Saya rasa letih dan hilang semangat mengajar. Apa saya boleh buat?",
		}},
	{Slug: "sales-marketing", Name: "Sales & Marketing",
		Desc:  "Bantu tingkatkan kemahiran komunikasi, pengaruh, rundingan dan pencapaian jualan.",
		Image: "bot-sales-marketing.jpg", Icon: "i-target", CTA: "Terokai Sales & Marketing Bot",
		Examples: []string{
			"Macam mana nak bina rapport dengan pelanggan baharu dalam 5 minit pertama?",
			"Pelanggan kata harga mahal. Bagaimana nak jawab?",
			"Bagaimana nak tutup jualan tanpa nampak terlalu mendesak?",
		}},
	{Slug: "hr", Name: "HR & People Management",
		Desc:  "Sokongan untuk pengurusan manusia, motivasi, prestasi, konflik dan pembangunan pasukan.",
		Image: "bot-hr.jpg", Icon: "i-users", CTA: "Terokai HR Bot",
		Examples: []string{
			"Dua orang pekerja saya selalu berkonflik. Bagaimana nak tangani?",
			"Macam mana nak beri maklum balas prestasi yang sukar tanpa mematahkan semangat?",
			"Apa cara untuk membina semangat berpasukan dalam pasukan baharu?",
		}},
}

func findBot(slug string) (Bot, bool) {
	for _, b := range bots {
		if b.Slug == slug {
			return b, true
		}
	}
	return Bot{}, false
}

func (s *Server) botsLanding(w http.ResponseWriter, r *http.Request) {
	s.render(w, "bots", Page{Title: "AI NLP Bots", Description: "Tiga rakan AI berasaskan NLP untuk guru, pasukan jualan dan pengurus HR. Daftar percuma dan tanya 3 soalan sehari.", Nav: "bots", Crumb: "AI NLP Bots", CSS: []string{"bots"}, Data: bots})
}

// botView is Page.Data for bot.html. Logged-out visitors only get Bot and Next.
type botView struct {
	Bot       Bot
	LoggedIn  bool
	Next      string // this page, for the daftar / log-masuk links
	Messages  []chatMsg
	Free      bool // free plan: show the daily counter
	Remaining int
	Limit     int
	Error     template.HTML // message of a refused or failed send (no-JS path)
	Draft     string        // the refused message, so the user does not lose it
}

// botPage is /ai-nlp-bots/{slug}: GET shows the bot (and the chat when logged
// in), POST sends one message (chat.go).
func (s *Server) botPage(w http.ResponseWriter, r *http.Request) {
	bot, ok := findBot(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case isGet(r):
		s.renderBot(w, r, http.StatusOK, bot, s.currentUser(r), "", "")
	case r.Method == http.MethodPost:
		s.chatPost(w, r, bot)
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}

func (s *Server) renderBot(w http.ResponseWriter, r *http.Request, status int, bot Bot, u *User, errMsg, draft string) {
	v := botView{Bot: bot, Next: r.URL.Path, Limit: freeDaily, Draft: draft}
	if errMsg != "" {
		v.Error = formatBody(errMsg)
	}
	if u != nil {
		v.LoggedIn = true
		v.Messages = s.pageHistory(u, bot)
		v.Free = u.Plan != planPaid
		v.Remaining = s.remaining(u)
	}
	s.renderStatus(w, status, "bot", Page{Title: "NLP Bot " + bot.Name, Description: bot.Desc, Nav: "bots", Crumb: "AI NLP Bots", CSS: []string{"chat"}, User: u, Data: v})
}
