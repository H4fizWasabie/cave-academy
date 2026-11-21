package site

import (
	"net/http"
	"strings"
	"testing"
)

func TestWhatsAppLinkEncodesMessage(t *testing.T) {
	cases := map[string]string{
		"Salam, saya berminat dengan produk Leadership 360° & DISC?": "https://wa.me/60123947200?text=Salam%2C%20saya%20berminat%20dengan%20produk%20Leadership%20360%C2%B0%20%26%20DISC%3F",
		"a+b=c d": "https://wa.me/60123947200?text=a%2Bb%3Dc%20d",
	}
	for msg, want := range cases {
		if got := waLink(msg); got != want {
			t.Errorf("waLink(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestProdukPageListsProductsWithWhatsAppLinks(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/produk-digital")
	if code != http.StatusOK {
		t.Fatalf("GET /produk-digital = %d, want 200", code)
	}
	for _, want := range []string{
		"Leadership 360°", "NLP for Educators", "AI for Educators", "DISC at Work", "Strategic Management Game",
		"Kategori Produk Digital", "Produk Pilihan", "Kenapa Pilih Produk Digital TCA?", "Terokai Koleksi Produk Digital",
		// A product button: Malay message naming the product, encoded.
		"https://wa.me/60123947200?text=Salam%2C%20saya%20berminat%20dengan%20produk%20digital%20DISC%20at%20Work.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("produk page missing %q", want)
		}
	}
	if strings.Contains(body, "wa.me/60123947200?text=Salam, ") {
		t.Error("a WhatsApp link carries an unencoded message")
	}
}
