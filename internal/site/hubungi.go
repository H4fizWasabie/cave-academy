package site

import (
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// formView is the template data for a page with a form: the submitted values
// (kept on error), per-field Malay error messages, and the thank-you flag.
type formView struct {
	V      map[string]string
	Err    map[string]string
	Thanks bool
}

// readForm parses a POSTed form (body capped at 64 KB) and returns the named
// fields trimmed. "website" is the honeypot field and is always included.
func readForm(w http.ResponseWriter, r *http.Request, names ...string) (map[string]string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Borang terlalu besar atau tidak sah.", http.StatusBadRequest)
		return nil, false
	}
	v := map[string]string{"website": strings.TrimSpace(r.PostFormValue("website"))}
	for _, n := range names {
		v[n] = strings.TrimSpace(r.PostFormValue(n))
	}
	return v, true
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && len(s) <= 254
}

// validPhone accepts digits with optional + ( ) - and spaces, 9 to 15 digits.
func validPhone(s string) bool {
	digits := 0
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '+' || c == '-' || c == ' ' || c == '(' || c == ')':
		default:
			return false
		}
	}
	return digits >= 9 && digits <= 15
}

// require adds msg to errs[field] when the value is empty or longer than max runes.
func require(errs map[string]string, v map[string]string, field string, max int, msg string) {
	if x := v[field]; x == "" || utf8.RuneCountInString(x) > max {
		errs[field] = msg
	}
}

// notifyAdmin emails the submission to ADMIN_EMAIL. The row is already saved,
// so a mail failure is only logged.
func (s *Server) notifyAdmin(subject string, v map[string]string, fields [][2]string) {
	var b strings.Builder
	for _, f := range fields {
		fmt.Fprintf(&b, "%s: %s\n", f[1], v[f[0]])
	}
	if err := s.mailer.Send(s.config.AdminEmail, subject, b.String()); err != nil {
		log.Printf("mail %q: %v", subject, err)
	}
}

func (s *Server) hubungi(w http.ResponseWriter, r *http.Request) {
	page := Page{Title: "Hubungi Kami", Crumb: "Hubungi Kami", CSS: []string{"pages"}}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		page.Data = formView{Thanks: r.URL.Query().Get("terima-kasih") == "1"}
		s.render(w, "hubungi", page)
	case http.MethodPost:
		fields := [][2]string{{"name", "Nama"}, {"email", "E-mel"}, {"message", "Mesej"}}
		v, ok := readForm(w, r, "name", "email", "message")
		if !ok {
			return
		}
		if v["website"] == "" {
			errs := map[string]string{}
			require(errs, v, "name", 100, "Sila masukkan nama anda.")
			if !validEmail(v["email"]) {
				errs["email"] = "Sila masukkan alamat e-mel yang sah."
			}
			require(errs, v, "message", 2000, "Sila tulis mesej anda.")
			if len(errs) > 0 {
				page.Data = formView{V: v, Err: errs}
				s.renderStatus(w, http.StatusUnprocessableEntity, "hubungi", page)
				return
			}
			if _, err := s.db.Exec("INSERT INTO contact_messages (name, email, message) VALUES (?, ?, ?)", v["name"], v["email"], v["message"]); err != nil {
				log.Printf("save contact message: %v", err)
				http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
				return
			}
			s.notifyAdmin("Mesej baharu dari borang Hubungi Kami", v, fields)
		}
		http.Redirect(w, r, "/hubungi-kami?terima-kasih=1", http.StatusSeeOther)
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}
