package site

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
	defaultNext   = "/ai-nlp-bots"
	minPassword   = 8
	maxPassword   = 72 // bcrypt ignores (or rejects) anything longer
)

// User is the logged-in account. Plan is "free" or "paid".
type User struct {
	ID    int64
	Name  string
	Email string
	Plan  string
}

// FirstName is the first word of the name, for the header.
func (u *User) FirstName() string {
	f, _, _ := strings.Cut(u.Name, " ")
	return f
}

// currentUser returns the user behind the request's session cookie, or nil
// when logged out, the token is unknown, or the session has expired.
func (s *Server) currentUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u := &User{}
	err = s.db.QueryRow(`SELECT u.id, u.name, u.email, u.plan FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > datetime('now')`, c.Value).Scan(&u.ID, &u.Name, &u.Email, &u.Plan)
	if err != nil {
		return nil
	}
	return u
}

// requireUser returns the logged-in user. When logged out it redirects to
// /log-masuk?next=<this page> and returns false: the handler must just return.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*User, bool) {
	if u := s.currentUser(r); u != nil {
		return u, true
	}
	http.Redirect(w, r, "/log-masuk?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	return nil, false
}

// reqWriter remembers the request so render can fill Page.User without every
// handler passing it in. ServeHTTP wraps each response writer in one.
type reqWriter struct {
	http.ResponseWriter
	r *http.Request
}

func (w *reqWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// authData is Page.Data for the auth.html template.
type authData struct {
	Mode  string // "daftar", "masuk" or "keluar"
	Name  string
	Email string
	Next  string // only a safe, explicit next; "" when none was given
	Error string
}

// safeNext returns next when it is a local path ("/x", not "//x" or "/\x"),
// else "". This is what stops open redirects.
func safeNext(next string) string {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(next, "/") ||
		strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return ""
	}
	return next
}

// sameOrigin is the CSRF check on top of SameSite=Lax: a POST that carries an
// Origin header must come from this host.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// authForm handles the shared plumbing of the three auth endpoints: method
// check, CSRF, form parsing. It returns false when it has already responded.
func authForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
		return false
	}
	if !sameOrigin(r) || r.ParseForm() != nil {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return false
	}
	return true
}

func isGet(r *http.Request) bool { return r.Method == http.MethodGet || r.Method == http.MethodHead }

func (s *Server) authPage(w http.ResponseWriter, status int, title string, d authData) {
	s.renderStatus(w, status, "auth", Page{Title: title, Crumb: title, Data: d})
}

func (s *Server) daftar(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.FormValue("next"))
	if isGet(r) {
		if s.currentUser(r) != nil {
			http.Redirect(w, r, orDefault(next), http.StatusSeeOther)
			return
		}
		s.authPage(w, http.StatusOK, "Daftar", authData{Mode: "daftar", Next: next})
		return
	}
	if !authForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	email := normalizeEmail(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	d := authData{Mode: "daftar", Name: name, Email: email, Next: next}
	switch {
	case name == "":
		d.Error = "Sila masukkan nama anda."
	case !validEmail(email):
		d.Error = "Sila masukkan emel yang sah."
	case len(password) < minPassword:
		d.Error = "Kata laluan mesti sekurang-kurangnya 8 aksara."
	case len(password) > maxPassword:
		d.Error = "Kata laluan terlalu panjang (maksimum 72 aksara)."
	}
	if d.Error != "" {
		s.authPage(w, http.StatusUnprocessableEntity, "Daftar", d)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	res, err := s.db.Exec(`INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)`, name, email, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			d.Error = "Emel ini sudah berdaftar. Sila log masuk."
			s.authPage(w, http.StatusUnprocessableEntity, "Daftar", d)
			return
		}
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	s.startSession(w, r, id, next)
}

func (s *Server) logMasuk(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.FormValue("next"))
	if isGet(r) {
		if s.currentUser(r) != nil {
			http.Redirect(w, r, orDefault(next), http.StatusSeeOther)
			return
		}
		s.authPage(w, http.StatusOK, "Log Masuk", authData{Mode: "masuk", Next: next})
		return
	}
	if !authForm(w, r) {
		return
	}
	email := normalizeEmail(r.PostFormValue("email"))
	var id int64
	var hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, email).Scan(&id, &hash)
	if err == nil {
		err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.PostFormValue("password")))
	} else if err != sql.ErrNoRows {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if err != nil {
		s.authPage(w, http.StatusUnauthorized, "Log Masuk",
			authData{Mode: "masuk", Email: email, Next: next, Error: "Emel atau kata laluan tidak betul."})
		return
	}
	s.startSession(w, r, id, next)
}

// logKeluar: GET shows a confirm button, POST ends the session. Logging out
// needs a POST so another site cannot do it with an image tag.
func (s *Server) logKeluar(w http.ResponseWriter, r *http.Request) {
	if isGet(r) {
		s.authPage(w, http.StatusOK, "Log Keluar", authData{Mode: "keluar"})
		return
	}
	if !authForm(w, r) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.db.Exec(`DELETE FROM sessions WHERE token = ?`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// startSession stores a new session for the user, sets the cookie and
// redirects to next (or the default landing page).
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64, next string) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.db.Exec(`DELETE FROM sessions WHERE expires_at <= datetime('now')`) // housekeeping
	exp := time.Now().UTC().Add(sessionTTL).Format("2006-01-02 15:04:05")
	if _, err := s.db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, userID, exp); err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	http.Redirect(w, r, orDefault(next), http.StatusSeeOther)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func orDefault(next string) string {
	if next == "" {
		return defaultNext
	}
	return next
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e
}
