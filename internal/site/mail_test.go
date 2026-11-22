package site

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMailerWithoutKeyLogsToOutput(t *testing.T) {
	var out bytes.Buffer
	m := &Mailer{From: "A <a@example.com>", Out: &out}
	if err := m.Send("to@example.com", "Hello", "Body text"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"to@example.com", "Hello", "Body text"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log output missing %q: %s", want, out.String())
		}
	}
}

func TestMailerPostsToBrevo(t *testing.T) {
	var gotKey, gotAccept string
	var got struct {
		Sender      struct{ Name, Email string }
		To          []struct{ Email string }
		Subject     string
		TextContent string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotAccept = r.Header.Get("api-key"), r.Header.Get("Accept")
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"messageId":"<1@brevo>"}`))
	}))
	defer srv.Close()

	m := &Mailer{APIKey: "xkeysib-test", From: "The Cave Academy <noreply@example.com>", Endpoint: srv.URL}
	if err := m.Send("to@example.com", "Hello", "Body text"); err != nil {
		t.Fatal(err)
	}
	if gotKey != "xkeysib-test" || gotAccept != "application/json" {
		t.Errorf("api-key = %q, Accept = %q", gotKey, gotAccept)
	}
	if got.Sender.Name != "The Cave Academy" || got.Sender.Email != "noreply@example.com" ||
		len(got.To) != 1 || got.To[0].Email != "to@example.com" || got.Subject != "Hello" || got.TextContent != "Body text" {
		t.Errorf("payload = %+v", got)
	}
}

func TestMailerRejectsBadFrom(t *testing.T) {
	m := &Mailer{APIKey: "k", From: "not an address", Endpoint: "http://127.0.0.1:1"}
	if err := m.Send("to@example.com", "s", "t"); err == nil || !strings.Contains(err.Error(), "MAIL_FROM") {
		t.Errorf("err = %v, want it to name MAIL_FROM", err)
	}
}

func TestMailerReportsBrevoErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"unauthorized","message":"Key not found"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	m := &Mailer{APIKey: "bad", From: "a@example.com", Endpoint: srv.URL}
	err := m.Send("to@example.com", "s", "t")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want it to mention 401", err)
	}
}
