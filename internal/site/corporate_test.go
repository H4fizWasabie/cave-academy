package site

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postRFQ submits values to /corporate-training and returns the response.
func postRFQ(t *testing.T, s *Server, v url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/corporate-training", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func validRFQ() url.Values {
	return url.Values{
		"organisation":   {"Syarikat Maju Sdn Bhd"},
		"contact_name":   {"Aisyah Rahman"},
		"email":          {"aisyah@maju.example"},
		"phone":          {"012-345 6789"},
		"training_area":  {"Pembangunan Pasukan"},
		"participants":   {"25"},
		"preferred_date": {"2026-12-15"},
		"notes":          {"Latihan dua hari di Kuala Lumpur."},
	}
}

func rfqCount(t *testing.T, s *Server) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM rfq_requests`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCorporatePageHasKeyContent(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/corporate-training")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{
		"Corporate Training &amp; ", "RFQ", "Program latihan berimpak tinggi untuk organisasi, jabatan dan agensi.",
		"Trainer", "Kandungan", "Boleh Disesuaikan", "Berimpak Tinggi",
		"Bidang Latihan Popular", "Kepimpinan &amp; Pengurusan", "Keberkesanan Organisasi", "Pembangunan Pasukan", "AI, NLP &amp; Komunikasi Berimpak",
		"Proses Pelaksanaan Latihan", "1.&nbsp;Perbincangan", "2.&nbsp;Cadangan", "3.&nbsp;Pelaksanaan", "4.&nbsp;Penilaian",
		"Mohon Sebut Harga (RFQ)", `href="#rfq"`,
		`id="rfq"`, `method="post"`, `action="/corporate-training#rfq"`,
		`name="organisation"`, `name="contact_name"`, `name="email"`, `name="phone"`,
		`name="training_area"`, `name="participants"`, `name="preferred_date"`, `name="notes"`,
		`value="Lain-lain"`, `name="website"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("corporate page missing %q", want)
		}
	}
	if strings.Contains(body, "Halaman ini akan datang") {
		t.Error("still the stub page")
	}
}

func TestRFQSubmitSavesAndEmailsAdmin(t *testing.T) {
	s := newTestServer(t)
	var out bytes.Buffer
	s.mailer.Out = &out

	rec := postRFQ(t, s, validRFQ())

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/corporate-training?terima-kasih=1#rfq" {
		t.Errorf("redirect to %q", loc)
	}
	var org, email, area, date string
	var participants int
	err := s.db.QueryRow(`SELECT organisation, email, training_area, participants, preferred_date FROM rfq_requests`).
		Scan(&org, &email, &area, &participants, &date)
	if err != nil {
		t.Fatalf("no saved row: %v", err)
	}
	if org != "Syarikat Maju Sdn Bhd" || email != "aisyah@maju.example" || area != "Pembangunan Pasukan" || participants != 25 || date != "2026-12-15" {
		t.Errorf("saved row = %q %q %q %d %q", org, email, area, participants, date)
	}
	mail := out.String()
	for _, want := range []string{"To: admin@example.com", "Syarikat Maju Sdn Bhd", "Aisyah Rahman", "aisyah@maju.example", "012-345 6789", "Pembangunan Pasukan", "25", "2026-12-15", "Latihan dua hari"} {
		if !strings.Contains(mail, want) {
			t.Errorf("admin email missing %q:\n%s", want, mail)
		}
	}
}

