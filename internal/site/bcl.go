package site

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BCL (bcl.my) checkout. A plan's bcl_link is a BCL payment form; when someone
// pays on it BCL calls POST /bcl/webhook/<BCL_WEBHOOK_KEY>. BCL signs nothing,
// so the key in the address only keeps strangers out: every order is read back
// from BCL's own API with BCL_API_TOKEN, and only what BCL says (successful,
// paid, live, the form, the amounts, the payer) decides what is granted. The
// webhook body gives nothing but the order number.

const bclDefaultAPI = "https://bcl.my/api"

var bclClient = &http.Client{Timeout: 10 * time.Second}

// bclText is a value BCL may send as a string, a number or a bool, kept as trimmed text; null is empty.
type bclText string

func (t *bclText) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) != nil {
		s = string(b)
		if s == "null" {
			s = ""
		}
	}
	*t = bclText(strings.TrimSpace(s))
	return nil
}

func (t bclText) String() string { return string(t) }

// bclOrder is the shape of BCL's webhook body and of /transactions/<order>'s answer.
type bclOrder struct {
	Event string `json:"event"`
	Data  struct {
		FormableID bclText `json:"formable_id"` // the BCL form id the order was paid on
		Main       struct {
			Order      bclText `json:"order_number"`
			PayerEmail bclText `json:"payer_email"`
			Kind       bclText `json:"transaction_type"` // "test" for Test Mode
			Subtotal   bclText `json:"subtotal_amount"`
			Discount   bclText `json:"discount_amount"`
			Amount     bclText `json:"amount"` // charged, fee included
			Paid       bclText `json:"is_paid"`
			Status     bclText `json:"status"` // 3 = successful
		} `json:"main_data"`
	} `json:"data"`
}

// paidSen is what the order paid before BCL's fee: the subtotal less a coupon's
// discount; the charged amount when BCL sends no subtotal. ok is false when no
// amount reads as RM.
func (o *bclOrder) paidSen() (sen int, ok bool) {
	m := o.Data.Main
	if m.Subtotal == "" {
		return parseRM(m.Amount.String())
	}
	sen, ok = parseRM(m.Subtotal.String())
	if d := m.Discount.String(); ok && d != "" {
		var disc int
		disc, ok = parseRM(d)
		sen -= disc
	}
	return sen, ok && sen >= 0
}

var errBCLNotReady = errors.New("BCL does not know the order, or it is not successful, paid and live yet")

