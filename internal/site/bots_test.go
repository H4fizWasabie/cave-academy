package site

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestBotsLandingHasKeyContent(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/ai-nlp-bots")
	if code != http.StatusOK {
		t.Fatalf("GET /ai-nlp-bots = %d, want 200", code)
	}
	for _, want := range []string{
		"Berteraskan NLP", "Menyokong Komunikasi", "Membantu Penyelesaian Masalah", "Mudah &amp; Praktikal",
		"Terokai AI NLP Bots", "Cuba Bot Percuma", `href="#senarai-bot"`,
		"Pilihan AI NLP Bots",
		"NLP Bot</span> Educators", "NLP Bot</span> Sales &amp; Marketing", "NLP Bot</span> HR &amp; People Management", "NLP Bot</span> Pengurusan Stres",
		`href="/ai-nlp-bots/educators"`, `href="/ai-nlp-bots/sales-marketing"`, `href="/ai-nlp-bots/hr"`, `href="/ai-nlp-bots/stres"`, "Terokai Bot Pengurusan Stres",
		"3 soalan sehari", `href="/daftar"`,
		"Bagaimana Ia Membantu Anda?", "Produktiviti", "Cuba AI NLP Bots", "Daftar Akaun Percuma", `href="/daftar?next=/ai-nlp-bots"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bots landing missing %q", want)
		}
	}
}

func TestBotsLandingAssetsExist(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/ai-nlp-bots")
	for _, m := range regexp.MustCompile(`(?:src|href)="(/static/[^"]+)"`).FindAllStringSubmatch(body, -1) {
		if code, _ := get(t, s, m[1]); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", m[1], code)
		}
	}
	if !strings.Contains(body, "/static/css/bots.css") {
		t.Error("bots.css not linked")
	}
}
