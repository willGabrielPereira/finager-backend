package config

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application configuration values loaded from the environment.
type Config struct {
	DatabaseDSN            string
	Port                   string
	JWTSecret              string
	JWTExpiresHours        int
	JWTRefreshExpiresHours int
	CORSAllowedOrigins     []string
	SentryDSN              string
	AppEnv                 string

	// ── E-mail (ver documentation/email.md) ──
	MailProvider         string        // resend | smtp | none (none = kill switch)
	MailDomain           string        // remetente = nao-responda@MailDomain
	MailFromName         string        // nome exibido no From
	MailFrom             *mail.Address // resolvido por resolveMailFrom; nil só em produção sem domínio
	AppBaseURL           string        // URL do front, usada nos links dos e-mails
	ResendToken          string
	SMTPHost             string
	SMTPPort             int
	SMTPUser             string
	SMTPPass             string
	OFXRemindersEnabled  bool
	OFXRemindersDailyCap int // teto de lembretes por dia (execução diária às 19h), medido no banco
}

// Load reads the .env file (if present) and returns a populated Config.
// In production containers the variables should already be present in the
// environment, so a missing .env file is not treated as a fatal error.
// An error is returned if any required variable is absent or empty.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading config from environment variables")
	}

	cfg := &Config{
		DatabaseDSN:            getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/finager?sslmode=disable"),
		Port:                   getEnv("PORT", "8080"),
		JWTSecret:              getEnv("JWT_SECRET", ""),
		JWTExpiresHours:        getEnvInt("JWT_EXPIRATION_HOURS", 1),
		JWTRefreshExpiresHours: getEnvInt("JWT_REFRESH_EXPIRATION_HOURS", 168), // 7 days
		CORSAllowedOrigins:     getEnvSlice("CORS_ALLOWED_ORIGINS", []string{"*"}), // Defaults to *
		SentryDSN:              getEnv("SENTRY_DSN", ""),
		AppEnv:                 getEnv("APP_ENV", "development"),
		MailProvider:           strings.ToLower(strings.TrimSpace(getEnv("MAIL_PROVIDER", "smtp"))),
		MailDomain:             strings.TrimSpace(getEnv("MAIL_DOMAIN", "")),
		MailFromName:           getEnv("MAIL_FROM_NAME", "Finager"),
		AppBaseURL:             getEnv("APP_BASE_URL", "http://localhost:3000"),
		ResendToken:            getEnv("RESEND_TOKEN", ""),
		SMTPHost:               getEnv("SMTP_HOST", "localhost"),
		SMTPPort:               getEnvInt("SMTP_PORT", 1025),
		SMTPUser:               getEnv("SMTP_USER", ""),
		SMTPPass:               getEnv("SMTP_PASS", ""),
		OFXRemindersEnabled:    getEnv("OFX_REMINDERS_ENABLED", "false") == "true",
		OFXRemindersDailyCap:   getEnvInt("OFX_REMINDERS_DAILY_CAP", 48),
	}

	from, err := resolveMailFrom(cfg.MailProvider, cfg.MailDomain, cfg.MailFromName, cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	cfg.MailFrom = from

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if cfg.isProduction() && cfg.MailProvider == "none" {
		slog.Warn("MAIL_PROVIDER=none em produção: nenhum e-mail será enviado (reset de senha, convites, lembretes)")
	}

	return cfg, nil
}

// validate checks that all required fields are present.
func (c *Config) validate() error {
	var missing []string

	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", errors.Join(toErrors(missing)...))
	}

	switch c.MailProvider {
	case "resend", "smtp", "none":
	default:
		return fmt.Errorf("MAIL_PROVIDER inválido %q: use resend, smtp ou none", c.MailProvider)
	}
	if c.MailProvider == "resend" && c.ResendToken == "" {
		return errors.New("MAIL_PROVIDER=resend exige RESEND_TOKEN")
	}

	if c.isProduction() && c.MailProvider != "none" {
		if c.MailFrom == nil {
			return errors.New("MAIL_DOMAIN é obrigatório em produção (ou use MAIL_PROVIDER=none)")
		}
		if !strings.HasPrefix(c.AppBaseURL, "https://") {
			return fmt.Errorf("APP_BASE_URL precisa começar com https:// em produção (atual: %q)", c.AppBaseURL)
		}
		if c.MailProvider == "smtp" && c.SMTPHost == "localhost" {
			return errors.New("SMTP_HOST=localhost não é permitido em produção")
		}
	}

	return nil
}

func (c *Config) isProduction() bool { return c.AppEnv == "production" }

// resolveMailFrom monta o remetente a partir das variáveis já lidas (função pura).
// Com MAIL_DOMAIN: "name <nao-responda@domain>". Sem domínio fora de produção usa
// um remetente de desenvolvimento; em produção devolve nil — validate() decide se é fatal.
func resolveMailFrom(provider, domain, name, appEnv string) (*mail.Address, error) {
	if domain != "" {
		addr, err := mail.ParseAddress("nao-responda@" + domain)
		if err != nil {
			return nil, fmt.Errorf("MAIL_DOMAIN inválido %q: %w", domain, err)
		}
		addr.Name = name
		return addr, nil
	}
	if appEnv == "production" {
		return nil, nil
	}
	if provider == "resend" {
		// Único remetente aceito pelo Resend sem domínio verificado.
		return &mail.Address{Name: name, Address: "onboarding@resend.dev"}, nil
	}
	return &mail.Address{Name: name, Address: "nao-responda@localhost"}, nil
}

func toErrors(ss []string) []error {
	errs := make([]error, len(ss))
	for i, s := range ss {
		errs[i] = errors.New(s)
	}
	return errs
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvSlice(key string, fallback []string) []string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		// Expects a comma-separated string like: "http://localhost:3000,https://meuapp.com"
		var list []string
		for _, v := range strings.Split(value, ",") {
			list = append(list, strings.TrimSpace(v))
		}
		return list
	}
	return fallback
}
