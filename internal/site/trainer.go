package site

import (
	"log"
	"net/http"
	"strconv"
)

func (s *Server) trainerPanel(w http.ResponseWriter, r *http.Request) {
	page := Page{Title: "Trainer Panel", Description: "Sertai rangkaian trainer profesional The Cave Academy. Daftar sebagai Trainer Panel dan kongsikan kepakaran anda.", Nav: "program", Crumb: "Trainer Panel", CSS: []string{"pages"}}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		page.Data = formView{Thanks: r.URL.Query().Get("terima-kasih") == "1"}
		s.render(w, "trainer", page)
	case http.MethodPost:
		if !s.formAllowed(w, r) {
			return
		}
		fields := [][2]string{{"name", "Nama"}, {"email", "E-mel"}, {"phone", "Telefon"}, {"expertise", "Bidang kepakaran"}, {"years_experience", "Tahun pengalaman"}, {"bio", "Bio ringkas"}}
		v, ok := readForm(w, r, "name", "email", "phone", "expertise", "years_experience", "bio")
		if !ok {
			return
		}
		if v["website"] == "" {
			errs := map[string]string{}
			require(errs, v, "name", 100, "Sila masukkan nama anda.")
			if !validEmail(v["email"]) {
				errs["email"] = "Sila masukkan alamat e-mel yang sah."
			}
			if !validPhone(v["phone"]) {
				errs["phone"] = "Sila masukkan nombor telefon yang sah."
			}
			require(errs, v, "expertise", 200, "Sila nyatakan bidang kepakaran anda.")
			years, err := strconv.Atoi(v["years_experience"])
			if err != nil || years < 0 || years > 60 {
				errs["years_experience"] = "Sila masukkan tahun pengalaman (0 hingga 60)."
			}
			if len([]rune(v["bio"])) > 2000 {
				errs["bio"] = "Bio terlalu panjang (maksimum 2000 aksara)."
			}
			if len(errs) > 0 {
				page.Data = formView{V: v, Err: errs}
				s.renderStatus(w, http.StatusUnprocessableEntity, "trainer", page)
				return
			}
			if _, err := s.db.Exec("INSERT INTO trainer_applications (name, email, phone, expertise, years_experience, bio) VALUES (?, ?, ?, ?, ?, ?)",
				v["name"], v["email"], v["phone"], v["expertise"], years, v["bio"]); err != nil {
				log.Printf("save trainer application: %v", err)
				http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
				return
			}
			s.formSaved(r)
			s.notifyAdmin("Permohonan baharu Trainer Panel", v, fields)
		}
		http.Redirect(w, r, "/trainer-panel?terima-kasih=1", http.StatusSeeOther)
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}
