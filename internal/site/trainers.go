package site

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Trainer profiles: a logged-in account owns at most one profile. It starts as
// a "member" (free) and is hidden; an admin promotes it to "cave_trainer" and
// publishes it (admin_trainers.go). Only a published Cave Trainer is public.

const (
	statusMember      = "member"
	statusCaveTrainer = "cave_trainer"
)

// profileTemplates are the layout choices; each is a CSS variation (trainer.css).
var profileTemplates = []struct{ Key, Label, Note string }{
	{"klasik", "Klasik", "Pengepala navy, rasmi dan kemas"},
	{"moden", "Moden", "Latar navy dengan aksen emas"},
	{"ringkas", "Ringkas", "Putih bersih, tumpuan pada teks"},
}

// reservedSlugs cannot be handed out: they collide with fixed /trainer/* routes.
var reservedSlugs = map[string]bool{"profil": true}

type trainerProfile struct {
	ID, UserID                                  int64
	Slug, DisplayName, Headline, Bio, Expertise string
	Phone, Location, LinkedIn, Template, Status string
	Years                                       int
	Published                                   bool
}

const profileCols = `id, user_id, slug, display_name, headline, bio, expertise, years, phone, location, linkedin_url, template, status, published`

type rowScanner interface{ Scan(...any) error }

func scanProfile(r rowScanner) (trainerProfile, error) {
	var p trainerProfile
	err := r.Scan(&p.ID, &p.UserID, &p.Slug, &p.DisplayName, &p.Headline, &p.Bio, &p.Expertise, &p.Years, &p.Phone, &p.Location, &p.LinkedIn, &p.Template, &p.Status, &p.Published)
	return p, err
}

// Live reports whether strangers may see the profile.
func (p trainerProfile) Live() bool { return p.Published && p.Status == statusCaveTrainer }

func (p trainerProfile) IsCave() bool { return p.Status == statusCaveTrainer }

