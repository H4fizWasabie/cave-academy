package site

import "net/http"

func (s *Server) produk(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Produk Digital", Nav: "produk", Crumb: "Produk Digital"})
}
