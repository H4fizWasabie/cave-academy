package site

import "net/http"

// Bot is one of the three NLP chatbots. Slug is the URL segment, Name the
// short name ("Educators"); the full title is "NLP Bot " + Name. Desc, Image,
// Icon (symbol id in partials.html) and CTA are for the landing page cards.
type Bot struct {
	Slug  string
	Name  string
	Desc  string
	Image string
	Icon  string
	CTA   string
}

var bots = []Bot{
	{Slug: "educators", Name: "Educators",
		Desc:  "Sokongan AI untuk guru dalam komunikasi, pengurusan kelas, motivasi dan kesejahteraan diri.",
		Image: "bot-educators.jpg", Icon: "i-mortarboard", CTA: "Terokai Educators Bot"},
	{Slug: "sales-marketing", Name: "Sales & Marketing",
		Desc:  "Bantu tingkatkan kemahiran komunikasi, pengaruh, rundingan dan pencapaian jualan.",
		Image: "bot-sales-marketing.jpg", Icon: "i-target", CTA: "Terokai Sales & Marketing Bot"},
	{Slug: "hr", Name: "HR & People Management",
		Desc:  "Sokongan untuk pengurusan manusia, motivasi, prestasi, konflik dan pembangunan pasukan.",
		Image: "bot-hr.jpg", Icon: "i-users", CTA: "Terokai HR Bot"},
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
	s.render(w, "bots", Page{Title: "AI NLP Bots", Nav: "bots", Crumb: "AI NLP Bots", CSS: []string{"bots"}, Data: bots})
}

func (s *Server) botPage(w http.ResponseWriter, r *http.Request) {
	bot, ok := findBot(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, "stub", Page{Title: "NLP Bot " + bot.Name, Nav: "bots", Crumb: "AI NLP Bots", Data: bot})
}
