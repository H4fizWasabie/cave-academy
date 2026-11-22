package site

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Admin /admin/trainer-profil: moderate trainer profiles (promote, demote,
// publish, unpublish) and email a trainer. Mail goes out with the admin as
// Reply-To (Mailer.ReplyTo), so the trainer's reply lands in the admin's inbox.

type adminTrainerMsg struct{ Subject, Body, At string }

type adminTrainerRow struct {
	trainerProfile
	Email string
	At    string
	Msgs  []adminTrainerMsg
}

type adminTrainersView struct {
	Rows  []adminTrainerRow
	Total int
}

func (s *Server) adminTrainerProfiles(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		s.adminTrainerAction(w, r)
		return
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
		return
	}
	rows, err := s.db.Query(`SELECT p.id, p.user_id, p.slug, p.display_name, p.headline, p.bio, p.expertise, p.years, p.phone, p.location,
		p.linkedin_url, p.template, p.status, p.published, u.email, p.updated_at
		FROM trainer_profiles p JOIN users u ON u.id = p.user_id ORDER BY p.id DESC LIMIT ?`, adminLimit)
	if err != nil {
		log.Printf("admin trainer profiles: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	view := adminTrainersView{Total: s.count(`SELECT COUNT(*) FROM trainer_profiles`)}
	byID := map[int64]int{}
	for rows.Next() {
		var p adminTrainerRow
		if err := rows.Scan(&p.ID, &p.UserID, &p.Slug, &p.DisplayName, &p.Headline, &p.Bio, &p.Expertise, &p.Years, &p.Phone, &p.Location,
			&p.LinkedIn, &p.Template, &p.Status, &p.Published, &p.Email, &p.At); err != nil {
			log.Printf("admin trainer profiles: %v", err)
			http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
			return
		}
		p.At = makeCell("date", p.At).V
		byID[p.ID] = len(view.Rows)
		view.Rows = append(view.Rows, p)
	}
	rows.Close()
	// Latest messages first, at most 5 shown per profile.
	ms, err := s.db.Query(`SELECT profile_id, subject, body, created_at FROM trainer_messages ORDER BY id DESC LIMIT 1000`)
	if err == nil {
		defer ms.Close()
		for ms.Next() {
			var id int64
			var m adminTrainerMsg
			if ms.Scan(&id, &m.Subject, &m.Body, &m.At) != nil {
				continue
			}
			m.At = makeCell("date", m.At).V
			if i, ok := byID[id]; ok && len(view.Rows[i].Msgs) < 5 {
				view.Rows[i].Msgs = append(view.Rows[i].Msgs, m)
			}
		}
	}
	s.render(w, "admin-trainers", Page{Title: "Admin: Profil Trainer", CSS: []string{"admin", "trainer"}, User: u, Data: view})
}

// adminTrainerAction handles every POST on the page, then redirects back (PRG).
func (s *Server) adminTrainerAction(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	var p trainerProfile
	var email string
	if err := s.db.QueryRow(`SELECT p.id, p.user_id, p.slug, p.display_name, p.status, u.email FROM trainer_profiles p JOIN users u ON u.id = p.user_id WHERE p.id = ?`, id).
		Scan(&p.ID, &p.UserID, &p.Slug, &p.DisplayName, &p.Status, &email); err != nil {
		s.notFound(w, r)
		return
	}
	var q string
	switch r.PostFormValue("action") {
	case "promote":
		res, err := s.db.Exec(`UPDATE trainer_profiles SET status = 'cave_trainer', updated_at = datetime('now') WHERE id = ? AND status = 'member'`, id)
		if err != nil {
			s.adminTrainerFail(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 { // only a real promotion sends the email
			if err := s.emailTrainer(p.ID, email, "Tahniah, anda kini Cave Trainer", s.promotionText(p)); err != nil {
				log.Printf("promotion email to profile %d: %v", p.ID, err)
			}
		}
	case "demote":
		q = `UPDATE trainer_profiles SET status = 'member', published = 0, updated_at = datetime('now') WHERE id = ?`
	case "publish":
		q = `UPDATE trainer_profiles SET published = 1, updated_at = datetime('now') WHERE id = ?`
	case "unpublish":
		q = `UPDATE trainer_profiles SET published = 0, updated_at = datetime('now') WHERE id = ?`
	case "message":
		subject, body := oneLine(r.PostFormValue("subject")), strings.TrimSpace(r.PostFormValue("message"))
		if subject == "" || body == "" || utf8.RuneCountInString(subject) > 150 || utf8.RuneCountInString(body) > 3000 {
			http.Error(w, "Subjek (maksimum 150 aksara) dan mesej (maksimum 3000 aksara) diperlukan", http.StatusBadRequest)
			return
		}
		text := "Salam " + p.DisplayName + ",\n\n" + body + "\n\nSila balas e-mel ini untuk terus berhubung dengan kami.\n\nThe Cave Academy"
		if err := s.emailTrainer(p.ID, email, subject, text); err != nil {
			log.Printf("admin email to profile %d: %v", p.ID, err)
			http.Error(w, "E-mel tidak dapat dihantar. Cuba lagi.", http.StatusBadGateway)
			return
		}
	default:
		http.Error(w, "Tindakan tidak sah", http.StatusBadRequest)
		return
	}
	if q != "" {
		if _, err := s.db.Exec(q, id); err != nil {
			s.adminTrainerFail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/trainer-profil", http.StatusSeeOther)
}

func (s *Server) adminTrainerFail(w http.ResponseWriter, err error) {
	log.Printf("admin trainer profile: %v", err)
	http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
}

// emailTrainer sends the mail and, only once it went out, logs it on the profile.
func (s *Server) emailTrainer(profileID int64, to, subject, text string) error {
	if err := s.mailer.Send(to, subject, text); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO trainer_messages (profile_id, subject, body) VALUES (?, ?, ?)`, profileID, subject, text)
	return err
}

func (s *Server) promotionText(p trainerProfile) string {
	return fmt.Sprintf("Salam %s,\n\nTahniah, anda kini Cave Trainer The Cave Academy!\n\n"+
		"Kemas kini profil anda di %s/trainer/profil. Selepas diterbitkan oleh pasukan kami, profil anda boleh dilihat di %s/trainer/%s.\n\n"+
		"Sila balas e-mel ini jika ada pertanyaan.\n\nThe Cave Academy", p.DisplayName, s.baseURL(), s.baseURL(), p.Slug)
}
