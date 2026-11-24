package site

import "testing"

func TestConfigDefaults(t *testing.T) {
	for _, k := range []string{"ADDR", "DB_PATH", "OPENROUTER_API_KEY", "OPENROUTER_MODEL", "BREVO_API_KEY", "MAIL_FROM", "ADMIN_EMAIL"} {
		t.Setenv(k, "")
	}
	c := ConfigFromEnv()
	if c.Addr != ":8080" || c.DBPath != "cave-academy.db" || c.OpenRouterModel != "openai/gpt-6-luna" ||
		c.MailFrom != "The Cave Academy <noreply@thecaveacademy.com>" || c.AdminEmail != "adam.caveconsult@gmail.com" ||
		c.OpenRouterKey != "" || c.BrevoKey != "" {
		t.Errorf("defaults = %+v", c)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("ADDR", ":9000")
	t.Setenv("BREVO_API_KEY", "xkeysib-x")
	c := ConfigFromEnv()
	if c.Addr != ":9000" || c.BrevoKey != "xkeysib-x" {
		t.Errorf("config = %+v", c)
	}
}

func TestConfigBaseURL(t *testing.T) {
	t.Setenv("BASE_URL", "")
	if c := ConfigFromEnv(); c.BaseURL != "https://thecaveacademy.com" {
		t.Errorf("default BaseURL = %q", c.BaseURL)
	}
	t.Setenv("BASE_URL", "http://localhost:8080")
	if c := ConfigFromEnv(); c.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestConfigIntroVideoURL(t *testing.T) {
	t.Setenv("INTRO_VIDEO_URL", "")
	if c := ConfigFromEnv(); c.IntroVideoURL != "" {
		t.Errorf("default IntroVideoURL = %q, want empty", c.IntroVideoURL)
	}
	t.Setenv("INTRO_VIDEO_URL", "https://youtu.be/x")
	if c := ConfigFromEnv(); c.IntroVideoURL != "https://youtu.be/x" {
		t.Errorf("IntroVideoURL = %q", c.IntroVideoURL)
	}
}
