// Package site is the whole Cave Academy web app: routes, templates, DB, mail.
//
// Layout of the package: shared plumbing lives here (server.go, config.go,
// db.go, mail.go); each page owns one handler file (home.go, corporate.go,
// bots.go, produk.go, tentang.go, hubungi.go, trainer.go, auth.go), one
// template in templates/<name>.html and one stylesheet static/css/<page>.css.
package site

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
)

//go:embed templates static
var assets embed.FS

// Server holds the shared dependencies every page handler uses.
type Server struct {
	db        *sql.DB
	config    Config
	mailer    *Mailer
	templates map[string]*template.Template
	mux       *http.ServeMux
	chat      chatOpts    // chat.go; the zero value is production
	limits    *rateLimits // ratelimit.go: login and signup throttling
}

// Page is the data every template receives as ".". Handlers fill it in;
// page-specific values go in Data.
type Page struct {
	Title string   // <title> and page heading; "" gives the plain site title
	Nav   string   // active nav item: home, program, bots, produk, tentang ("" for none)
	CSS   []string // extra stylesheets, e.g. "home" loads /static/css/home.css
	Crumb string   // breadcrumb label for {{template "breadcrumb" .}}; "" on the home page
	User  *User    // logged-in user or nil; render fills it in, handlers leave it alone
	Data  any
}

// New opens the database, loads the templates and registers every route.
func New(cfg Config) (*Server, error) {
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Server{
		db:     db,
		config: cfg,
		mailer: &Mailer{APIKey: cfg.BrevoKey, From: cfg.MailFrom},
		mux:    http.NewServeMux(),
		limits: newRateLimits(),
	}
	if s.templates, err = loadTemplates(); err != nil {
		db.Close()
		return nil, fmt.Errorf("templates: %w", err)
	}
	s.routes()
	return s, nil
}

func (s *Server) Close() error { return s.db.Close() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(&reqWriter{w, r}, r) // reqWriter lets render find the logged-in user
}

// routes registers every v1 route. Patterns without a method accept GET and
// POST so a page's owner can handle its form in the same handler.
func (s *Server) routes() {
	staticFS, _ := fs.Sub(assets, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Home.
	s.mux.HandleFunc("GET /{$}", s.home)

	// Corporate Training & RFQ.
	s.mux.HandleFunc("/corporate-training", s.corporate)

	// AI NLP Bots: landing and one chat page per bot (slug in bots.go).
	s.mux.HandleFunc("/ai-nlp-bots", s.botsLanding)
	s.mux.HandleFunc("/ai-nlp-bots/{slug}", s.botPage)

	// Produk Digital.
	s.mux.HandleFunc("/produk-digital", s.produk)

	// Tentang Kami.
	s.mux.HandleFunc("/tentang-kami", s.tentang)

	// Hubungi Kami.
	s.mux.HandleFunc("/hubungi-kami", s.hubungi)

	// Trainer Panel signup.
	s.mux.HandleFunc("/trainer-panel", s.trainerPanel)

	// Accounts.
	s.mux.HandleFunc("/daftar", s.daftar)
	s.mux.HandleFunc("/log-masuk", s.logMasuk)
	s.mux.HandleFunc("/log-keluar", s.logKeluar)

	// Admin back office (admin.go): admins only, anyone else gets a 404.
	s.mux.HandleFunc("/admin", s.adminOverview)
	s.mux.HandleFunc("/admin/{page}", s.adminPage)
}

// loadTemplates parses layout.html + partials.html together with each page
// template, one set per page, so pages can all define "content" without
// clashing. The map key is the page file name without ".html".
func loadTemplates() (map[string]*template.Template, error) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	out := map[string]*template.Template{}
	for _, f := range files {
		name := path.Base(f)
		if name == "layout.html" || name == "partials.html" {
			continue
		}
		t, err := template.ParseFS(assets, "templates/layout.html", "templates/partials.html", f)
		if err != nil {
			return nil, err
		}
		out[name[:len(name)-len(".html")]] = t
	}
	return out, nil
}

// render executes templates/<name>.html inside the layout with status 200.
func (s *Server) render(w http.ResponseWriter, name string, p Page) {
	s.renderStatus(w, http.StatusOK, name, p)
}

func (s *Server) renderStatus(w http.ResponseWriter, status int, name string, p Page) {
	t, ok := s.templates[name]
	if !ok {
		log.Printf("render: no template %q", name)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if rw, ok := w.(*reqWriter); ok && p.User == nil {
		p.User = s.currentUser(rw.r)
	}
	var buf bytes.Buffer // buffer so a template error cannot leave half a page
	if err := t.ExecuteTemplate(&buf, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}
