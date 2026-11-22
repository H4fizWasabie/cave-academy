package site

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// /admin/pelan: create, edit and deactivate plans, and the paid BCL orders that
// gave nothing (unmatched_payments) so an admin can grant them by hand. Plans
// are never deleted: subscriptions point at them.

const (
	maxPlanName   = 60
	maxPlanPrice  = 10_000_000 // sen: RM100,000
	maxPlanDaily  = 10_000
	maxPlanDays   = 3650
	maxPlanSortAb = 1000
)

type unmatchedRow struct {
	ID       int64
	Order    string
	Email    string
	Paid     string
	Reason   string
	When     string
	PlanID   int64 // the plan BCL's form matched, 0 when none
	PlanName string
}

// planForm is one plan form: Plan's fields and methods, the price as shown
// in its input and a prefix that keeps element ids unique on the page.
type planForm struct {
	Plan
	PriceText, Pre string
}

// adminPelanView is Page.Data for admin_pelan.html.
type adminPelanView struct {
	Plans     []Plan
	Forms     []planForm // one per plan
	New       planForm   // the new-plan form; holds the rejected one when it failed, so nothing typed is lost
	Unmatched []unmatchedRow
	Error     string
}

func (s *Server) renderAdminPelan(w http.ResponseWriter, u *User, status int, errMsg string, draft Plan, draftRM string) {
	v := adminPelanView{Error: errMsg, New: planForm{Plan: draft, PriceText: draftRM, Pre: "baru"}}
	var err error
	if v.Plans, err = s.plans(false); err == nil {
		v.Unmatched, err = s.unmatched(v.Plans)
	}
	if err != nil {
		log.Printf("admin pelan: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if draftRM == "" && draft.Name == "" {
		v.New.Plan = Plan{Active: true, Days: 30}
	}
	for _, p := range v.Plans {
		v.Forms = append(v.Forms, planForm{Plan: p, PriceText: p.PriceInput(), Pre: fmt.Sprintf("p%d", p.ID)})
	}
	s.renderStatus(w, status, "admin_pelan", Page{Title: "Admin: Pelan", CSS: []string{"admin", "pelan"}, User: u, Data: v})
}

func (s *Server) unmatched(plans []Plan) ([]unmatchedRow, error) {
	names := map[int64]string{}
	for _, p := range plans {
		names[p.ID] = p.Name
	}
	rows, err := s.db.Query(`SELECT id, bcl_order, email, plan_id, paid_sen, reason, created_at FROM unmatched_payments ORDER BY id DESC LIMIT ?`, adminLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []unmatchedRow
	for rows.Next() {
		var r unmatchedRow
		var pid sql.NullInt64
		var sen int
		var at string
		if err := rows.Scan(&r.ID, &r.Order, &r.Email, &pid, &sen, &r.Reason, &at); err != nil {
			return nil, err
		}
		r.PlanID, r.PlanName, r.Paid = pid.Int64, names[pid.Int64], formatRM(sen)
		if t, err := time.ParseInLocation(dbTime, at, time.UTC); err == nil {
			at = t.In(klZone).Format("02/01/2006 15:04")
		}
		r.When = at
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Server) adminPelan(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	switch {
	case isGet(r):
		s.renderAdminPelan(w, u, http.StatusOK, "", Plan{}, "")
	case r.Method == http.MethodPost:
		s.adminPelanPost(w, r, u)
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminPelanPost(w http.ResponseWriter, r *http.Request, u *User) {
	if !sameOrigin(r) {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	var err error
	switch r.PostFormValue("action") {
	case "save":
		p, rm, msg := s.parsePlanForm(r)
		if msg != "" {
			if p.ID == 0 {
				s.renderAdminPelan(w, u, http.StatusUnprocessableEntity, msg, p, rm)
			} else {
				s.renderAdminPelan(w, u, http.StatusUnprocessableEntity, msg, Plan{}, "")
			}
			return
		}
		err = s.savePlan(p)
		if err == sql.ErrNoRows {
			s.notFound(w, r)
			return
		}
	case "toggle":
		var id int64
		if id, err = strconv.ParseInt(r.PostFormValue("id"), 10, 64); err != nil {
			http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
			return
		}
		var res sql.Result
		if res, err = s.db.Exec(`UPDATE plans SET active = ? WHERE id = ?`, r.PostFormValue("active") == "1", id); err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				s.notFound(w, r)
				return
			}
		}
	case "grant":
		var msg string
		if msg, err = s.grantUnmatched(r); msg != "" {
			s.renderAdminPelan(w, u, http.StatusUnprocessableEntity, msg, Plan{}, "")
			return
		}
	default:
		http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("admin pelan: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/pelan", http.StatusSeeOther)
}

// parsePlanForm reads and validates the plan form. msg is a Malay error, "" when fine;
// the Plan and the price text come back either way so a bad form can be shown again.
func (s *Server) parsePlanForm(r *http.Request) (p Plan, priceText, msg string) {
	f := r.PostFormValue
	p.ID, _ = strconv.ParseInt(f("id"), 10, 64)
	p.Name, priceText, p.BCLLink, p.Active = strings.TrimSpace(f("name")), strings.TrimSpace(f("price")), strings.TrimSpace(f("bcl_link")), f("active") == "1"
	daily, derr := strconv.Atoi(strings.TrimSpace(f("daily_questions")))
	days, yerr := strconv.Atoi(strings.TrimSpace(f("days")))
	sortN, serr := 0, error(nil)
	if t := strings.TrimSpace(f("sort")); t != "" {
		sortN, serr = strconv.Atoi(t)
	}
	p.DailyQuestions, p.Days, p.Sort = daily, days, sortN
	var priceOK bool
	p.PriceSen, priceOK = parseRM(priceText)
	switch {
	case p.Name == "" || utf8.RuneCountInString(p.Name) > maxPlanName:
		msg = fmt.Sprintf("Nama pelan wajib diisi (maksimum %d aksara).", maxPlanName)
	case !priceOK || p.PriceSen <= 0 || p.PriceSen > maxPlanPrice:
		msg = "Harga mesti dalam RM, contohnya 29 atau 29.90."
	case derr != nil || daily < 0 || daily > maxPlanDaily:
		msg = fmt.Sprintf("Soalan sehari mesti nombor 0 hingga %d (0 = tanpa had).", maxPlanDaily)
	case yerr != nil || days < 1 || days > maxPlanDays:
		msg = fmt.Sprintf("Tempoh mesti 1 hingga %d hari.", maxPlanDays)
	case serr != nil || sortN < -maxPlanSortAb || sortN > maxPlanSortAb:
		msg = "Susunan mesti nombor bulat."
	case !validBCLLink(p.BCLLink):
		msg = "Pautan BCL mesti alamat borang https di bcl.my, contohnya https://syarikat.bcl.my/form/nama-borang."
	}
	if msg == "" {
		all, err := s.plans(false)
		if err != nil {
			return p, priceText, "Ralat dalaman. Cuba lagi."
		}
		for _, o := range all {
			if o.ID != p.ID && bclSlug(o.BCLLink) == bclSlug(p.BCLLink) {
				return p, priceText, "Pelan \"" + o.Name + "\" sudah guna borang BCL yang sama."
			}
		}
	}
	return p, priceText, msg
}

// savePlan inserts a plan (ID 0) or updates one; sql.ErrNoRows when the id is unknown.
func (s *Server) savePlan(p Plan) error {
	if p.ID == 0 {
		_, err := s.db.Exec(`INSERT INTO plans (name, price_sen, daily_questions, days, bcl_link, active, sort) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.PriceSen, p.DailyQuestions, p.Days, p.BCLLink, p.Active, p.Sort)
		return err
	}
	res, err := s.db.Exec(`UPDATE plans SET name = ?, price_sen = ?, daily_questions = ?, days = ?, bcl_link = ?, active = ?, sort = ? WHERE id = ?`,
		p.Name, p.PriceSen, p.DailyQuestions, p.Days, p.BCLLink, p.Active, p.Sort, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// grantUnmatched gives an unmatched order's subscription to the account with the
// typed email (the payer's, unless the admin changes it) on the chosen plan.
// msg is a Malay message when the admin must fix the form; err is a failure.
func (s *Server) grantUnmatched(r *http.Request) (msg string, err error) {
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	pid, _ := strconv.ParseInt(r.PostFormValue("plan_id"), 10, 64)
	var order string
	if err := s.db.QueryRow(`SELECT bcl_order FROM unmatched_payments WHERE id = ?`, id).Scan(&order); err == sql.ErrNoRows {
		return "Bayaran itu sudah tiada dalam senarai.", nil
	} else if err != nil {
		return "", err
	}
	all, err := s.plans(false)
	if err != nil {
		return "", err
	}
	var plan *Plan
	for i := range all {
		if all[i].ID == pid {
			plan = &all[i]
		}
	}
	if plan == nil {
		return "Pilih pelan untuk bayaran itu.", nil
	}
	var uid int64
	switch err := s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, normalizeEmail(r.PostFormValue("email"))).Scan(&uid); {
	case err == sql.ErrNoRows:
		return "Tiada akaun dengan emel itu. Minta pengguna mendaftar dahulu.", nil
	case err != nil:
		return "", err
	}
	_, err = s.grantSubscription(uid, *plan, order)
	return "", err
}
