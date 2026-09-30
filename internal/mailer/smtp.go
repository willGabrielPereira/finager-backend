package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// smtpSender envia via SMTP com net/smtp, respeitando o ctx.
// Limitações: sem TLS implícito (465); STARTTLS só se o servidor anunciar;
// PlainAuth exige TLS fora de localhost. Entrega parcial: destinatários recusados
// no RCPT são logados e ignorados; só falha se nenhum for aceito.
type smtpSender struct {
	host string
	port int
	user string
	pass string
	from *mail.Address
}

func (s *smtpSender) Send(ctx context.Context, m Message) error {
	// Validação antes de conectar: bloqueia injeção de cabeçalho (CRLF).
	if len(m.To) == 0 {
		return errors.New("smtp: nenhum destinatário")
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("smtp: assunto contém quebra de linha")
	}
	rcpts := make([]string, 0, len(m.To))
	for _, to := range m.To {
		if strings.ContainsAny(to, "\r\n") {
			return fmt.Errorf("smtp: destinatário %s contém quebra de linha", Mask(to))
		}
		a, err := mail.ParseAddress(to)
		if err != nil {
			return fmt.Errorf("smtp: destinatário inválido %s: %w", Mask(to), err)
		}
		rcpts = append(rcpts, a.Address)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// Cancelamento do ctx fecha a conexão e aborta a sessão em andamento.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close() // logo após NewClient: qualquer retorno abaixo fecha a conexão

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if s.user != "" {
		if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}

	accepted := make([]string, 0, len(rcpts))
	for _, addr := range rcpts {
		if err := c.Rcpt(addr); err != nil {
			slog.Warn("email.rcpt_rejected", "to", Mask(addr), "err", err)
			continue
		}
		accepted = append(accepted, addr)
	}
	if len(accepted) == 0 {
		return errors.New("smtp: todos os destinatários foram recusados")
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	var hdr strings.Builder
	hdr.WriteString("From: " + s.from.String() + "\r\n")
	hdr.WriteString("To: " + strings.Join(accepted, ", ") + "\r\n")
	hdr.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	hdr.WriteString("MIME-Version: 1.0\r\n")
	hdr.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	hdr.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	hdr.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	hdr.WriteString("Message-ID: " + messageID(s.from.Address) + "\r\n\r\n")
	if _, err := w.Write([]byte(hdr.String())); err != nil {
		return fmt.Errorf("smtp: cabeçalhos: %w", err)
	}
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(m.HTML)); err != nil {
		return fmt.Errorf("smtp: corpo: %w", err)
	}
	if err := qp.Close(); err != nil {
		return fmt.Errorf("smtp: corpo: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: fim do DATA: %w", err)
	}
	return c.Quit()
}

// messageID gera <aleatório@domínio-do-remetente>.
func messageID(from string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	domain := from[strings.LastIndex(from, "@")+1:]
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}
