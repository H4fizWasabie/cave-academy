package site

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const resetTTL = time.Hour

// resetData is Page.Data for the lupa.html template.
type resetData struct {
	Mode  string // "minta", "dihantar", "baru" or "tidak-sah"
	Email string
	Token string // raw token, carried in a hidden field; never logged
	Error string
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) resetPage(w http.ResponseWriter, status int, title string, d resetData) {
	// same-origin, not no-referrer: the token is in the URL, so keep it out of
	// cross-origin Referers, but no-referrer makes browsers send "Origin: null"
	// on the form POST and sameOrigin rejects it.
	w.Header().Set("Referrer-Policy", "same-origin")
	s.renderStatus(w, status, "lupa", Page{Title: title, Crumb: title, Data: d})
}

// lupaKataLaluan: GET shows the email form, POST emails a reset link when the
// account exists. The response is the same either way.
func (s *Server) lupaKataLaluan(w http.ResponseWriter, r *http.Request) {
	if isGet(r) {
		s.resetPage(w, http.StatusOK, "Lupa Kata Laluan", resetData{Mode: "minta"})
		return
	}
	if !authForm(w, r) {
		return
	}
	email := normalizeEmail(r.PostFormValue("email"))
	ip := clientIP(r)
	if !s.limits.resetEmail.over(email) && !s.limits.resetIP.over(ip) {
		s.limits.resetEmail.add(email)
		s.limits.resetIP.add(ip)
		s.sendReset(email)
	}
	s.resetPage(w, http.StatusOK, "Lupa Kata Laluan", resetData{Mode: "dihantar"})
}

// sendReset creates a token for the account and emails the link. Unknown
// emails and failures are silent: the caller must not learn which.
func (s *Server) sendReset(email string) {
	var id int64
	if s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id) != nil {
		return
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	exp := time.Now().UTC().Add(resetTTL).Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE password_resets SET used_at = datetime('now') WHERE user_id = ? AND used_at IS NULL`, id); err != nil {
		return
	}
	if _, err := tx.Exec(`INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hashToken(token), id, exp); err != nil {
		return
	}
	if tx.Commit() != nil {
		return
	}
	text := "Anda (atau seseorang) meminta untuk menetapkan semula kata laluan akaun The Cave Academy anda.\n\n" +
		"Klik pautan ini untuk memilih kata laluan baharu. Pautan ini tamat tempoh dalam 1 jam:\n\n" +
		s.config.BaseURL + "/reset-kata-laluan?token=" + url.QueryEscape(token) + "\n\n" +
		"Jika anda tidak memintanya, abaikan e-mel ini. Kata laluan anda tidak berubah.\n"
	if err := s.mailer.Send(email, "Tetapkan semula kata laluan anda", text); err != nil {
		log.Printf("reset email: %v", err) // the error never carries the token
	}
}

// validResetToken returns the user the token belongs to when it exists, is
// unused and has not expired.
func (s *Server) validResetToken(token string) (int64, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT user_id FROM password_resets WHERE token_hash = ? AND used_at IS NULL AND expires_at > datetime('now')`,
		hashToken(token)).Scan(&id)
	return id, err == nil
}

// resetKataLaluan: GET checks the token and shows the new-password form,
// POST sets the password, ends every old session and logs the user in.
func (s *Server) resetKataLaluan(w http.ResponseWriter, r *http.Request) {
	if isGet(r) {
		token := r.URL.Query().Get("token")
		if _, ok := s.validResetToken(token); !ok {
			s.resetPage(w, http.StatusGone, "Pautan Tidak Sah", resetData{Mode: "tidak-sah"})
			return
		}
		s.resetPage(w, http.StatusOK, "Kata Laluan Baharu", resetData{Mode: "baru", Token: token})
		return
	}
	if !authForm(w, r) {
		return
	}
	token, password := r.PostFormValue("token"), r.PostFormValue("password")
	id, ok := s.validResetToken(token)
	if !ok {
		s.resetPage(w, http.StatusGone, "Pautan Tidak Sah", resetData{Mode: "tidak-sah"})
		return
	}
	d := resetData{Mode: "baru", Token: token}
	switch {
	case len(password) < minPassword:
		d.Error = "Kata laluan mesti sekurang-kurangnya 8 aksara."
	case len(password) > maxPassword:
		d.Error = "Kata laluan terlalu panjang (maksimum 72 aksara)."
	}
	if d.Error != "" {
		s.resetPage(w, http.StatusUnprocessableEntity, "Kata Laluan Baharu", d)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	// The guarded UPDATE is what makes a token single-use even for two racing requests.
	res, err := tx.Exec(`UPDATE password_resets SET used_at = datetime('now') WHERE token_hash = ? AND used_at IS NULL AND expires_at > datetime('now')`, hashToken(token))
	if n, _ := res.RowsAffected(); err != nil || n != 1 {
		s.resetPage(w, http.StatusGone, "Pautan Tidak Sah", resetData{Mode: "tidak-sah"})
		return
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id); err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if tx.Commit() != nil {
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	s.startSession(w, r, id, "/")
}
