// Package mailer isola o envio de e-mail atrás da interface Sender.
// Invariante: e-mail NUNCA derruba o sistema em runtime — todo envio assíncrono
// passa por Go/SendAsync (contexto próprio + recover). Ver documentation/email.md.
package mailer

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/config"
)

// Message é um e-mail HTML para um ou mais destinatários (um único envio).
type Message struct {
	To      []string
	Subject string
	HTML    string
}

// Sender entrega uma Message. Implementações: resendSender e smtpSender.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// New escolhe o provedor por cfg.MailProvider. "none" devolve nil, nil (kill switch):
// quem envia deve checar sender != nil. Erro aqui = config inválida (fatal no boot).
func New(cfg *config.Config) (Sender, error) {
	switch cfg.MailProvider {
	case "none":
		return nil, nil
	case "resend", "smtp":
	default:
		return nil, fmt.Errorf("mailer: MAIL_PROVIDER inválido %q", cfg.MailProvider)
	}
	if cfg.MailFrom == nil {
		return nil, errors.New("mailer: remetente não configurado (MAIL_DOMAIN)")
	}
	if cfg.MailProvider == "resend" {
		if cfg.ResendToken == "" {
			return nil, errors.New("mailer: RESEND_TOKEN ausente")
		}
		return &resendSender{
			token:   cfg.ResendToken,
			from:    cfg.MailFrom,
			baseURL: "https://api.resend.com",
			client:  &http.Client{Timeout: 15 * time.Second},
		}, nil
	}
	return &smtpSender{
		host: cfg.SMTPHost,
		port: cfg.SMTPPort,
		user: cfg.SMTPUser,
		pass: cfg.SMTPPass,
		from: cfg.MailFrom,
	}, nil
}

//go:embed templates/*.html
var templatesFS embed.FS

// Render executa templates/layout.html + templates/<name>.html. O layout
// define o bloco "layout" (header/rodapé comuns) e cada arquivo de e-mail
// define "subject" e "content". html/template escapa os dados automaticamente.
func Render(name string, data any) (subject, htmlBody string, err error) {
	t, err := template.ParseFS(templatesFS, "templates/layout.html", "templates/"+name+".html")
	if err != nil {
		return "", "", fmt.Errorf("mailer: template %q: %w", name, err)
	}
	var sb, hb bytes.Buffer
	if err := t.ExecuteTemplate(&sb, "subject", data); err != nil {
		return "", "", fmt.Errorf("mailer: assunto de %q: %w", name, err)
	}
	if err := t.ExecuteTemplate(&hb, "layout", data); err != nil {
		return "", "", fmt.Errorf("mailer: corpo de %q: %w", name, err)
	}
	// O assunto vai para um cabeçalho (texto puro), não para HTML: desfaz o escape.
	return strings.TrimSpace(html.UnescapeString(sb.String())), hb.String(), nil
}

// RecoverPanic registra e engole panics de envio.
// ATENÇÃO: use sempre `defer mailer.RecoverPanic(event)` — função deferida DIRETA.
// `defer func(){ mailer.RecoverPanic(event) }()` NÃO funciona: recover() só
// intercepta o panic quando chamado diretamente pela função deferida.
func RecoverPanic(event string) {
	if r := recover(); r != nil {
		slog.Error("email.panic", "event", event, "recover", r)
		sentry.CaptureException(fmt.Errorf("email panic (%s): %v", event, r))
	}
}

// Go executa trabalho de e-mail fora do ciclo da requisição: contexto próprio,
// timeout e recover. Único ponto de disparo assíncrono de e-mail do projeto.
func Go(event string, timeout time.Duration, fn func(ctx context.Context)) {
	go func() {
		defer RecoverPanic(event)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		fn(ctx)
	}()
}

// SendAsync envia m em segundo plano (melhor esforço). No-op se s == nil.
func SendAsync(s Sender, m Message, event string) {
	if s == nil {
		return
	}
	Go(event, 10*time.Second, func(ctx context.Context) {
		to := maskAll(m.To)
		if err := s.Send(ctx, m); err != nil {
			slog.Error("email.failed", "event", event, "to_count", len(m.To), "to", to, "err", err)
			sentry.CaptureException(fmt.Errorf("email failed (%s): %w", event, err))
			return
		}
		slog.Info("email.sent", "event", event, "to_count", len(m.To), "to", to)
	})
}

// Mask esconde o endereço para logs: "joao@x.com" -> "j***@x.com".
func Mask(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 1 {
		return "***"
	}
	return addr[:1] + "***" + addr[at:]
}

func maskAll(addrs []string) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = Mask(a)
	}
	return out
}

// UnsubscribeSig assina o descadastro do usuário (HMAC-SHA256, hex).
func UnsubscribeSig(secret []byte, userID uuid.UUID) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("unsubscribe:" + userID.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyUnsubscribe confere a assinatura em tempo constante.
func VerifyUnsubscribe(secret []byte, userID uuid.UUID, sig string) bool {
	return hmac.Equal([]byte(UnsubscribeSig(secret, userID)), []byte(sig))
}
