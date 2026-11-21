package site

import "net/http"

// Bot is one of the three NLP chatbots. Slug is the URL segment.
type Bot struct {
	Slug string
	Name string
}

var bots = []Bot{
	{Slug: "educators", Name: "Educators"},
	{Slug: "sales-marketing", Name: "Sales & Marketing"},
	{Slug: "hr", Name: "HR & People Management"},
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
	s.render(w, "stub", Page{Title: "AI NLP Bots", Nav: "bots", Crumb: "AI NLP Bots"})
}

func (s *Server) botPage(w http.ResponseWriter, r *http.Request) {
	bot, ok := findBot(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, "stub", Page{Title: "NLP Bot " + bot.Name, Nav: "bots", Crumb: "AI NLP Bots", Data: bot})
}
