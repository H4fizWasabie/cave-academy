package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"time"
)

const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

// Mailer sends plain-text email through Brevo's transactional API. Without an APIKey
// it prints the email to Out (default stdout) instead, so local runs work.
type Mailer struct {
	APIKey   string
	From     string
	ReplyTo  string    // default Reply-To address: replies go to the admin, not the noreply sender
	Endpoint string    // default brevoEndpoint; tests point it at httptest
	Out      io.Writer // default os.Stdout
	Client   *http.Client
}

// Send mails with the default Reply-To.
func (m *Mailer) Send(to, subject, text string) error {
	return m.SendReplyTo(to, m.ReplyTo, subject, text)
}

// SendReplyTo is Send with this message's own Reply-To ("" for none).
func (m *Mailer) SendReplyTo(to, replyTo, subject, text string) error {
	if m.APIKey == "" {
		out := m.Out
		if out == nil {
			out = os.Stdout
		}
		_, err := fmt.Fprintf(out, "--- email (BREVO_API_KEY not set) ---\nFrom: %s\nTo: %s\nReply-To: %s\nSubject: %s\n\n%s\n---\n", m.From, to, replyTo, subject, text)
		return err
	}

	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("MAIL_FROM %q: %w", m.From, err)
	}
	msg := map[string]any{
		"sender":      map[string]string{"name": from.Name, "email": from.Address},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"textContent": text,
	}
	if replyTo != "" {
		msg["replyTo"] = map[string]string{"email": replyTo}
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = brevoEndpoint
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("brevo: %s: %s", resp.Status, msg)
	}
	return nil
}
