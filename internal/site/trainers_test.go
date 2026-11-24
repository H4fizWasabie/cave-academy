package site

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// tpForm is a valid profile form; pairs of key, value override fields.
func tpForm(over ...string) url.Values {
	v := url.Values{
		"display_name": {"Siti Aminah"}, "headline": {"Jurulatih kepimpinan"}, "bio": {"Bio ringkas saya.\nBaris kedua."},
		"expertise": {"kepimpinan, komunikasi"}, "years": {"7"}, "phone": {"012-345 6789"}, "location": {"Kuala Lumpur"},
		"linkedin_url": {"https://www.linkedin.com/in/siti"}, "template": {"klasik"},
	}
	for i := 0; i+1 < len(over); i += 2 {
		v.Set(over[i], over[i+1])
	}
	return v
}

func tpSave(t *testing.T, s *Server, c *http.Cookie, form url.Values) int {
	t.Helper()
	return authPost(t, s, "/trainer/profil", form, []*http.Cookie{c}).Code
}

func tpSlug(t *testing.T, s *Server, email string) string {
	t.Helper()
	var slug string
	if err := s.db.QueryRow(`SELECT p.slug FROM trainer_profiles p JOIN users u ON u.id = p.user_id WHERE u.email = ?`, email).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	return slug
}

