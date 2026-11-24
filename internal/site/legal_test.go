package site

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLegalPagesRender(t *testing.T) {
	s := newTestServer(t)
	for path, wants := range map[string][]string{
		"/dasar-privasi": {"Dasar Privasi", "Akta Perlindungan Data Peribadi 2010", "OpenRouter", "Brevo", "adam.caveconsult@gmail.com", "Kemas kini terakhir: 24 November 2026", "PG0512993-H"},
		"/terma":         {"Terma Penggunaan", "3 soalan sehari", "bukan nasihat profesional", "undang-undang Malaysia", "Kemas kini terakhir: 24 November 2026", "Idea Niaga Ventures"},
	} {
		code, body := get(t, s, path)
		if code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, code)
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s missing %q", path, want)
			}
		}
	}
}

func TestFooterAndSignupLinkToLegalPages(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/", "/tentang-kami", "/daftar"} {
		_, body := get(t, s, path)
		for _, want := range []string{`href="/dasar-privasi"`, `href="/terma"`} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s missing %s", path, want)
			}
		}
	}
	_, body := get(t, s, "/daftar")
	if !strings.Contains(body, "Dengan mendaftar, anda bersetuju dengan") {
		t.Error("signup page missing consent line")
	}
	_, body = get(t, s, "/log-masuk")
	if strings.Contains(body, "Dengan mendaftar") {
		t.Error("login page must not show the signup consent line")
	}
}

func TestRobotsTxt(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/robots.txt")
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"User-agent: *", "Allow: /", "Disallow: /admin", "Disallow: /log-keluar", "Sitemap: https://thecaveacademy.com/sitemap.xml"} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt missing %q:\n%s", want, body)
		}
	}
}

func TestSitemapUsesBaseURL(t *testing.T) {
	s, err := New(Config{DBPath: filepath.Join(t.TempDir(), "t.db"), BaseURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<loc>https://example.test/</loc>", "<loc>https://example.test/corporate-training</loc>",
		"<loc>https://example.test/ai-nlp-bots/hr</loc>", "<loc>https://example.test/dasar-privasi</loc>", "<loc>https://example.test/terma</loc>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q", want)
		}
	}
	for _, bad := range []string{"/log-masuk", "/daftar", "/admin", "example.test//"} {
		if strings.Contains(body, bad) {
			t.Errorf("sitemap must not contain %q", bad)
		}
	}
}

func TestPageMetaTags(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/tentang-kami")
	for _, want := range []string{
		`<meta name="description" content="The Cave Academy ialah`,
		`<link rel="canonical" href="https://thecaveacademy.com/tentang-kami">`,
		`<meta property="og:url" content="https://thecaveacademy.com/tentang-kami">`,
		`<meta property="og:image" content="https://thecaveacademy.com/static/img/og-image.jpg">`,
		`<meta property="og:title" content="Tentang Kami | The Cave Academy">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="theme-color" content="#0f1a3c">`,
		`<link rel="apple-touch-icon" href="/static/img/apple-touch-icon.png">`,
		`<html lang="ms">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tentang page missing %s", want)
		}
	}
	// A page without its own description falls back to the site default.
	_, body = get(t, s, "/daftar")
	if !strings.Contains(body, `<meta name="description" content="The Cave Academy: pembangunan kompetensi`) {
		t.Error("signup page missing default description")
	}
}

func TestSEOImagesExist(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"/static/img/favicon.png", "/static/img/apple-touch-icon.png", "/static/img/og-image.jpg"} {
		if code, _ := get(t, s, p); code != http.StatusOK {
			t.Errorf("GET %s = %d", p, code)
		}
	}
}

func TestTentangShowsFounderAndFooterAddress(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/tentang-kami")
	if code != http.StatusOK {
		t.Fatalf("GET /tentang-kami = %d, want 200", code)
	}
	for _, want := range []string{"Coach Adam Daniel", "Pengiktirafan", "Lot 11B Bazar Enggang Keramat", "Dikuasakan oleh The Cave Consult", "facebook.com/share/1CKDRGWmXg"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /tentang-kami missing %q", want)
		}
	}
	for _, img := range regexp.MustCompile(`src="(/static/img/[^"]+)"`).FindAllStringSubmatch(body, -1) {
		if code, _ := get(t, s, img[1]); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", img[1], code)
		}
	}
}

func TestMenuOffersDaftarWhenLoggedOut(t *testing.T) {
	s := newTestServer(t)
	if body := request(s, "/").Body.String(); !strings.Contains(body, `<li class="nav__signup"><a href="/daftar">Daftar</a></li>`) {
		t.Error("logged-out menu has no Daftar link")
	}
	c := signUp(t, s, "ali@example.com")
	if body := request(s, "/", c).Body.String(); strings.Contains(body, "nav__signup") {
		t.Error("logged-in menu still offers Daftar")
	}
}
