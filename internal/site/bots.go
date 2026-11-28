package site

import (
	"html/template"
	"net/http"
)

// Bot is one of the NLP chatbots. Slug is the URL segment, Name the
// short name ("Educators"); the full title is "NLP Bot " + Name. Desc, Image,
// Icon (symbol id in partials.html) and CTA are for the landing page cards.
// Examples are the three starter questions on the bot's page. Notice, when set,
// is a short statement shown on the bot's page (logged out and in). The bot's system
// prompt is server-side only (chat_prompts.go).
type Bot struct {
	Slug     string
	Name     string
	Desc     string
	Image    string
	Icon     string
	CTA      string
	Examples []string
	Notice   string
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
	{Slug: "stres", Name: "Pengurusan Stres",
		Desc:  "Teman AI untuk mengurus stres harian: tenangkan fikiran, ubah cara anda melihat tekanan dan ambil langkah kecil yang seterusnya.",
		Image: "bot-educators.jpg", Icon: "i-shield", CTA: "Terokai Bot Pengurusan Stres",
		Notice: "Bot ini bukan ahli terapi, kaunselor atau perkhidmatan perubatan. Jika anda dalam krisis, hubungi 999 atau Befrienders KL 03-7627 2929 (24 jam).",
		Examples: []string{
			"Saya rasa tertekan dengan beban kerja yang tak habis-habis. Apa saya boleh buat?",
			"Bila rasa terlalu banyak benda nak buat, macam mana nak tenangkan diri dan mula dari mana?",
			"Saya susah tidur sebab asyik risau fikir pasal esok. Ada cara untuk tenangkan fikiran?",
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
	s.render(w, "bots", Page{Title: "AI NLP Bots", Description: "Rakan AI berasaskan NLP untuk guru, pasukan jualan, pengurus HR dan pengurusan stres harian. Daftar percuma dan tanya 3 soalan sehari.", Nav: "bots", Crumb: "AI NLP Bots", CSS: []string{"bots"},
		Data: map[string]any{"Bots": bots, "WA": waLink("Salam, saya ingin tahu tentang pelan AI NLP Bots The Cave Academy.")}})
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
	PlanNote  string        // "Pelan Pro hingga 12/12/2026" while a subscription runs, else ""
}

// botPage is /ai-nlp-bots/{slug}: GET shows the bot (and the chat when logged
// in), POST sends one message (chat.go).
func (s *Server) botPage(w http.ResponseWriter, r *http.Request) {
	bot, ok := findBot(r.PathValue("slug"))
	if !ok {
		s.notFound(w, r)
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
		e := s.entitlementOf(u)
		v.Free, v.Limit, v.PlanNote = e.Limit >= 0, e.Limit, e.note()
		v.Remaining = s.remaining(u)
	}
	s.renderStatus(w, status, "bot", Page{Title: "NLP Bot " + bot.Name, Description: bot.Desc, Nav: "bots", Crumb: bot.Name, CrumbParent: "AI NLP Bots", CrumbParentURL: "/ai-nlp-bots", CSS: []string{"chat", "pelan"}, User: u, Data: v})
}
