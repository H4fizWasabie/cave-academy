package site

import "net/http"

func (s *Server) corporate(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Corporate Training & RFQ", Nav: "program", Crumb: "Corporate Training"})
}