func TestRFQInvalidSubmitRerendersFormWithMalayErrors(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		value   string
		message string
	}{
		{"missing organisation", "organisation", " ", "Sila isi nama organisasi."},
		{"missing contact", "contact_name", "", "Sila isi nama untuk dihubungi."},
		{"bad email", "email", "bukan-emel", "Sila isi alamat e-mel yang sah."},
		{"short phone", "phone", "123", "Sila isi nombor telefon yang sah."},
		{"letters in phone", "phone", "abcdefghij", "Sila isi nombor telefon yang sah."},
		{"unknown area", "training_area", "Lain-lain-lagi", "Sila pilih bidang latihan."},
		{"no area", "training_area", "", "Sila pilih bidang latihan."},
		{"zero participants", "participants", "0", "Bilangan peserta mesti nombor 1 atau lebih."},
		{"text participants", "participants", "ramai", "Bilangan peserta mesti nombor 1 atau lebih."},
		{"bad date", "preferred_date", "15/12/2026", "Tarikh tidak sah."},
		{"long notes", "notes", strings.Repeat("a", 2001), "Catatan terlalu panjang"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestServer(t)
			var out bytes.Buffer
			s.mailer.Out = &out
			v := validRFQ()
			v.Set(c.field, c.value)

			rec := postRFQ(t, s, v)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status %d, want 422", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), c.message) {
				t.Errorf("body missing %q", c.message)
			}
			if rfqCount(t, s) != 0 || out.Len() != 0 {
				t.Error("an invalid request was saved or emailed")
			}
		})
	}
}

func TestRFQInvalidSubmitKeepsValues(t *testing.T) {
	s := newTestServer(t)
	v := validRFQ()
	v.Set("email", "salah")
	v.Set("notes", `Nota <b>penting</b> & "khas"`)

	body := postRFQ(t, s, v).Body.String()

	for _, want := range []string{
		`value="Syarikat Maju Sdn Bhd"`, `value="Aisyah Rahman"`, `value="salah"`, `value="012-345 6789"`,
		`value="Pembangunan Pasukan" selected`, `value="25"`, `value="2026-12-15"`,
		`Nota &lt;b&gt;penting&lt;/b&gt; &amp; &#34;khas&#34;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("re-rendered form missing %q", want)
		}
	}
}

func TestRFQOptionalFieldsMayBeEmpty(t *testing.T) {
	s := newTestServer(t)
	s.mailer.Out = &bytes.Buffer{}
	v := validRFQ()
	v.Set("participants", "")
	v.Set("preferred_date", "")
	v.Set("notes", "")
	v.Set("training_area", "Lain-lain")

	if rec := postRFQ(t, s, v); rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if rfqCount(t, s) != 1 {
		t.Error("request not saved")
	}
}

func TestRFQHoneypotLooksLikeSuccessButSavesNothing(t *testing.T) {
	s := newTestServer(t)
	var out bytes.Buffer
	s.mailer.Out = &out
	v := validRFQ()
	v.Set("website", "http://spam.example")

	rec := postRFQ(t, s, v)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/corporate-training?terima-kasih=1#rfq" {
		t.Errorf("got %d to %q, want the same redirect as a success", rec.Code, rec.Header().Get("Location"))
	}
	if rfqCount(t, s) != 0 || out.Len() != 0 {
		t.Error("a honeypot submission was saved or emailed")
	}
}

func TestThankYouShownAfterRedirect(t *testing.T) {
	s := newTestServer(t)
	_, body := get(t, s, "/corporate-training?terima-kasih=1")
	if !strings.Contains(body, "Terima kasih!") || strings.Contains(body, `name="organisation"`) {
		t.Error("thank-you page should replace the form")
	}
	_, body = get(t, s, "/corporate-training")
	if strings.Contains(body, "Terima kasih!") {
		t.Error("thank-you shown without the redirect flag")
	}
}

func TestRFQSubjectIsOneLine(t *testing.T) {
	s := newTestServer(t)
	var out bytes.Buffer
	s.mailer.Out = &out
	v := validRFQ()
	v.Set("organisation", "Maju\r\nBcc: x@evil.example")

	postRFQ(t, s, v)

	if !strings.Contains(out.String(), "Subject: RFQ baharu: Maju Bcc: x@evil.example\n") {
		t.Errorf("subject not collapsed to one line:\n%s", out.String())
	}
}

func TestCorporateRejectsOtherMethods(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/corporate-training", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d, want 405", rec.Code)
	}
}
