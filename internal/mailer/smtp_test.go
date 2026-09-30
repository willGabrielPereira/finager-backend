package mailer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// Testes de integração com Mailpit (requer Docker).

func newMailpitSender(t *testing.T, env ...map[string]string) (*smtpSender, string) {
	t.Helper()
	host, port, apiURL, cleanup := testutil.SetupMailpit(t, env...)
	t.Cleanup(cleanup)
	from := &mail.Address{Name: "Finager Ação, Ltda", Address: "nao-responda@finager.test"}
	return &smtpSender{host: host, port: port, from: from}, apiURL
}

// mailpitMessages lista as mensagens (resumo) guardadas no Mailpit.
func mailpitMessages(t *testing.T, apiURL string) []struct {
	From struct{ Name, Address string }
	To   []struct{ Address string }
} {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Messages []struct {
			From struct{ Name, Address string }
			To   []struct{ Address string }
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Messages
}

func sendCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestSMTPSenderMailpit(t *testing.T) {
	s, apiURL := newMailpitSender(t)

	// Linha única > 998 bytes com acentos: exige quoted-printable para não quebrar.
	body := "<p>" + strings.Repeat("Atenção à importação de extratos — ", 60) + "</p>"
	if len(body) <= 998 {
		t.Fatalf("corpo de teste curto demais: %d", len(body))
	}
	err := s.Send(sendCtx(t), Message{To: []string{"Joana <joana@aceito.test>"}, Subject: "Redefinição de senha — ação", HTML: body})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	subject, html, to := testutil.WaitForMessage(t, apiURL, "joana@aceito.test")
	if subject != "Redefinição de senha — ação" {
		t.Errorf("assunto = %q", subject)
	}
	if strings.TrimSpace(html) != body {
		t.Errorf("HTML decodificado difere do enviado (len %d vs %d)", len(strings.TrimSpace(html)), len(body))
	}
	if len(to) != 1 || to[0] != "joana@aceito.test" {
		t.Errorf("To = %v", to)
	}
	msgs := mailpitMessages(t, apiURL)
	if len(msgs) != 1 || msgs[0].From.Name != "Finager Ação, Ltda" || msgs[0].From.Address != "nao-responda@finager.test" {
		t.Errorf("From decodificado incorreto: %+v", msgs)
	}
}

func TestSMTPSenderMultipleRecipients(t *testing.T) {
	s, apiURL := newMailpitSender(t)

	err := s.Send(sendCtx(t), Message{To: []string{"ana@aceito.test", "bia@aceito.test"}, Subject: "Lembrete", HTML: "<p>oi</p>"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, _, to := testutil.WaitForMessage(t, apiURL, "ana@aceito.test")
	if len(to) != 2 || to[0] != "ana@aceito.test" || to[1] != "bia@aceito.test" {
		t.Errorf("To = %v, esperado os 2 destinatários", to)
	}
	if n := len(mailpitMessages(t, apiURL)); n != 1 {
		t.Errorf("esperada 1 única mensagem; há %d", n)
	}
}

func TestSMTPSenderPartialRcpt(t *testing.T) {
	// Mailpit recusa no RCPT TO qualquer destinatário fora de @aceito.test.
	s, apiURL := newMailpitSender(t, map[string]string{"MP_SMTP_ALLOWED_RECIPIENTS": `@aceito\.test$`})

	err := s.Send(sendCtx(t), Message{To: []string{"ana@aceito.test", "bia@recusado.test"}, Subject: "Parcial", HTML: "<p>oi</p>"})
	if err != nil {
		t.Fatalf("entrega parcial deveria retornar nil; veio %v", err)
	}
	_, _, to := testutil.WaitForMessage(t, apiURL, "ana@aceito.test")
	if len(to) != 1 || to[0] != "ana@aceito.test" {
		t.Errorf("To = %v, esperado só o aceito", to)
	}

	// Nenhum aceito → erro e nenhuma mensagem nova.
	err = s.Send(sendCtx(t), Message{To: []string{"bia@recusado.test", "caio@recusado.test"}, Subject: "Nada", HTML: "<p>oi</p>"})
	if err == nil {
		t.Fatal("todos recusados deveria retornar erro")
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(mailpitMessages(t, apiURL)); n != 1 {
		t.Errorf("esperada só a mensagem parcial; há %d", n)
	}
}
