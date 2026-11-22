package site

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Admin back office: /admin and /admin/{rfq,trainer,mesej,pengguna}. Admins
// are users.is_admin = 1, set only by the make-admin CLI (SetAdmin), never by
// signup: emails are not verified, so nothing here derives admin from one.

const adminLimit = 200 // rows per list on screen; CSV has every row

// SetAdmin sets or clears the admin flag of an existing account. It is what
// `make-admin` / `revoke-admin` call.
func SetAdmin(db *sql.DB, email string, on bool) error {
	res, err := db.Exec(`UPDATE users SET is_admin = ? WHERE email = ?`, on, normalizeEmail(email))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no account with email %q (sign up first)", email)
	}
	return nil
}

// requireAdmin returns the logged-in admin. Logged out it redirects to the
// login page; a logged-in non-admin gets a plain 404 so the area's existence
// is not revealed. On false the handler must just return.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !u.IsAdmin {
		s.notFound(w, r)
		return nil, false
	}
	return u, true
}

// adminList describes one list page. Query returns Heads' columns in order
// followed by the row id; it may take one "?" = the start of today (UTC text).
type adminList struct {
	Title string
	Heads []string
	Kinds []string // per column: "" text, "long", "email", "phone", "date", "plan", "bool"
	Query string
}

var adminLists = map[string]adminList{
	"rfq": {
		Title: "Permintaan RFQ",
		Heads: []string{"Organisasi", "Nama", "E-mel", "Telefon", "Bidang latihan", "Peserta", "Tarikh pilihan", "Nota", "Diterima"},
		Kinds: []string{"", "", "email", "phone", "", "", "", "long", "date"},
		Query: `SELECT organisation, contact_name, email, phone, training_area, participants, preferred_date, notes, created_at, id FROM rfq_requests ORDER BY id DESC`,
	},
	"trainer": {
		Title: "Permohonan Trainer",
		Heads: []string{"Nama", "E-mel", "Telefon", "Kepakaran", "Tahun", "Bio", "Diterima"},
		Kinds: []string{"", "email", "phone", "", "", "long", "date"},
		Query: `SELECT name, email, phone, expertise, years_experience, bio, created_at, id FROM trainer_applications ORDER BY id DESC`,
	},
	"mesej": {
		Title: "Mesej",
		Heads: []string{"Nama", "E-mel", "Mesej", "Diterima"},
		Kinds: []string{"", "email", "long", "date"},
		Query: `SELECT name, email, message, created_at, id FROM contact_messages ORDER BY id DESC`,
	},
	"pengguna": {
		Title: "Pengguna",
		Heads: []string{"Nama", "E-mel", "Pelan", "Daftar", "Soalan hari ini", "Admin"},
		Kinds: []string{"", "email", "plan", "date", "", "bool"},
		Query: `SELECT name, email, plan, created_at,
			(SELECT COUNT(*) FROM chat_messages c WHERE c.user_id = users.id AND c.role = 'user' AND c.created_at >= ?),
			is_admin, id FROM users ORDER BY id DESC`,
	},
}

// adminCell is one table cell: V is shown, CSV is exported. Href and WA are
// the mailto:/tel: and WhatsApp links for email and phone cells.
type adminCell struct {
	V, CSV, Kind, WA string
	Href             template.URL // template.URL: html/template drops tel: links otherwise
}

type adminRow struct {
	ID    int64
	Cells []adminCell
}

type adminCard struct {
	Label, Href, Note string
	Total, Week       int
}

// adminView is Page.Data for templates/admin.html.
type adminView struct {
	Key   string // "" on the overview, else the adminLists key
	Title string
	Cards []adminCard
	Heads []string
	Rows  []adminRow
	Total int // all rows, Rows holds at most adminLimit of them
	Limit int
}

func (s *Server) adminRender(w http.ResponseWriter, u *User, v adminView) {
	s.render(w, "admin", Page{Title: "Admin: " + v.Title, CSS: []string{"admin"}, User: u, Data: v})
}

