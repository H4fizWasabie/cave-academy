package site

import "testing"

func TestConfigDefaults(t *testing.T) {
	for _, k := range []string{"ADDR", "DB_PATH", "OPENROUTER_API_KEY", "OPENROUTER_MODEL", "RESEND_API_KEY", "MAIL_FROM", "ADMIN_EMAIL"} {
		t.Setenv(k, "")
	}
	c := ConfigFromEnv()
	if c.Addr != ":8080" || c.DBPath != "cave-academy.db" || c.OpenRouterModel != "openai/gpt-6-luna" ||
		c.MailFrom != "The Cave Academy <noreply@caveacademy.my>" || c.AdminEmail != "adam.caveconsult@gmail.com" ||
		c.OpenRouterKey != "" || c.ResendKey != "" {
		t.Errorf("defaults = %+v", c)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("ADDR", ":9000")
	t.Setenv("RESEND_API_KEY", "re_x")
	c := ConfigFromEnv()
	if c.Addr != ":9000" || c.ResendKey != "re_x" {
		t.Errorf("config = %+v", c)
	}
}
