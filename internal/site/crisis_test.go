package site

import (
	"net/http"
	"strings"
	"testing"
)

func TestIsCrisis(t *testing.T) {
	for _, m := range []string{
		"Saya nak mati", "nk mati la", "Aku ingin   MATI.", "dah tak nak hidup", "x nak hidup lagi", "tak nk hidup",
		"saya fikir nak bunuh diri", "nak akhiri hidup ini", "saya mahu cederakan diri", "kelar tangan",
		"dia bunuh dirinya", "fikir nak membunuh diri", "nak bunuh dri", "tk nak hidup", "tak nak hdup", "tak mau hidup",
		"dah tak larat nak teruskan hidup", "tak larat nak hidup", "nak tidur selamanya", "nak tidur selama-lamanya",
		"I want to die", "thinking of suicide", "I feel suicidal", "i will kill myself", "self-harm", "self harm urges", "i might hurt myself",
	} {
		if !isCrisis(m) {
			t.Errorf("isCrisis(%q) = false", m)
		}
	}
	for _, m := range []string{
		"Saya stres dengan kerja", "rasa tertekan", "penat sangat", "burnout", "rasa nak give up kerja",
		"saya mati kutu", "bos saya nak bunuh saya dengan kerja ni", "tolong nak matikan lampu", "nak mati-matian siapkan projek",
		"susah tidur sebab risau", "mati", "bunuh", "tak larat nak teruskan kerja ni", "nak tidur awal malam ni",
	} {
		if isCrisis(m) {
			t.Errorf("isCrisis(%q) = true", m)
		}
	}
}

func assertHelpLines(t *testing.T, s string) {
	t.Helper()
	for _, want := range []string{"999", "03-7627 2929", "15555", "15999"} {
		if !strings.Contains(s, want) {
			t.Errorf("reply lacks %q: %.200q", want, s)
		}
	}
}

func TestCrisisMessageGetsHelpLinesAndSkipsModel(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	code, out := g.sendJSON("stres", "Saya nak mati", c)
	reply, _ := out["reply"].(string)
	if code != http.StatusOK || out["remaining"] != float64(2) {
		t.Fatalf("crisis send: %d %v", code, out)
	}
	assertHelpLines(t, reply)
	if g.or.count() != 0 {
		t.Error("model was called for a crisis message")
	}
	if g.rows(t, `1=1`) != 2 {
		t.Error("crisis exchange not stored")
	}
}

func TestCrisisReplyIgnoresLimitCapAndMissingKey(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	for i := 0; i < 3; i++ {
		g.sendJSON("hr", "Soalan", c)
	}
	calls := g.or.count()
	code, out := g.sendJSON("hr", "I want to die", c) // 0 questions left
	reply, _ := out["reply"].(string)
	if code != http.StatusOK {
		t.Fatalf("with 0 remaining: %d %v", code, out)
	}
	assertHelpLines(t, reply)

	g2 := newChatRig(t)
	c2 := signUp(t, g2.s, "ali@example.com")
	g2.s.config.OpenRouterKey = ""
	code, out = g2.sendJSON("educators", "nak bunuh diri", c2)
	reply, _ = out["reply"].(string)
	if code != http.StatusOK {
		t.Fatalf("with no key: %d %v", code, out)
	}
	assertHelpLines(t, reply)
	if g.or.count() != calls || g2.or.count() != 0 {
		t.Error("model called for a crisis message")
	}
}

func TestCrisisNoJSRedirectShowsHelpLines(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	rec := g.send("stres", "saya tak nak hidup", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ai-nlp-bots/stres#terkini" {
		t.Fatalf("send = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	assertHelpLines(t, request(g.s, "/ai-nlp-bots/stres", c).Body.String())
	if g.or.count() != 0 {
		t.Error("model was called")
	}
}

func TestStressMessageCallsModel(t *testing.T) {
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	if code, _ := g.sendJSON("stres", "Saya stres dan penat sangat dengan kerja", c); code != http.StatusOK || g.or.count() != 1 {
		t.Errorf("stress message: %d, %d model calls", code, g.or.count())
	}
}

func TestStressBotPageAndPrompt(t *testing.T) {
	s := newTestServer(t)
	code, body := get(t, s, "/ai-nlp-bots/stres")
	b, _ := findBot("stres")
	if code != http.StatusOK || b.Notice == "" || !strings.Contains(body, "Befrienders KL 03-7627 2929") || !strings.Contains(body, "bukan ahli terapi") {
		t.Errorf("logged-out stres page: %d, missing notice", code)
	}
	g := newChatRig(t)
	c := signUp(t, g.s, "siti@example.com")
	if body := request(g.s, "/ai-nlp-bots/stres", c).Body.String(); !strings.Contains(body, "bukan ahli terapi") {
		t.Error("logged-in stres page lacks the notice")
	}
	sys := systemPrompt(b)
	for _, want := range []string{"bukan ahli terapi", "999", "03-7627 2929", "15555", "15999"} {
		if !strings.Contains(sys, want) {
			t.Errorf("stres prompt lacks %q", want)
		}
	}
	edu, _ := findBot("educators")
	if _, body := get(t, s, "/ai-nlp-bots/educators"); strings.Contains(body, "bukan ahli terapi") || !strings.Contains(systemPrompt(edu), "15555") {
		t.Error("notice shown on another bot, or help lines missing from its prompt")
	}
}
