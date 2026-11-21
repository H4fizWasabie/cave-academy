package site

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newTestServer builds a Server on a temp SQLite file. Reuse it in page tests.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{DBPath: filepath.Join(t.TempDir(), "test.db"), MailFrom: "Test <test@example.com>", AdminEmail: "admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// get requests path from s and returns status and body.
func get(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func TestEveryRouteResponds200(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{
		"/", "/corporate-training", "/ai-nlp-bots",
		"/ai-nlp-bots/educators", "/ai-nlp-bots/sales-marketing", "/ai-nlp-bots/hr",
		"/produk-digital", "/tentang-kami", "/hubungi-kami", "/trainer-panel",
		"/daftar", "/log-masuk", "/log-keluar", "/static/css/base.css",
	} {
		if code, _ := get(t, s, path); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, code)
		}
	}
}

func TestUnknownPathsAre404(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/ai-nlp-bots/unknown", "/tiada-halaman"} {
		if code, _ := get(t, s, path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

func TestStubPagesSayComingSoon(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/tentang-kami")
	if !strings.Contains(body, "Halaman ini akan datang") {
		t.Error("stub page missing placeholder text")
	}
}

func TestHomeHasKeyContent(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/")
	for _, want := range []string{
		"Pembangunan Kompetensi", "AI, NLP &amp; Pengalaman Praktikal",
		"Terokai Program Latihan", "Lihat Pengenalan (2 Minit)",
		"Penyelesaian Untuk Anda", "Corporate Training &amp; RFQ", "AI NLP Bots", "Digital Products",
		"Berminat Menjadi Trainer Panel", `href="/trainer-panel"`,
		`href="/corporate-training"`, `href="/ai-nlp-bots"`, `href="/produk-digital"`,
		"EMPOWER MINDS. SHAPE LEADERS.", "https://wa.me/60123947200", "012-394 7200", "adam.caveconsult@gmail.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page missing %q", want)
		}
	}
}

func TestActiveNavItemIsMarked(t *testing.T) {
	s := newTestServer(t)
	for path, label := range map[string]string{"/": "Utama", "/ai-nlp-bots/hr": "AI NLP Bots", "/produk-digital": "Produk Digital"} {
		_, body := get(t, s, path)
		re := regexp.MustCompile(`aria-current="page"[^>]*>\s*` + regexp.QuoteMeta(label))
		if !re.MatchString(body) {
			t.Errorf("GET %s: nav item %q not marked active", path, label)
		}
		if n := strings.Count(body, `aria-current="page"`); n != 1 {
			t.Errorf("GET %s: %d active nav items, want 1", path, n)
		}
	}
}

func TestHomeAssetsExist(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/")
	paths := regexp.MustCompile(`(?:src|href)="(/static/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(paths) == 0 {
		t.Fatal("no static assets referenced")
	}
	for _, m := range paths {
		if code, _ := get(t, s, m[1]); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", m[1], code)
		}
	}
}
