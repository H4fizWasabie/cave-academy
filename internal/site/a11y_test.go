package site

import (
	"net/http"
	"strings"
	"testing"
)

func pageBody(t *testing.T, s *Server, path string) string {
	t.Helper()
	code, body := get(t, s, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, code)
	}
	return body
}

func TestEveryPageHasSkipLinkAndMainTarget(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"/", "/tentang-kami", "/hubungi-kami", "/trainer-panel", "/log-masuk"} {
		body := pageBody(t, s, p)
		if !strings.Contains(body, `href="#utama"`) || !strings.Contains(body, `<main id="utama"`) {
			t.Errorf("%s: missing skip link or #utama target", p)
		}
	}
}

func TestFormFieldsAreLabelledAndAutocompleted(t *testing.T) {
	s := newTestServer(t)
	cases := map[string][]string{
		"/hubungi-kami":  {`for="name"`, `autocomplete="name"`, `for="email"`, `autocomplete="email"`, `for="message"`},
		"/trainer-panel": {`for="phone"`, `autocomplete="tel"`, `inputmode="tel"`, `for="years_experience"`, `inputmode="numeric"`},
		"/daftar":        {`autocomplete="name"`, `autocomplete="email"`, `autocomplete="new-password"`},
		"/log-masuk":     {`autocomplete="email"`, `autocomplete="current-password"`},
	}
	for path, wants := range cases {
		body := pageBody(t, s, path)
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %s", path, w)
			}
		}
	}
}

func TestFormErrorsAreAnnouncedAndLinked(t *testing.T) {
	s := newTestServer(t)
	rec := post(s, "/hubungi-kami", with(contactForm, "email", "bad"))
	body := rec.Body.String()
	for _, w := range []string{`aria-invalid="true" aria-describedby="email-err"`, `role="alert" id="email-err"`} {
		if !strings.Contains(body, w) {
			t.Errorf("contact error state missing %s", w)
		}
	}
}
