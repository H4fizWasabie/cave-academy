package site

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// post submits a form to path and returns the recorder.
func post(s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func count(t *testing.T, s *Server, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// captureMail makes the server's mailer write to the returned buffer.
func captureMail(s *Server) *bytes.Buffer {
	var buf bytes.Buffer
	s.mailer.Out = &buf
	return &buf
}

var contactForm = url.Values{"name": {"Aisyah"}, "email": {"aisyah@example.com"}, "message": {"Saya mahu tahu lebih lanjut."}}

var trainerForm = url.Values{
	"name": {"Zul"}, "email": {"zul@example.com"}, "phone": {"012-555 1234"},
	"expertise": {"Kepimpinan"}, "years_experience": {"8"}, "bio": {"Jurulatih sejak 2016."},
}

func with(form url.Values, k, v string) url.Values {
	out := url.Values{}
	for key, vals := range form {
		out[key] = vals
	}
	out.Set(k, v)
	return out
}

func TestContactSubmitSavesEmailsAndRedirects(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	rec := post(s, "/hubungi-kami", contactForm)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/hubungi-kami?terima-kasih=1" {
		t.Fatalf("got %d to %q, want 303 to /hubungi-kami?terima-kasih=1", rec.Code, rec.Header().Get("Location"))
	}
	if n := count(t, s, "contact_messages"); n != 1 {
		t.Fatalf("saved %d messages, want 1", n)
	}
	for _, want := range []string{"To: admin@example.com", "Aisyah", "aisyah@example.com", "Saya mahu tahu lebih lanjut."} {
		if !strings.Contains(mail.String(), want) {
			t.Errorf("email missing %q:\n%s", want, mail)
		}
	}
	_, body := get(t, s, "/hubungi-kami?terima-kasih=1")
	if !strings.Contains(body, "Terima kasih") {
		t.Error("thank-you message not shown")
	}
}

func TestContactInvalidKeepsValuesAndSavesNothing(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	rec := post(s, "/hubungi-kami", with(with(contactForm, "email", "bukan-emel"), "message", ""))
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	for _, want := range []string{"Sila masukkan alamat e-mel yang sah.", "Sila tulis mesej anda.", `value="Aisyah"`, `value="bukan-emel"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
	if count(t, s, "contact_messages") != 0 || mail.Len() != 0 {
		t.Error("invalid submission was saved or emailed")
	}
}

func TestContactHoneypotPretendsSuccess(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	rec := post(s, "/hubungi-kami", with(contactForm, "website", "http://spam.example"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/hubungi-kami?terima-kasih=1" {
		t.Fatalf("got %d to %q, want the normal redirect", rec.Code, rec.Header().Get("Location"))
	}
	if count(t, s, "contact_messages") != 0 || mail.Len() != 0 {
		t.Error("honeypot submission was saved or emailed")
	}
}

func TestTrainerSubmitSavesEmailsAndRedirects(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	rec := post(s, "/trainer-panel", trainerForm)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/trainer-panel?terima-kasih=1" {
		t.Fatalf("got %d to %q, want 303 to /trainer-panel?terima-kasih=1", rec.Code, rec.Header().Get("Location"))
	}
	var years int
	if err := s.db.QueryRow("SELECT years_experience FROM trainer_applications WHERE name = 'Zul'").Scan(&years); err != nil || years != 8 {
		t.Fatalf("saved application: years=%d err=%v", years, err)
	}
	for _, want := range []string{"To: admin@example.com", "Zul", "zul@example.com", "012-555 1234", "Kepimpinan", "8", "Jurulatih sejak 2016."} {
		if !strings.Contains(mail.String(), want) {
			t.Errorf("email missing %q:\n%s", want, mail)
		}
	}
	_, body := get(t, s, "/trainer-panel?terima-kasih=1")
	if !strings.Contains(body, "Terima kasih") {
		t.Error("thank-you message not shown")
	}
}

func TestTrainerInvalidKeepsValuesAndSavesNothing(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	form := with(with(with(trainerForm, "phone", "abc"), "years_experience", "banyak"), "expertise", "")
	rec := post(s, "/trainer-panel", form)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	for _, want := range []string{"Sila masukkan nombor telefon yang sah.", "Sila masukkan tahun pengalaman (0 hingga 60).", "Sila nyatakan bidang kepakaran anda.", `value="Zul"`, `value="banyak"`, "Jurulatih sejak 2016."} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
	if count(t, s, "trainer_applications") != 0 || mail.Len() != 0 {
		t.Error("invalid submission was saved or emailed")
	}
}

func TestTrainerHoneypotPretendsSuccess(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)

	rec := post(s, "/trainer-panel", with(trainerForm, "website", "http://spam.example"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/trainer-panel?terima-kasih=1" {
		t.Fatalf("got %d to %q, want the normal redirect", rec.Code, rec.Header().Get("Location"))
	}
	if count(t, s, "trainer_applications") != 0 || mail.Len() != 0 {
		t.Error("honeypot submission was saved or emailed")
	}
}

func TestAboutPageLinksOnward(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/tentang-kami")
	for _, want := range []string{"Siapa Kami", "Kenapa Kami", "EMPOWER MINDS. SHAPE LEADERS.", `href="/corporate-training"`, `href="/ai-nlp-bots"`, `href="/produk-digital"`, `href="/hubungi-kami"`} {
		if !strings.Contains(strings.ToUpper(body), strings.ToUpper(want)) {
			t.Errorf("about page missing %q", want)
		}
	}
}
