package site

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Paid tiers. Plans are rows (table plans), edited by admins at /admin/pelan; a
// user's daily question limit is the best subscription that is running now,
// else the free freeDaily. users.plan = 'paid' stays a manual unlimited override.
// BCL payments become subscriptions in bcl.go.

const minBCLKey = 32 // shortest BCL_WEBHOOK_KEY

// Plan is one row of the plans table.
type Plan struct {
	ID             int64
	Name           string
	PriceSen       int
	DailyQuestions int // 0 = unlimited
	Days           int
	BCLLink        string
	Active         bool
	Sort           int
}

// Price is "RM29" or "RM29.50".
func (p Plan) Price() string { return formatRM(p.PriceSen) }

// PriceInput is the price as the admin form edits it: "29" or "29.50", no "RM".
func (p Plan) PriceInput() string { return strings.TrimPrefix(formatRM(p.PriceSen), "RM") }

// Quota is the questions-per-day line of a plan card.
func (p Plan) Quota() string {
	if p.DailyQuestions == 0 {
		return "Soalan tanpa had setiap hari"
	}
	return fmt.Sprintf("%d soalan sehari", p.DailyQuestions)
}

// Duration is the length of one purchase.
func (p Plan) Duration() string { return fmt.Sprintf("%d hari", p.Days) }

func formatRM(sen int) string {
	if sen%100 == 0 {
		return fmt.Sprintf("RM%d", sen/100)
	}
	return fmt.Sprintf("RM%d.%02d", sen/100, sen%100)
}

// parseRM reads "29", "29.5" or "29.50" as sen. ok is false for anything else
// (signs, thousands separators, more than two decimals).
func parseRM(s string) (sen int, ok bool) {
	whole, frac, dotted := strings.Cut(strings.TrimSpace(s), ".")
	if whole == "" || len(frac) > 2 || dotted && frac == "" || strings.Trim(whole+frac, "0123456789") != "" {
		return 0, false
	}
	frac += strings.Repeat("0", 2-len(frac))
	n, err := strconv.Atoi(whole + frac)
	return n, err == nil && n <= math.MaxInt32
}

// bclSlug is the last path part of a BCL form address, lowercased, with a
// query string and trailing slash ignored; BCL's form list names forms by it.
func bclSlug(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return ""
	}
	return strings.ToLower(path.Base(p))
}

// validBCLLink accepts only https://bcl.my or https://<merchant>.bcl.my form addresses.
func validBCLLink(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil || bclSlug(link) == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "bcl.my" || strings.HasSuffix(h, ".bcl.my")
}

const planCols = `id, name, price_sen, daily_questions, days, bcl_link, active, sort`

// plans lists every plan, or only the active ones, by sort then id.
func (s *Server) plans(activeOnly bool) ([]Plan, error) {
	q := `SELECT ` + planCols + ` FROM plans`
	if activeOnly {
		q += ` WHERE active = 1`
	}
	rows, err := s.db.Query(q + ` ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceSen, &p.DailyQuestions, &p.Days, &p.BCLLink, &p.Active, &p.Sort); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// entitlement is what a user may do today. Limit is the questions per KL day,
// -1 for unlimited. Plan and Ends describe the subscription behind it ("" when none).
type entitlement struct {
	Limit int
	Plan  string
	Ends  time.Time
}

// entitlementOf is the best subscription running now (unlimited beats any
// number, then the higher number, then the later end); none gives the free
// limit. The users.plan = 'paid' override is unlimited whatever else is there.
func (s *Server) entitlementOf(u *User) entitlement {
	e := entitlement{Limit: freeDaily}
	now := s.now().UTC().Format(dbTime)
	var bestPlan int64
	rows, err := s.db.Query(`SELECT p.id, p.name, p.daily_questions, sub.ends_at FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.user_id = ? AND sub.starts_at <= ? AND sub.ends_at > ?`, u.ID, now, now)
	if err != nil {
		log.Printf("entitlement: %v", err)
	} else {
		defer rows.Close()
		best := 0 // rank of the plan behind e; 0 = none yet
		for rows.Next() {
			var name, ends string
			var daily int
			var pid int64
			if rows.Scan(&pid, &name, &daily, &ends) != nil {
				continue
			}
			t, err := time.ParseInLocation(dbTime, ends, time.UTC)
			if err != nil {
				continue
			}
			rank, limit := daily, daily
			if daily == 0 { // unlimited outranks any number
				rank, limit = math.MaxInt32, -1
			}
			if rank > best || rank == best && t.After(e.Ends) {
				best, bestPlan, e = rank, pid, entitlement{Limit: limit, Plan: name, Ends: t}
			}
		}
		rows.Close()
		// Renewals stack: show the furthest end of that plan, not the running row's.
		var last sql.NullString
		if best > 0 && s.db.QueryRow(`SELECT MAX(ends_at) FROM subscriptions WHERE user_id = ? AND plan_id = ? AND ends_at > ?`, u.ID, bestPlan, now).Scan(&last) == nil {
			if t, err := time.ParseInLocation(dbTime, last.String, time.UTC); err == nil && t.After(e.Ends) {
				e.Ends = t
			}
		}
	}
	if u.Plan == planPaid {
		e.Limit = -1
	}
	return e
}

// dailyLimit is the user's questions per KL day, -1 for unlimited.
func (s *Server) dailyLimit(u *User) int { return s.entitlementOf(u).Limit }

// note is the bot page's "Pelan Pro hingga 12/12/2026" line, "" without a subscription.
func (e entitlement) note() string {
	if e.Plan == "" {
		return ""
	}
	return "Pelan " + e.Plan + " hingga " + e.Ends.In(klZone).Format("02/01/2006")
}

// pelanView is Page.Data for pelan.html.
type pelanView struct {
	Plans    []Plan
	LoggedIn bool
	Email    string
	WA       string
}

// pelan is GET /pelan: the public pricing page.
func (s *Server) pelan(w http.ResponseWriter, r *http.Request) {
	plans, err := s.plans(true)
	if err != nil {
		log.Printf("pelan: %v", err)
		s.serverError(w, r)
		return
	}
	v := pelanView{Plans: plans, WA: waURL}
	u := s.currentUser(r)
	if u != nil {
		v.LoggedIn, v.Email = true, u.Email
	}
	s.render(w, "pelan", Page{Title: "Pelan Langganan", Description: "Pelan langganan AI NLP Bots The Cave Academy: lebih banyak soalan sehari untuk guru, pasukan jualan dan pengurus HR.",
		Nav: "bots", Crumb: "Pelan Langganan", CSS: []string{"pelan"}, User: u, Data: v})
}
