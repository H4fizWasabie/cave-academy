package site

import "net/http"

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, "home", Page{Title: "Utama", Nav: "home", CSS: []string{"home"}})
}