// bclGet reads BCL's API at pathAndQuery into v. The token goes in a header and
// never in an error or log line; BCL blocks Go's default User-Agent.
func (s *Server) bclGet(ctx context.Context, pathAndQuery string, v any) error {
	base := s.config.BCLAPIURL
	if base == "" {
		base = bclDefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+pathAndQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.config.BCLAPIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cave-academy/1.0")
	res, err := bclClient.Do(req)
	if err != nil {
		return errors.New("BCL unreachable")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errBCLNotReady
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("BCL API token refused (%d): replace BCL_API_TOKEN", res.StatusCode)
	default:
		return fmt.Errorf("BCL answered %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(v); err != nil {
		return fmt.Errorf("unreadable BCL answer: %w", err)
	}
	return nil
}

// bclFormSlug is the slug BCL lists for form id, "" when it lists none.
// ponytail: reads the first 100 forms only; page through /forms if the merchant ever has more.
func (s *Server) bclFormSlug(ctx context.Context, id string) (string, error) {
	var forms struct {
		Data []struct {
			ID   bclText `json:"form_id"`
			Slug bclText `json:"slug"`
		} `json:"data"`
	}
	if err := s.bclGet(ctx, "/forms?per_page=100", &forms); err != nil {
		return "", err
	}
	for _, f := range forms.Data {
		if f.ID.String() == id {
			return strings.ToLower(f.Slug.String()), nil
		}
	}
	return "", nil
}

// planBySlug is the plan whose bcl_link ends in slug. Inactive plans count: a
// payer who paid just before a plan was switched off still paid.
func (s *Server) planBySlug(slug string) (*Plan, error) {
	if slug == "" {
		return nil, nil
	}
	all, err := s.plans(false)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if bclSlug(all[i].BCLLink) == slug {
			return &all[i], nil
		}
	}
	return nil, nil
}

// bclWebhook is POST /bcl/webhook/{key}. Answers: 404 for a wrong or unset
// key; 400 for a body that is not BCL's; 200 for events and Test Mode orders
// to ignore, for a paid order granted (or already granted) and for a paid
// order kept for an admin (no account, unknown form, too little paid); 500
// when BCL is unreachable, does not know the order or does not have it
// successful, paid and live yet, so BCL retries.
func (s *Server) bclWebhook(w http.ResponseWriter, r *http.Request) {
	key := s.config.BCLWebhookKey
	if len(key) < minBCLKey || s.config.BCLAPIToken == "" || subtle.ConstantTimeCompare([]byte(r.PathValue("key")), []byte(key)) != 1 {
		s.notFound(w, r)
		return
	}
	var hook bclOrder
	if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	order := hook.Data.Main.Order.String()
	switch {
	case hook.Event != "" && hook.Event != "payment-success":
		log.Printf("bcl: event %q ignored", hook.Event)
	case strings.EqualFold(hook.Data.Main.Kind.String(), "test"):
		log.Printf("bcl: order %q is a Test Mode payment; nothing given", order)
	case order == "" || len(order) > 64:
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	default:
		outcome, err := s.takeBCLOrder(r.Context(), order)
		if err != nil {
			log.Printf("bcl: order %q: %v", order, err) // BCL retries
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		log.Printf("bcl: order %q: %s", order, outcome)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok")
}

// takeBCLOrder confirms the order with BCL and grants it or keeps it for an
// admin. It says what happened, for the log. An error means try again later.
func (s *Server) takeBCLOrder(ctx context.Context, order string) (string, error) {
	// An order already granted (by a webhook or by hand) is done: never hold it again.
	var done int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE bcl_order = ?`, order).Scan(&done); err != nil {
		return "", err
	}
	if done > 0 {
		return "already granted", nil
	}
	var d bclOrder
	if err := s.bclGet(ctx, "/transactions/"+url.PathEscape(order), &d); err != nil {
		return "", err
	}
	m := d.Data.Main
	if m.Order.String() != order {
		return "", errors.New("BCL's order detail names another order number")
	}
	if m.Status != "3" || (m.Paid != "1" && m.Paid != "true") || !strings.EqualFold(m.Kind.String(), "live") {
		return "", fmt.Errorf("%w (status %s, is_paid %s, type %s)", errBCLNotReady, m.Status, m.Paid, m.Kind)
	}
	formID := d.Data.FormableID.String()
	if formID == "" {
		return "", errors.New("BCL's order detail names no form")
	}
	slug, err := s.bclFormSlug(ctx, formID)
	if err != nil {
		return "", err
	}
	plan, err := s.planBySlug(slug)
	if err != nil {
		return "", err
	}
	email := normalizeEmail(m.PayerEmail.String())
	paid, readable := d.paidSen()

	hold := func(reason string) (string, error) {
		var pid sql.NullInt64
		if plan != nil {
			pid = sql.NullInt64{Int64: plan.ID, Valid: true}
		}
		_, err := s.db.Exec(`INSERT OR IGNORE INTO unmatched_payments (bcl_order, email, plan_id, paid_sen, reason) VALUES (?, ?, ?, ?, ?)`,
			order, email, pid, paid, reason)
		return "kept for an admin: " + reason, err
	}
	switch {
	case plan == nil:
		return hold("borang BCL ini bukan pelan mana-mana")
	case !readable || paid < plan.PriceSen:
		return hold("bayaran kurang daripada harga pelan")
	}
	var uid int64
	switch err := s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&uid); {
	case err == sql.ErrNoRows:
		return hold("tiada akaun dengan emel pembayar")
	case err != nil:
		return "", err
	}
	granted, err := s.grantSubscription(uid, *plan, order)
	if err != nil {
		return "", err
	}
	if !granted {
		return "already granted", nil
	}
	return fmt.Sprintf("granted plan %d to user %d", plan.ID, uid), nil
}

// grantSubscription gives the user the plan for its days, once per BCL order
// (a repeat changes nothing and returns false). A new purchase of a plan the
// user still has runs on from that subscription's end; any other starts now.
// It also clears the order from unmatched_payments.
func (s *Server) grantSubscription(userID int64, p Plan, order string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	start := now
	var end sql.NullString
	if err := tx.QueryRow(`SELECT MAX(ends_at) FROM subscriptions WHERE user_id = ? AND plan_id = ? AND ends_at > ?`,
		userID, p.ID, now.Format(dbTime)).Scan(&end); err != nil {
		return false, err
	}
	if t, err := time.ParseInLocation(dbTime, end.String, time.UTC); end.Valid && err == nil && t.After(start) {
		start = t
	}
	res, err := tx.Exec(`INSERT INTO subscriptions (user_id, plan_id, starts_at, ends_at, bcl_order) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(bcl_order) DO NOTHING`, userID, p.ID, start.Format(dbTime), start.AddDate(0, 0, p.Days).Format(dbTime), order)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM unmatched_payments WHERE bcl_order = ?`, order); err != nil {
		return false, err
	}
	return n == 1, tx.Commit()
}
