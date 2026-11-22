package site

import "net/http"

// Both pages are static Malay text in templates/privasi.html and terma.html.
// Keep legalUpdated in step with the "Kemas kini terakhir" line when the text changes.
const legalUpdated = "22 November 2026"

func (s *Server) privasi(w http.ResponseWriter, r *http.Request) {
	s.render(w, "privasi", Page{
		Title: "Dasar Privasi", Crumb: "Dasar Privasi", CSS: []string{"legal"}, Data: legalUpdated,
		Description: "Dasar Privasi The Cave Academy: data peribadi yang kami kumpul, tujuan, pemproses data, tempoh simpanan dan hak anda di bawah PDPA 2010.",
	})
}

func (s *Server) terma(w http.ResponseWriter, r *http.Request) {
	s.render(w, "terma", Page{
		Title: "Terma Penggunaan", Crumb: "Terma Penggunaan", CSS: []string{"legal"}, Data: legalUpdated,
		Description: "Terma Penggunaan The Cave Academy: had jawapan AI, penggunaan yang dibenarkan, had 3 soalan sehari, akaun dan liabiliti.",
	})
}
