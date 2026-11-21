package site

import "net/http"

func (s *Server) tentang(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Tentang Kami", Nav: "tentang", Crumb: "Tentang Kami"})
}
