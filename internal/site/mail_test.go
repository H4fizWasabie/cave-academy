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

func TestMailerPostsToResend(t *testing.T) {
	var gotAuth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	m := &Mailer{APIKey: "re_test", From: "A <a@example.com>", Endpoint: srv.URL}
	if err := m.Send("to@example.com", "Hello", "Body text"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer re_test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	to, _ := got["to"].([]any)
	if got["from"] != "A <a@example.com>" || got["subject"] != "Hello" || got["text"] != "Body text" || len(to) != 1 || to[0] != "to@example.com" {
		t.Errorf("payload = %v", got)
	}
}

func TestMailerReportsResendErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	m := &Mailer{APIKey: "bad", Endpoint: srv.URL}
	err := m.Send("to@example.com", "s", "t")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want it to mention 401", err)
	}
}
