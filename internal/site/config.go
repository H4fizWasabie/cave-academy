package site

import (
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

// Config holds the runtime settings read from the environment (see docs/spec.md).
type Config struct {
	Env             string // "development" (default) or "production"; see Validate
	Addr            string
	DBPath          string
	OpenRouterKey   string
	OpenRouterModel string
	BrevoKey        string
	MailFrom        string
	AdminEmail      string
	BaseURL         string // public origin for canonical links and the sitemap, no trailing slash
	ChatDailyCap    int    // OpenRouter answers per KL day across all users; 0 = 2000
	IntroVideoURL   string // intro video; empty hides the "(2 Minit)" video buttons
}

// ConfigFromEnv reads the environment and fills in the spec's defaults.
func ConfigFromEnv() Config {
	appEnv := env("APP_ENV", "development")
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" && appEnv != "production" {
		baseURL = "https://caveacademy.my" // production must set it: Validate refuses an empty one
	}
	capN, _ := strconv.Atoi(os.Getenv("CHAT_DAILY_CAP"))
	if capN <= 0 {
		capN = defaultCap
	}
	return Config{
		Env:             appEnv,
		ChatDailyCap:    capN,
		Addr:            env("ADDR", ":8080"),
		DBPath:          env("DB_PATH", "cave-academy.db"),
		OpenRouterKey:   os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel: env("OPENROUTER_MODEL", "openai/gpt-6-luna"),
		BrevoKey:        os.Getenv("BREVO_API_KEY"),
		MailFrom:        env("MAIL_FROM", "The Cave Academy <noreply@caveacademy.my>"),
		AdminEmail:      env("ADMIN_EMAIL", "adam.caveconsult@gmail.com"),
		BaseURL:         strings.TrimRight(baseURL, "/"),
		IntroVideoURL:   os.Getenv("INTRO_VIDEO_URL"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Validate refuses a production start with missing secrets or a bad sender, so
// a half-configured deploy fails loudly instead of silently dropping mail or
// chat. Development keeps the fallbacks.
func (c Config) Validate() error {
	switch c.Env {
	case "development", "":
		return nil
	case "production":
	default:
		return fmt.Errorf("APP_ENV %q: want development or production", c.Env)
	}
	var missing []string
	for _, k := range []struct{ name, val string }{{"OPENROUTER_API_KEY", c.OpenRouterKey}, {"BREVO_API_KEY", c.BrevoKey}, {"BASE_URL", c.BaseURL}} {
		if k.val == "" {
			missing = append(missing, k.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("APP_ENV=production but %s not set", strings.Join(missing, ", "))
	}
	if _, err := mail.ParseAddress(c.MailFrom); err != nil {
		return fmt.Errorf("MAIL_FROM %q: %w", c.MailFrom, err)
	}
	return nil
}