// Initials are the first letters of the first two words of the name.
func (p trainerProfile) Initials() string {
	var b strings.Builder
	for i, w := range strings.Fields(p.DisplayName) {
		if i == 2 {
			break
		}
		r, _ := utf8.DecodeRuneInString(w)
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// Tags are the comma separated expertise entries.
func (p trainerProfile) Tags() []string {
	var out []string
	for _, t := range strings.Split(p.Expertise, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// PhoneHref is the tel: link; template.URL because html/template drops tel: otherwise.
func (p trainerProfile) PhoneHref() template.URL { return template.URL("tel:" + telNumber(p.Phone)) }

// slugify lower-cases the name and keeps a-z and 0-9, joining runs of anything else with "-".
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 50 {
		out = strings.Trim(out[:50], "-")
	}
	if out == "" {
		out = "trainer"
	}
	return out
}

// uniqueSlug returns base, or base-2, base-3 ... the first one that is neither reserved nor taken.
func (s *Server) uniqueSlug(base string) (string, error) {
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", base, i)
		}
		if reservedSlugs[cand] {
			continue
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM trainer_profiles WHERE slug = ?`, cand).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return cand, nil
		}
	}
}

func (s *Server) profileByUser(userID int64) (trainerProfile, bool) {
	p, err := scanProfile(s.db.QueryRow(`SELECT `+profileCols+` FROM trainer_profiles WHERE user_id = ?`, userID))
	return p, err == nil
}

func (s *Server) profileBySlug(slug string) (trainerProfile, bool) {
	p, err := scanProfile(s.db.QueryRow(`SELECT `+profileCols+` FROM trainer_profiles WHERE slug = ?`, slug))
	return p, err == nil
}

// canSee is the visibility rule: live profiles are public; the owner and admins also see the rest.
func canSee(p trainerProfile, u *User) bool {
	return p.Live() || u != nil && (u.IsAdmin || u.ID == p.UserID)
}

// validLinkedIn accepts only http(s) URLs on linkedin.com or a subdomain, without userinfo.
func validLinkedIn(raw string) bool {
	if len(raw) > 200 || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "linkedin.com" || strings.HasSuffix(h, ".linkedin.com")
}

// parseProfileForm validates the submitted fields. On errors the returned
// profile still holds the input so the form can be shown again.
func parseProfileForm(v map[string]string) (trainerProfile, map[string]string) {
	errs := map[string]string{}
	for _, k := range []string{"display_name", "headline", "expertise", "phone", "location"} {
		v[k] = oneLine(v[k])
	}
	v["bio"] = strings.ReplaceAll(v["bio"], "\r\n", "\n")
	p := trainerProfile{DisplayName: v["display_name"], Headline: v["headline"], Bio: v["bio"], Expertise: v["expertise"],
		Phone: v["phone"], Location: v["location"], LinkedIn: v["linkedin_url"], Template: v["template"]}

	require(errs, v, "display_name", 100, "Sila masukkan nama anda (maksimum 100 aksara).")
	if utf8.RuneCountInString(v["headline"]) > 120 {
		errs["headline"] = "Tajuk terlalu panjang (maksimum 120 aksara)."
	}
	if utf8.RuneCountInString(v["bio"]) > 2000 {
		errs["bio"] = "Bio terlalu panjang (maksimum 2000 aksara)."
	}
	require(errs, v, "expertise", 200, "Sila nyatakan bidang kepakaran (maksimum 200 aksara).")
	if _, bad := errs["expertise"]; !bad {
		tags := p.Tags()
		if len(tags) > 12 {
			errs["expertise"] = "Maksimum 12 bidang kepakaran."
		}
		for _, t := range tags {
			if utf8.RuneCountInString(t) > 40 {
				errs["expertise"] = "Setiap bidang kepakaran maksimum 40 aksara."
			}
		}
		p.Expertise = strings.Join(tags, ", ")
	}
	years, err := strconv.Atoi(v["years"])
	if err != nil || years < 0 || years > 60 {
		errs["years"] = "Sila masukkan tahun pengalaman (0 hingga 60)."
	}
	p.Years = years
	if v["phone"] != "" && !validPhone(v["phone"]) {
		errs["phone"] = "Sila masukkan nombor telefon yang sah."
	}
	if utf8.RuneCountInString(v["location"]) > 100 {
		errs["location"] = "Lokasi terlalu panjang (maksimum 100 aksara)."
	}
	if v["linkedin_url"] != "" && !validLinkedIn(v["linkedin_url"]) {
		errs["linkedin_url"] = "Sila masukkan pautan LinkedIn yang sah (https://www.linkedin.com/...)."
	}
	known := false
	for _, t := range profileTemplates {
		known = known || t.Key == v["template"]
	}
	if !known {
		errs["template"] = "Sila pilih satu templat."
		p.Template = profileTemplates[0].Key
	}
	return p, errs
}

// saveProfile updates the user's profile, or creates it with a fresh unique
// slug. The slug never changes afterwards, so shared links keep working.
func (s *Server) saveProfile(userID int64, p trainerProfile) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var res sql.Result
		res, err = s.db.Exec(`UPDATE trainer_profiles SET display_name = ?, headline = ?, bio = ?, expertise = ?, years = ?, phone = ?,
			location = ?, linkedin_url = ?, template = ?, updated_at = datetime('now') WHERE user_id = ?`,
			p.DisplayName, p.Headline, p.Bio, p.Expertise, p.Years, p.Phone, p.Location, p.LinkedIn, p.Template, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
		var slug string
		if slug, err = s.uniqueSlug(slugify(p.DisplayName)); err != nil {
			return err
		}
		// A concurrent insert (double submit) fails on the UNIQUE columns; the next pass then updates.
		_, err = s.db.Exec(`INSERT INTO trainer_profiles (user_id, slug, display_name, headline, bio, expertise, years, phone, location, linkedin_url, template)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, slug, p.DisplayName, p.Headline, p.Bio, p.Expertise, p.Years, p.Phone, p.Location, p.LinkedIn, p.Template)
		if err == nil {
			return nil
		}
	}
	return err
}

// trainerEditView is Page.Data for templates/trainer-edit.html.
type trainerEditView struct {
	P         trainerProfile
	Err       map[string]string
	Exists    bool
	Saved     bool
	Templates []struct{ Key, Label, Note string }
}

// trainerEdit is /trainer/profil: create or edit the logged-in user's profile.
func (s *Server) trainerEdit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page := Page{Title: "Profil Trainer Saya", Nav: "program", Crumb: "Profil Trainer", CSS: []string{"pages", "trainer"}, User: u}
	existing, exists := s.profileByUser(u.ID)
	view := trainerEditView{P: existing, Exists: exists, Templates: profileTemplates}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !exists {
			view.P = trainerProfile{DisplayName: u.Name, Template: profileTemplates[0].Key, Status: statusMember}
		}
		view.Saved = r.URL.Query().Get("disimpan") == "1"
	case http.MethodPost:
		if !sameOrigin(r) {
			http.Error(w, "Permintaan ditolak", http.StatusForbidden)
			return
		}
		v, ok := readForm(w, r, "display_name", "headline", "bio", "expertise", "years", "phone", "location", "linkedin_url", "template")
		if !ok {
			return
		}
		p, errs := parseProfileForm(v)
		if len(errs) > 0 {
			p.Slug, p.Status = existing.Slug, existing.Status
			if !exists {
				p.Status = statusMember
			}
			view.P, view.Err = p, errs
			page.Data = view
			s.renderStatus(w, http.StatusUnprocessableEntity, "trainer-edit", page)
			return
		}
		if err := s.saveProfile(u.ID, p); err != nil {
			log.Printf("save trainer profile: %v", err)
			http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/trainer/profil?disimpan=1", http.StatusSeeOther)
		return
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
		return
	}
	page.Data = view
	s.render(w, "trainer-edit", page)
}

// trainerView is Page.Data for templates/trainer-profile.html.
type trainerView struct {
	P      trainerProfile
	Notice bool // an owner or admin looking at a profile strangers cannot see
}

// trainerShow is GET /trainer/{slug}; hidden profiles are a plain 404 to everyone but owner and admins.
func (s *Server) trainerShow(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	p, ok := s.profileBySlug(r.PathValue("slug"))
	if !ok || !canSee(p, u) {
		s.notFound(w, r)
		return
	}
	if !p.Live() {
		w.Header().Set("Cache-Control", "no-store")
	}
	desc := p.Headline
	if desc == "" {
		desc = "Profil trainer " + p.DisplayName + " di The Cave Academy."
	}
	s.render(w, "trainer-profile", Page{Title: p.DisplayName, Description: desc, Nav: "program", Crumb: "Trainer", CSS: []string{"trainer"}, User: u,
		Data: trainerView{P: p, Notice: !p.Live()}})
}

// trainerList is GET /trainer: every live Cave Trainer.
func (s *Server) trainerList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT ` + profileCols + ` FROM trainer_profiles WHERE published = 1 AND status = 'cave_trainer' ORDER BY display_name COLLATE NOCASE`)
	if err != nil {
		log.Printf("trainer list: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var list []trainerProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			log.Printf("trainer list: %v", err)
			http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
			return
		}
		list = append(list, p)
	}
	s.render(w, "trainers", Page{Title: "Cave Trainer", Description: "Kenali trainer The Cave Academy: kepakaran, pengalaman dan cara menghubungi mereka.",
		Nav: "program", Crumb: "Cave Trainer", CSS: []string{"pages", "trainer"}, Data: list})
}
