package site

import "net/http"

func (s *Server) hubungi(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Hubungi Kami", Nav: "", Crumb: "Hubungi Kami"})
}