func (s *Server) count(q string) int {
	n := 0
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		log.Printf("admin count: %v", err)
	}
	return n
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	week := ` WHERE created_at >= datetime('now', '-7 days')`
	card := func(label, key, table, note string) adminCard {
		return adminCard{Label: label, Href: "/admin/" + key, Note: note,
			Total: s.count(`SELECT COUNT(*) FROM ` + table), Week: s.count(`SELECT COUNT(*) FROM ` + table + week)}
	}
	paid := fmt.Sprintf("%d berbayar", s.count(`SELECT COUNT(*) FROM users WHERE plan = 'paid'`))
	s.adminRender(w, u, adminView{Title: "Ringkasan", Cards: []adminCard{
		card("Pengguna", "pengguna", "users", paid),
		card("Permintaan RFQ", "rfq", "rfq_requests", ""),
		card("Permohonan Trainer", "trainer", "trainer_applications", ""),
		card("Mesej", "mesej", "contact_messages", ""),
	}})
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	key := r.PathValue("page")
	l, ok := adminLists[key]
	if !ok {
		s.notFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		if key != "pengguna" {
			http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
			return
		}
		s.adminSetPlan(w, r)
		return
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
		return
	}
	csvOut := r.URL.Query().Get("format") == "csv"
	rows, err := s.adminRows(l, csvOut)
	if err != nil {
		log.Printf("admin %s: %v", key, err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if csvOut {
		writeCSV(w, key, l, rows)
		return
	}
	table := map[string]string{"rfq": "rfq_requests", "trainer": "trainer_applications", "mesej": "contact_messages", "pengguna": "users"}[key]
	s.adminRender(w, u, adminView{Key: key, Title: l.Title, Heads: l.Heads, Rows: rows,
		Total: s.count(`SELECT COUNT(*) FROM ` + table), Limit: adminLimit})
}

// adminRows runs the list query (newest first, all rows when all is true,
// else adminLimit) and turns every column into a cell.
func (s *Server) adminRows(l adminList, all bool) ([]adminRow, error) {
	q := l.Query
	if !all {
		q += fmt.Sprintf(" LIMIT %d", adminLimit)
	}
	var args []any
	if strings.Contains(q, "?") {
		n := s.now().In(klZone)
		args = append(args, time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, klZone).UTC().Format(dbTime))
	}
	rs, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	n := len(l.Heads) + 1 // + id
	var out []adminRow
	for rs.Next() {
		vals := make([]any, n)
		ptrs := make([]any, n)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := adminRow{}
		row.ID, _ = strconv.ParseInt(cellText(vals[n-1]), 10, 64)
		for i, kind := range l.Kinds {
			row.Cells = append(row.Cells, makeCell(kind, cellText(vals[i])))
		}
		out = append(out, row)
	}
	return out, rs.Err()
}

func cellText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

func makeCell(kind, v string) adminCell {
	c := adminCell{V: v, CSV: v, Kind: kind}
	switch kind {
	case "email":
		if v != "" {
			c.Href = template.URL("mailto:" + v)
		}
	case "phone":
		if d := waDigits(v); d != "" {
			c.Href = template.URL("tel:" + telNumber(v))
			c.WA = "https://wa.me/" + d
		}
	case "date":
		if t, err := time.ParseInLocation(dbTime, v, time.UTC); err == nil {
			t = t.In(klZone)
			c.V, c.CSV = t.Format("02/01/2006 15:04"), t.Format("2006-01-02 15:04")
		}
	case "bool":
		c.V, c.CSV = "", ""
		if v == "1" {
			c.V, c.CSV = "Ya", "Ya"
		}
	}
	return c
}

// telNumber keeps the digits and a leading + of a phone number for tel: links.
func telNumber(p string) string {
	var b strings.Builder
	for i, c := range strings.TrimSpace(p) {
		if c >= '0' && c <= '9' || i == 0 && c == '+' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// waDigits is the number for https://wa.me/<digits>: digits only, a leading 0
// (Malaysian local format) replaced by 60.
func waDigits(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "0") {
		d = "60" + d[1:]
	}
	return d
}

// csvSafe stops spreadsheet formula injection: a cell that would start a
// formula gets a leading apostrophe.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// writeCSV sends the list as UTF-8 with a BOM so Excel shows Malay text right.
func writeCSV(w http.ResponseWriter, key string, l adminList, rows []adminRow) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+key+`.csv"`)
	w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.Write(l.Heads)
	for _, r := range rows {
		rec := make([]string, len(r.Cells))
		for i, c := range r.Cells {
			rec[i] = csvSafe(c.CSV)
		}
		cw.Write(rec)
	}
	cw.Flush()
}

// adminSetPlan handles the plan form on /admin/pengguna: free or paid only.
// Admin flags are CLI-only, so nothing else is read from the form.
func (s *Server) adminSetPlan(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	plan := r.PostFormValue("plan")
	if err != nil || (plan != "free" && plan != planPaid) {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	res, err := s.db.Exec(`UPDATE users SET plan = ? WHERE id = ?`, plan, id)
	if err != nil {
		log.Printf("admin set plan: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/pengguna", http.StatusSeeOther)
}
