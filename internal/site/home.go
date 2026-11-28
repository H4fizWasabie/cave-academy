package site

import "net/http"

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, "home", Page{Title: "Utama", Description: "Platform latihan, rangkaian trainer profesional, AI NLP Bots dan produk digital untuk membangunkan kompetensi individu, pasukan dan organisasi.", Nav: "home", CSS: []string{"home"}, Data: map[string]any{"Bots": bots}})
}
