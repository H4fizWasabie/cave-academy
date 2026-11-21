package site

import "net/http"

func (s *Server) daftar(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Daftar", Crumb: "Daftar"})
}

func (s *Server) logMasuk(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Log Masuk", Crumb: "Log Masuk"})
}

func (s *Server) logKeluar(w http.ResponseWriter, r *http.Request) {
	s.render(w, "stub", Page{Title: "Log Keluar", Crumb: "Log Keluar"})
}