// tpSet forces a profile's moderation state.
func tpSet(t *testing.T, s *Server, slug, status string, published bool) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE trainer_profiles SET status = ?, published = ? WHERE slug = ?`, status, published, slug); err != nil {
		t.Fatal(err)
	}
}

func TestTrainerProfileNeedsLogin(t *testing.T) {
	s := newTestServer(t)
	if rec := request(s, "/trainer/profil"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/log-masuk?next=") {
		t.Errorf("GET logged out = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := authPost(t, s, "/trainer/profil", tpForm(), nil); rec.Code != http.StatusSeeOther || count(t, s, "trainer_profiles") != 0 {
		t.Errorf("POST logged out = %d, rows %d", rec.Code, count(t, s, "trainer_profiles"))
	}
}

func TestTrainerProfileCreateThenEdit(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	if code := tpSave(t, s, c, tpForm()); code != http.StatusSeeOther {
		t.Fatalf("create = %d", code)
	}
	var status string
	var published bool
	s.db.QueryRow(`SELECT status, published FROM trainer_profiles`).Scan(&status, &published)
	if status != "member" || published || tpSlug(t, s, "siti@example.com") != "siti-aminah" {
		t.Errorf("new profile status=%q published=%v slug=%q", status, published, tpSlug(t, s, "siti@example.com"))
	}
	if code := tpSave(t, s, c, tpForm("headline", "Pakar NLP", "template", "moden", "display_name", "Siti A. Rahman")); code != http.StatusSeeOther {
		t.Fatalf("edit = %d", code)
	}
	var headline, tmpl string
	s.db.QueryRow(`SELECT headline, template FROM trainer_profiles`).Scan(&headline, &tmpl)
	if count(t, s, "trainer_profiles") != 1 || headline != "Pakar NLP" || tmpl != "moden" || tpSlug(t, s, "siti@example.com") != "siti-aminah" {
		t.Errorf("edit: rows=%d headline=%q template=%q slug=%q (slug must stay stable)", count(t, s, "trainer_profiles"), headline, tmpl, tpSlug(t, s, "siti@example.com"))
	}
	body := request(s, "/trainer/profil", c).Body.String()
	for _, want := range []string{`value="Siti A. Rahman"`, `id="tp-preview"`, "tp--moden", "/static/js/trainer-edit.js", `/trainer/siti-aminah/pdf`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestTrainerProfileValidation(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	long := func(n int) string { return strings.Repeat("a", n) }
	bad := map[string]url.Values{
		"no name":          tpForm("display_name", ""),
		"long name":        tpForm("display_name", long(101)),
		"long headline":    tpForm("headline", long(121)),
		"long bio":         tpForm("bio", long(2001)),
		"no expertise":     tpForm("expertise", ""),
		"long expertise":   tpForm("expertise", long(201)),
		"too many tags":    tpForm("expertise", "a,b,c,d,e,f,g,h,i,j,k,l,m"),
		"bad years":        tpForm("years", "abc"),
		"years too high":   tpForm("years", "61"),
		"bad phone":        tpForm("phone", "call me"),
		"long location":    tpForm("location", long(101)),
		"js url":           tpForm("linkedin_url", "javascript:alert(1)"),
		"other host":       tpForm("linkedin_url", "https://evil.example/in/x"),
		"lookalike host":   tpForm("linkedin_url", "https://linkedin.com.evil.example/in/x"),
		"userinfo trick":   tpForm("linkedin_url", "https://linkedin.com@evil.example/"),
		"unknown template": tpForm("template", "../../etc"),
	}
	for name, form := range bad {
		if code := tpSave(t, s, c, form); code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, code)
		}
	}
	if n := count(t, s, "trainer_profiles"); n != 0 {
		t.Errorf("invalid forms saved %d rows", n)
	}
	rec := authPost(t, s, "/trainer/profil", tpForm("years", "x"), []*http.Cookie{c})
	if !strings.Contains(rec.Body.String(), `role="alert"`) || !strings.Contains(rec.Body.String(), `value="Siti Aminah"`) {
		t.Error("422 page should keep the input and show an alert")
	}
	for _, ok := range []string{"https://linkedin.com/in/x", "http://my.linkedin.com/in/x", ""} {
		if code := tpSave(t, s, c, tpForm("linkedin_url", ok)); code != http.StatusSeeOther {
			t.Errorf("linkedin %q rejected: %d", ok, code)
		}
	}
}

func TestTrainerProfileCrossOriginRejected(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	rec := authPost(t, s, "/trainer/profil", tpForm(), []*http.Cookie{c}, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || count(t, s, "trainer_profiles") != 0 {
		t.Errorf("cross-origin = %d, rows %d", rec.Code, count(t, s, "trainer_profiles"))
	}
}

func TestTrainerSlugsAreUnique(t *testing.T) {
	s := newTestServer(t)
	a, b, c := signUp(t, s, "a@example.com"), signUp(t, s, "b@example.com"), signUp(t, s, "c@example.com")
	tpSave(t, s, a, tpForm())
	tpSave(t, s, b, tpForm())
	tpSave(t, s, c, tpForm("display_name", "Profil"))
	got := []string{tpSlug(t, s, "a@example.com"), tpSlug(t, s, "b@example.com"), tpSlug(t, s, "c@example.com")}
	want := []string{"siti-aminah", "siti-aminah-2", "profil-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slug %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTrainerVisibility(t *testing.T) {
	s := newTestServer(t)
	owner := signUp(t, s, "owner@example.com")
	other := signUp(t, s, "other@example.com")
	admin := adminCookie(t, s, "admin@example.com")
	tpSave(t, s, owner, tpForm())
	slug := "siti-aminah"
	cases := []struct {
		name      string
		status    string
		published bool
		anon      int
		stranger  int
		owner     int
		admin     int
	}{
		{"member unpublished", "member", false, 404, 404, 200, 200},
		{"member published", "member", true, 404, 404, 200, 200},
		{"trainer unpublished", "cave_trainer", false, 404, 404, 200, 200},
		{"trainer published", "cave_trainer", true, 200, 200, 200, 200},
	}
	for _, tc := range cases {
		tpSet(t, s, slug, tc.status, tc.published)
		for _, path := range []string{"/trainer/" + slug, "/trainer/" + slug + "/pdf"} {
			for who, want := range map[string]int{"anon": tc.anon, "stranger": tc.stranger, "owner": tc.owner, "admin": tc.admin} {
				var cs []*http.Cookie
				switch who {
				case "stranger":
					cs = []*http.Cookie{other}
				case "owner":
					cs = []*http.Cookie{owner}
				case "admin":
					cs = []*http.Cookie{admin}
				}
				if rec := request(s, path, cs...); rec.Code != want {
					t.Errorf("%s: %s as %s = %d, want %d", tc.name, path, who, rec.Code, want)
				}
			}
		}
	}
	if code, _ := get(t, s, "/trainer/tiada-slug"); code != http.StatusNotFound {
		t.Errorf("unknown slug = %d", code)
	}
	// Owner preview of an unpublished profile is flagged and kept out of search engines.
	tpSet(t, s, slug, "member", false)
	body := request(s, "/trainer/"+slug, owner).Body.String()
	if !strings.Contains(body, "noindex") || !strings.Contains(body, "belum diterbitkan") {
		t.Error("owner preview should say it is unpublished and noindex")
	}
}

func TestTrainerDirectoryListsOnlyLiveTrainers(t *testing.T) {
	s := newTestServer(t)
	if strings.Contains(pageBody(t, s, "/trainer"), `<li><a href="/trainer">`) {
		t.Error("nav should hide Cave Trainer when none is live")
	}
	a, b := signUp(t, s, "a@example.com"), signUp(t, s, "b@example.com")
	tpSave(t, s, a, tpForm("display_name", "Aminah Live"))
	tpSave(t, s, b, tpForm("display_name", "Budi Tersembunyi"))
	tpSet(t, s, "aminah-live", "cave_trainer", true)
	tpSet(t, s, "budi-tersembunyi", "cave_trainer", false)
	if !strings.Contains(pageBody(t, s, "/trainer"), `<li><a href="/trainer">`) {
		t.Error("nav should link Cave Trainer while a live trainer exists")
	}
	body := pageBody(t, s, "/trainer")
	if !strings.Contains(body, "Aminah Live") || strings.Contains(body, "Budi Tersembunyi") {
		t.Errorf("directory shows wrong trainers")
	}
	if _, sm := get(t, s, "/sitemap.xml"); !strings.Contains(sm, "/trainer</loc>") {
		t.Error("sitemap missing /trainer")
	}
	if _, sm := get(t, s, "/sitemap.xml"); !strings.Contains(sm, "/trainer/aminah-live</loc>") || strings.Contains(sm, "budi-tersembunyi") {
		t.Error("sitemap should list the published Cave trainer only")
	}
}

func TestTrainerProfileEscapesUserText(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "x@example.com")
	tpSave(t, s, c, tpForm("display_name", "<script>alert(1)</script>", "bio", "<img src=x onerror=alert(1)>"))
	slug := tpSlug(t, s, "x@example.com")
	tpSet(t, s, slug, "cave_trainer", true)
	body := pageBody(t, s, "/trainer/"+slug)
	if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<img src=x") {
		t.Error("profile text was not escaped")
	}
}

func TestTrainerPDF(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	tpSave(t, s, c, tpForm("display_name", "Siti Aminah binti Ñandú", "bio", strings.Repeat("Latihan kepimpinan yang berkesan. ", 55)))
	slug := tpSlug(t, s, "siti@example.com")
	tpSet(t, s, slug, "cave_trainer", true)
	rec := request(s, "/trainer/"+slug+"/pdf")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(rec.Body.String(), "%PDF") || rec.Body.Len() < 1000 {
		t.Errorf("not a PDF: %d bytes", rec.Body.Len())
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="`+slug+`.pdf"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if n := strings.Count(rec.Body.String(), "/Type /Page\n"); n != 1 {
		t.Errorf("pdf has %d pages, want 1", n)
	}
}

func TestAdminTrainerProfilesNeedAdmin(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "biasa@example.com")
	tpSave(t, s, c, tpForm())
	if rec := request(s, "/admin/trainer-profil"); rec.Code != http.StatusSeeOther {
		t.Errorf("logged out = %d", rec.Code)
	}
	if rec := request(s, "/admin/trainer-profil", c); rec.Code != http.StatusNotFound {
		t.Errorf("non-admin GET = %d", rec.Code)
	}
	rec := authPost(t, s, "/admin/trainer-profil", url.Values{"id": {"1"}, "action": {"promote"}}, []*http.Cookie{c})
	var status string
	s.db.QueryRow(`SELECT status FROM trainer_profiles`).Scan(&status)
	if rec.Code != http.StatusNotFound || status != "member" {
		t.Errorf("non-admin promote = %d, status %q", rec.Code, status)
	}
}

func TestAdminTrainerPromotePublishAndMail(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)
	c := signUp(t, s, "siti@example.com")
	admin := adminCookie(t, s, "admin@example.com")
	tpSave(t, s, c, tpForm())
	act := func(action string, extra ...string) *httptest.ResponseRecorder {
		f := url.Values{"id": {"1"}, "action": {action}}
		for i := 0; i+1 < len(extra); i += 2 {
			f.Set(extra[i], extra[i+1])
		}
		return authPost(t, s, "/admin/trainer-profil", f, []*http.Cookie{admin})
	}
	state := func() (string, bool) {
		var st string
		var pub bool
		s.db.QueryRow(`SELECT status, published FROM trainer_profiles`).Scan(&st, &pub)
		return st, pub
	}

	if rec := authPost(t, s, "/admin/trainer-profil", url.Values{"id": {"1"}, "action": {"promote"}}, []*http.Cookie{admin}, "Origin", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin promote = %d", rec.Code)
	}
	if st, _ := state(); st != "member" {
		t.Fatal("cross-origin POST changed status")
	}

	if rec := act("promote"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/trainer-profil" {
		t.Errorf("promote = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if st, _ := state(); st != "cave_trainer" {
		t.Errorf("status after promote = %q", st)
	}
	out := mail.String()
	if !strings.Contains(out, "To: siti@example.com") || !strings.Contains(out, "Tahniah") || !strings.Contains(out, "Cave Trainer") {
		t.Errorf("promotion email missing:\n%s", out)
	}
	act("promote") // already a Cave Trainer: no second email
	if n := strings.Count(mail.String(), "Subject: Tahniah"); n != 1 {
		t.Errorf("promotion email sent %d times", n)
	}

	act("publish")
	if _, pub := state(); !pub {
		t.Error("publish did not publish")
	}
	if code, _ := get(t, s, "/trainer/siti-aminah"); code != http.StatusOK {
		t.Errorf("published trainer page = %d", code)
	}
	act("unpublish")
	if _, pub := state(); pub {
		t.Error("unpublish did not unpublish")
	}
	act("publish")
	act("demote")
	if st, pub := state(); st != "member" || pub {
		t.Errorf("after demote status=%q published=%v", st, pub)
	}
	if rec := act("explode"); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown action = %d", rec.Code)
	}
}

func TestAdminTrainerMessage(t *testing.T) {
	s := newTestServer(t)
	mail := captureMail(s)
	c := signUp(t, s, "siti@example.com")
	admin := adminCookie(t, s, "admin@example.com")
	tpSave(t, s, c, tpForm())
	send := func(subject, body string) int {
		return authPost(t, s, "/admin/trainer-profil", url.Values{"id": {"1"}, "action": {"message"}, "subject": {subject}, "message": {body}}, []*http.Cookie{admin}).Code
	}
	if code := send("", "isi"); code != http.StatusBadRequest {
		t.Errorf("empty subject = %d", code)
	}
	if code := send("Sesi baharu", "Boleh anda mengajar pada 5 Disember?"); code != http.StatusSeeOther {
		t.Fatalf("send = %d", code)
	}
	out := mail.String()
	if !strings.Contains(out, "To: siti@example.com") || !strings.Contains(out, "Subject: Sesi baharu") || !strings.Contains(out, "5 Disember") {
		t.Errorf("message email:\n%s", out)
	}
	if count(t, s, "trainer_messages") != 1 {
		t.Errorf("logged %d messages", count(t, s, "trainer_messages"))
	}
	page := request(s, "/admin/trainer-profil", admin).Body.String()
	for _, want := range []string{"Siti Aminah", "siti@example.com", "Sesi baharu", "5 Disember"} {
		if !strings.Contains(page, want) {
			t.Errorf("admin page missing %q", want)
		}
	}
}

func TestAdminOverviewCountsTrainerProfiles(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	tpSave(t, s, admin, tpForm())
	body := request(s, "/admin", admin).Body.String()
	if !strings.Contains(body, `href="/admin/trainer-profil"`) || !strings.Contains(body, "Profil Trainer") {
		t.Error("overview missing the trainer profile card")
	}
}

func TestTrainerPDFStaysOnOnePageWithHugeBio(t *testing.T) {
	s := newTestServer(t)
	c := signUp(t, s, "siti@example.com")
	tpSave(t, s, c, tpForm("bio", strings.Repeat("a\n", 900)+"z"))
	rec := request(s, "/trainer/siti-aminah/pdf", c)
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "/Type /Page\n") != 1 {
		t.Errorf("pdf = %d, pages %d", rec.Code, strings.Count(rec.Body.String(), "/Type /Page\n"))
	}
}
