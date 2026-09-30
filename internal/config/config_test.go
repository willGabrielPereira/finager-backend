package config

import (
	"net/mail"
	"strings"
	"testing"
)

func TestResolveMailFrom(t *testing.T) {
	cases := []struct {
		name, provider, domain, fromName, env string
		wantAddr                              string // "" = nil
		wantErr                               bool
	}{
		{"domínio definido", "resend", "mail.finager.com.br", "Finager", "production", "nao-responda@mail.finager.com.br", false},
		{"nome com acento e vírgula", "smtp", "finager.com.br", "Finager, Gestão & Ação", "development", "nao-responda@finager.com.br", false},
		{"domínio inválido", "smtp", "exa mple.com", "Finager", "development", "", true},
		{"domínio inválido com @", "smtp", "a@b.com", "Finager", "development", "", true},
		{"fallback dev resend", "resend", "", "Finager", "development", "onboarding@resend.dev", false},
		{"fallback dev smtp", "smtp", "", "Finager", "development", "nao-responda@localhost", false},
		{"produção sem domínio", "smtp", "", "Finager", "production", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveMailFrom(tc.provider, tc.domain, tc.fromName, tc.env)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantAddr == "" {
				if got != nil {
					t.Fatalf("esperado nil; veio %v", got)
				}
				return
			}
			if got == nil || got.Address != tc.wantAddr || got.Name != tc.fromName {
				t.Fatalf("endereço = %v, esperado %q <%s>", got, tc.fromName, tc.wantAddr)
			}
			// O From precisa sobreviver a String() -> ParseAddress (RFC 2047).
			re, err := mail.ParseAddress(got.String())
			if err != nil || re.Name != tc.fromName || re.Address != tc.wantAddr {
				t.Fatalf("From não reparseável: %q -> %v, %v", got.String(), re, err)
			}
		})
	}
}

func TestValidateMail(t *testing.T) {
	from := &mail.Address{Name: "Finager", Address: "nao-responda@finager.com.br"}
	prodOK := func() Config {
		return Config{JWTSecret: "x", AppEnv: "production", MailProvider: "smtp", MailFrom: from,
			AppBaseURL: "https://app.finager.com.br", SMTPHost: "smtp.resend.com", SMTPPort: 587}
	}
	cases := []struct {
		name    string
		mut     func(c *Config)
		wantErr string // "" = sem erro
	}{
		{"produção válida", func(c *Config) {}, ""},
		{"provider inválido", func(c *Config) { c.MailProvider = "sendgrid" }, "MAIL_PROVIDER"},
		{"resend sem token", func(c *Config) { c.MailProvider = "resend" }, "RESEND_TOKEN"},
		{"resend sem token fora de produção", func(c *Config) { c.MailProvider = "resend"; c.AppEnv = "development" }, "RESEND_TOKEN"},
		{"resend com token", func(c *Config) { c.MailProvider = "resend"; c.ResendToken = "re_x" }, ""},
		{"produção sem domínio", func(c *Config) { c.MailFrom = nil }, "MAIL_DOMAIN"},
		{"produção com APP_BASE_URL default", func(c *Config) { c.AppBaseURL = "http://localhost:3000" }, "APP_BASE_URL"},
		{"produção smtp localhost", func(c *Config) { c.SMTPHost = "localhost" }, "SMTP_HOST"},
		{"produção none é só warn", func(c *Config) {
			c.MailProvider = "none"
			c.MailFrom = nil
			c.AppBaseURL = "http://localhost:3000"
			c.SMTPHost = "localhost"
		}, ""},
		{"dev com defaults", func(c *Config) {
			c.AppEnv = "development"
			c.AppBaseURL = "http://localhost:3000"
			c.SMTPHost = "localhost"
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := prodOK()
			tc.mut(&c)
			err := c.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("esperado erro contendo %q; veio %v", tc.wantErr, err)
			}
		})
	}
}
