package site

import (
	"net/http"
	"strings"
	"testing"
)

func TestHomeRevampContent(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	for _, want := range []string{
		"Diiktiraf &amp; berdaftar", "Penyedia Latihan Berdaftar HRD Corp", "Jurulatih Bertauliah JPK",
		"Produk Digital", "NLP Bot</span> Educators", "NLP Bot</span> Pengurusan Stres",
		`href="/ai-nlp-bots/hr"`, "Coach Adam Daniel", `href="/tentang-kami"`, "Kenali Kami",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home missing %q", want)
		}
	}
	if strings.Contains(body, "Digital Products") {
		t.Error("home still has the English title Digital Products")
	}
}
