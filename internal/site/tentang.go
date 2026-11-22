package site

import "net/http"

func (s *Server) tentang(w http.ResponseWriter, r *http.Request) {
	s.render(w, "tentang", Page{Title: "Tentang Kami", Description: "The Cave Academy ialah platform pembangunan kompetensi yang menggabungkan AI, NLP dan pengalaman latihan praktikal. Empower Minds. Shape Leaders.", Nav: "tentang", Crumb: "Tentang Kami", CSS: []string{"pages"}})
}
