package site

import (
	"os"
	"strings"
)

// Config holds the runtime settings read from the environment (see docs/spec.md).
type Config struct {
	Addr            string
	DBPath          string
	OpenRouterKey   string
	OpenRouterModel string
	BrevoKey        string
	MailFrom        string
	AdminEmail      string
	BaseURL         string // public origin for canonical links and the sitemap, no trailing slash
}

// ConfigFromEnv reads the environment and fills in the spec's defaults.
func ConfigFromEnv() Config {
	return Config{
		Addr:            env("ADDR", ":8080"),
		DBPath:          env("DB_PATH", "cave-academy.db"),
		OpenRouterKey:   os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel: env("OPENROUTER_MODEL", "openai/gpt-6-luna"),
		BrevoKey:        os.Getenv("BREVO_API_KEY"),
		MailFrom:        env("MAIL_FROM", "The Cave Academy <noreply@caveacademy.my>"),
		AdminEmail:      env("ADMIN_EMAIL", "adam.caveconsult@gmail.com"),
		BaseURL:         strings.TrimRight(env("BASE_URL", "https://caveacademy.my"), "/"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
