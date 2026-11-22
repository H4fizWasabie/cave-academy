package site

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

const (
	defaultBaseURL     = "https://caveacademy.my"
	defaultDescription = "The Cave Academy: pembangunan kompetensi berasaskan AI, NLP dan pengalaman praktikal untuk individu, pasukan dan organisasi."
)

// seoMeta is what the layout needs for the canonical link and the Open Graph
// and Twitter tags. render fills it in; handlers only set Page.Description.
type seoMeta struct {
	Canonical   string
	Image       string
	Description string
}

// baseURL is the public origin without a trailing slash.
func (s *Server) baseURL() string {
	if b := strings.TrimRight(s.config.BaseURL, "/"); b != "" {
		return b
	}
	return defaultBaseURL
}

func (s *Server) fillSEO(p *Page, r *http.Request, status int) {
	p.SEO.Description = p.Description
	if p.SEO.Description == "" {
		p.SEO.Description = defaultDescription
	}
	p.SEO.Image = s.baseURL() + "/static/img/og-image.jpg"
	if status == http.StatusOK {
		p.SEO.Canonical = s.baseURL() + r.URL.Path
	}
}

// sitemapPaths are the public, indexable pages. Account, admin and form
// result pages are left out.
var sitemapPaths = []string{
	"/", "/corporate-training", "/ai-nlp-bots", "/ai-nlp-bots/educators", "/ai-nlp-bots/sales-marketing",
	"/ai-nlp-bots/hr", "/produk-digital", "/tentang-kami", "/hubungi-kami", "/trainer-panel",
	"/dasar-privasi", "/terma", "/pelan", "/trainer",
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /admin\nDisallow: /log-keluar\nDisallow: /healthz\n\nSitemap: %s/sitemap.xml\n", s.baseURL())
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n")
	paths := sitemapPaths
	if rows, err := s.db.Query(`SELECT slug FROM trainer_profiles WHERE published = 1 AND status = ? ORDER BY slug`, statusCaveTrainer); err != nil {
		log.Printf("sitemap: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var slug string
			if rows.Scan(&slug) == nil {
				paths = append(paths[:len(paths):len(paths)], "/trainer/"+slug)
			}
		}
	}
	for _, p := range paths {
		fmt.Fprintf(w, "  <url><loc>%s%s</loc></url>\n", s.baseURL(), p)
	}
	fmt.Fprint(w, "</urlset>\n")
}
