package site

import "net/http"

func (s *Server) trainerPanel(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Trainer Panel", Nav: "program", Crumb: "Trainer Panel"})
}
