package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// Mailer sends plain-text email through Resend's HTTP API. Without an APIKey
// it prints the email to Out (default stdout) instead, so local runs work.
type Mailer struct {
	APIKey   string
	From     string
	Endpoint string    // default resendEndpoint; tests point it at httptest
	Out      io.Writer // default os.Stdout
	Client   *http.Client
}

func (m *Mailer) Send(to, subject, text string) error {
	if m.APIKey == "" {
		out := m.Out
		if out == nil {
			out = os.Stdout
		}
		_, err := fmt.Fprintf(out, "--- email (RESEND_API_KEY not set) ---\nFrom: %s\nTo: %s\nSubject: %s\n\n%s\n---\n", m.From, to, subject, text)
		return err
	}

	body, err := json.Marshal(map[string]any{"from": m.From, "to": []string{to}, "subject": subject, "text": text})
	if err != nil {
		return err
	}
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = resendEndpoint
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")
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
		return fmt.Errorf("resend: %s: %s", resp.Status, msg)
	}
	return nil
}
