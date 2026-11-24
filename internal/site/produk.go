package site

import (
	"net/http"
	"net/url"
	"strings"
)

// waNumber is the client's WhatsApp number in wa.me format.
const waNumber = "60123947200"

// waLink returns a wa.me link that opens a chat with msg prefilled. Spaces
// are %20 (not "+") and every reserved character is escaped.
func waLink(msg string) string {
	return "https://wa.me/" + waNumber + "?text=" + strings.ReplaceAll(url.QueryEscape(msg), "+", "%20")
}

// Product is one digital product. v1 has no checkout, so each product links
// to WhatsApp; the BCL checkout can hook in later by slug.
type Product struct {
	Name        string
	Slug        string
	Category    string // Category.Slug
	Image       string // file name under /static/img/
	Description string
}

// Category is one of the three product groups.
type Category struct {
	Name, Slug, Image, Icon, Description, Button string
}

var produkCategories = []Category{
	{"Management Game / Simulasi Pengurusan", "management-game", "category-management-game.jpg", "i-gamepad",
		"Program simulasi interaktif untuk membangunkan kemahiran pengurusan, kepimpinan, penyelesaian masalah dan membuat keputusan.", "Tanya tentang Management Game"},
	{"Workbook & Modul", "workbook", "category-workbook.jpg", "i-book",
		"Bahan latihan lengkap, terstruktur dan mudah digunakan untuk program latihan, bengkel dan pembangunan pasukan.", "Tanya tentang Workbook & Modul"},
	{"eBook", "ebook", "category-ebook.jpg", "i-ebook",
		"Bahan rujukan digital yang padat, praktikal dan relevan dengan keperluan kepimpinan, NLP, AI dan pengurusan organisasi.", "Tanya tentang eBook"},
}

var produkList = []Product{
	{"Leadership 360°", "leadership-360", "ebook", "product-leadership-360.jpg", "Memimpin Manusia, Memacu Prestasi, Mencipta Impak"},
	{"NLP for Educators", "nlp-for-educators", "ebook", "product-nlp-educators.jpg", "Komunikasi, Motivasi dan Pembangunan Diri Warga Pendidik"},
	{"AI for Educators", "ai-for-educators", "ebook", "product-ai-educators.jpg", "Penggunaan AI Secara Praktikal, Selamat dan Berkesan"},
	{"DISC at Work", "disc-at-work", "workbook", "product-disc.jpg", "Memahami Personaliti, Meningkatkan Komunikasi dan Kerjasama Pasukan"},
	{"Strategic Management Game", "strategic-management-game", "management-game", "product-strategic-game.jpg", "Simulasi Pengurusan Strategik dan Operasi Organisasi"},
}

// produkItem and produkCat are the template views: the data plus its WhatsApp link.
type produkItem struct {
	Product
	WA string
}

type produkCat struct {
	Category
	WA string
}

func (s *Server) produk(w http.ResponseWriter, r *http.Request) {
	var items []produkItem
	for _, p := range produkList {
		items = append(items, produkItem{p, waLink("Salam, saya berminat dengan produk digital " + p.Name + ". Boleh saya dapatkan maklumat lanjut?")})
	}
	var cats []produkCat
	for _, c := range produkCategories {
		cats = append(cats, produkCat{c, waLink("Salam, saya berminat dengan kategori " + c.Name + ". Boleh saya dapatkan maklumat lanjut?")})
	}
	s.render(w, "produk", Page{
		Title: "Produk Digital", Description: "Management game, workbook, eBook dan bahan latihan digital daripada The Cave Academy untuk pembelajaran berterusan.", Nav: "produk", Crumb: "Produk Digital", CSS: []string{"produk"},
		Data: map[string]any{"Products": items, "Categories": cats, "WA": waLink("Salam, saya ingin tahu tentang produk digital The Cave Academy.")},
	})
}
