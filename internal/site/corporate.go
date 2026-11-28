package site

import (
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// trainingArea is one "Bidang Latihan Popular" tile; its Name is also a
// training-area option in the RFQ form and is stored as is in rfq_requests.
type trainingArea struct {
	Name  string
	Image string
	Icon  string
}

var trainingAreas = []trainingArea{
	{"Kepimpinan & Pengurusan", "corp-area-leadership.jpg", "i-users"},
	{"Keberkesanan Organisasi", "corp-area-organisation.jpg", "c-gear"},
	{"Pembangunan Pasukan", "corp-area-team.jpg", "i-users"},
	{"AI, NLP & Komunikasi Berimpak", "corp-area-ai-nlp.jpg", "c-chip"},
}

const areaOther = "Lain-lain"

// rfqForm is the submitted form, kept so a failed submit re-renders with the
// visitor's values.
type rfqForm struct {
	Organisation, ContactName, Email, Phone, TrainingArea string
	Participants, PreferredDate, Notes                    string
}

type corporateData struct {
	Areas  []trainingArea
	Other  string
	Form   rfqForm
	Errors map[string]string // field name -> Malay message
	Sent   bool
	Today  string // YYYY-MM-DD in Kuala Lumpur: the earliest preferred date
}

func (s *Server) corporate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.renderCorporate(w, http.StatusOK, corporateData{Sent: r.URL.Query().Get("terima-kasih") == "1"})
	case http.MethodPost:
		s.submitRFQ(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}

func (s *Server) renderCorporate(w http.ResponseWriter, status int, d corporateData) {
	d.Areas, d.Other = trainingAreas, areaOther
	d.Today = s.now().In(klZone).Format("2006-01-02")
	s.renderStatus(w, status, "corporate", Page{
		Title: "Corporate Training & RFQ", Description: "Mohon program latihan korporat untuk organisasi, jabatan dan agensi. Hantar RFQ dan kami akan hubungi anda.", Nav: "program", Crumb: "Corporate Training",
		CSS: []string{"corporate"}, Data: d,
	})
}

func (s *Server) submitRFQ(w http.ResponseWriter, r *http.Request) {
	if !s.formAllowed(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	f := rfqForm{
		Organisation: strings.TrimSpace(r.PostFormValue("organisation")), ContactName: strings.TrimSpace(r.PostFormValue("contact_name")),
		Email: strings.TrimSpace(r.PostFormValue("email")), Phone: strings.TrimSpace(r.PostFormValue("phone")),
		TrainingArea: r.PostFormValue("training_area"), Participants: strings.TrimSpace(r.PostFormValue("participants")),
		PreferredDate: strings.TrimSpace(r.PostFormValue("preferred_date")), Notes: strings.TrimSpace(r.PostFormValue("notes")),
	}
	if s.isBot(r) {
		// Honeypot or timing trap: only bots trip it. Look like a success, save and send nothing.
		http.Redirect(w, r, "/corporate-training?terima-kasih=1#rfq", http.StatusSeeOther)
		return
	}
	errs, participants := f.validate(s.now().In(klZone).Format("2006-01-02"))
	if len(errs) > 0 {
		s.renderCorporate(w, http.StatusUnprocessableEntity, corporateData{Form: f, Errors: errs})
		return
	}
	if _, err := s.db.Exec(`INSERT INTO rfq_requests (organisation, contact_name, email, phone, training_area, participants, preferred_date, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Organisation, f.ContactName, f.Email, f.Phone, f.TrainingArea, participants, f.PreferredDate, f.Notes); err != nil {
		log.Printf("rfq: save: %v", err)
		s.renderCorporate(w, http.StatusInternalServerError, corporateData{Form: f, Errors: map[string]string{
			"form": "Maaf, permintaan anda tidak dapat disimpan. Sila cuba lagi atau hubungi kami melalui WhatsApp.",
		}})
		return
	}
	s.formSaved(r)
	body := fmt.Sprintf("Permintaan sebut harga (RFQ) baharu\n\nOrganisasi: %s\nNama: %s\nE-mel: %s\nTelefon: %s\nBidang latihan: %s\nPeserta: %d\nTarikh pilihan: %s\n\nCatatan:\n%s\n",
		f.Organisation, f.ContactName, f.Email, f.Phone, f.TrainingArea, participants, f.PreferredDate, f.Notes)
	if err := s.mailer.SendReplyTo(s.config.AdminEmail, f.Email, "RFQ baharu: "+oneLine(f.Organisation), body); err != nil {
		log.Printf("rfq: email: %v", err) // the request is saved; do not fail the visitor
	}
	http.Redirect(w, r, "/corporate-training?terima-kasih=1#rfq", http.StatusSeeOther)
}

// oneLine collapses whitespace (including newlines) so user input is safe in a subject.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// validate returns a Malay message per invalid field (keyed by form field
// name; today is YYYY-MM-DD) and the parsed participant count (0 when left empty).
func (f rfqForm) validate(today string) (errs map[string]string, participants int) {
	errs = map[string]string{}
	long := func(field, v string, max int) {
		if utf8.RuneCountInString(v) > max {
			errs[field] = fmt.Sprintf("Terlalu panjang (maksimum %d aksara).", max)
		}
	}
	if f.Organisation == "" {
		errs["organisation"] = "Sila isi nama organisasi."
	} else {
		long("organisation", f.Organisation, 200)
	}
	if f.ContactName == "" {
		errs["contact_name"] = "Sila isi nama untuk dihubungi."
	} else {
		long("contact_name", f.ContactName, 200)
	}
	if a, err := mail.ParseAddress(f.Email); err != nil || a.Address != f.Email || !strings.Contains(f.Email[strings.LastIndex(f.Email, "@"):], ".") {
		errs["email"] = "Sila isi alamat e-mel yang sah."
	} else {
		long("email", f.Email, 200)
	}
	if !validPhone(f.Phone) {
		errs["phone"] = "Sila isi nombor telefon yang sah."
	}
	known := f.TrainingArea == areaOther
	for _, a := range trainingAreas {
		known = known || a.Name == f.TrainingArea
	}
	if !known {
		errs["training_area"] = "Sila pilih bidang latihan."
	}
	if f.Participants != "" {
		n, err := strconv.Atoi(f.Participants)
		if err != nil || n < 1 || n > 100000 {
			errs["participants"] = "Bilangan peserta mesti nombor 1 atau lebih."
		}
		participants = n
	}
	if f.PreferredDate != "" {
		if _, err := time.Parse("2006-01-02", f.PreferredDate); err != nil {
			errs["preferred_date"] = "Tarikh tidak sah."
		} else if f.PreferredDate < today {
			errs["preferred_date"] = "Tarikh pilihan tidak boleh pada masa lalu."
		}
	}
	if utf8.RuneCountInString(f.Notes) > 2000 {
		errs["notes"] = "Catatan terlalu panjang (maksimum 2000 aksara)."
	}
	return errs, participants
}
