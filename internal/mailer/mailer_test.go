package mailer

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/config"
)

func TestNewByProvider(t *testing.T) {
	from := &mail.Address{Name: "Finager", Address: "nao-responda@localhost"}

	s, err := New(&config.Config{MailProvider: "none"})
	if s != nil || err != nil {
		t.Fatalf("none: esperado nil, nil; veio %v, %v", s, err)
	}
	s, err = New(&config.Config{MailProvider: "smtp", MailFrom: from, SMTPHost: "localhost", SMTPPort: 1025})
	if _, ok := s.(*smtpSender); !ok || err != nil {
		t.Fatalf("smtp: esperado *smtpSender; veio %T, %v", s, err)
	}
	s, err = New(&config.Config{MailProvider: "resend", MailFrom: from, ResendToken: "tok"})
	if _, ok := s.(*resendSender); !ok || err != nil {
		t.Fatalf("resend: esperado *resendSender; veio %T, %v", s, err)
	}
	if _, err := New(&config.Config{MailProvider: "resend", MailFrom: from}); err == nil {
		t.Fatal("resend sem token deveria falhar")
	}
	if _, err := New(&config.Config{MailProvider: "smtp"}); err == nil {
		t.Fatal("smtp sem remetente deveria falhar")
	}
	if _, err := New(&config.Config{MailProvider: "sendgrid", MailFrom: from}); err == nil {
		t.Fatal("provider inválido deveria falhar")
	}
}

func TestResendSenderSendsToArray(t *testing.T) {
	var gotAuth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("requisição inesperada: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &resendSender{token: "tok123", from: &mail.Address{Name: "Finager Ação", Address: "nao-responda@x.com"}, baseURL: srv.URL, client: srv.Client()}
	err := s.Send(context.Background(), Message{To: []string{"a@x.com", "b@x.com"}, Subject: "Oi", HTML: "<p>oi</p>"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	to, ok := got["to"].([]any)
	if !ok || len(to) != 2 || to[0] != "a@x.com" || to[1] != "b@x.com" {
		t.Errorf("to deveria ser array com 2 endereços; veio %#v", got["to"])
	}
	if got["subject"] != "Oi" || got["html"] != "<p>oi</p>" {
		t.Errorf("corpo inesperado: %#v", got)
	}
	if f, _ := mail.ParseAddress(got["from"].(string)); f == nil || f.Name != "Finager Ação" {
		t.Errorf("from não reparseável: %v", got["from"])
	}
}

func TestResendSenderNon2xxReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid to"}` + strings.Repeat("x", 2000)))
	}))
	defer srv.Close()

	s := &resendSender{token: "t", from: &mail.Address{Address: "a@x.com"}, baseURL: srv.URL, client: srv.Client()}
	err := s.Send(context.Background(), Message{To: []string{"a@x.com"}, Subject: "s", HTML: "h"})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "invalid to") {
		t.Fatalf("esperado erro com status e corpo; veio %v", err)
	}
	if len(err.Error()) > 600 {
		t.Errorf("corpo deveria ser truncado a 512 bytes; erro tem %d", len(err.Error()))
	}
}

func TestResendSenderCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &resendSender{token: "t", from: &mail.Address{Address: "a@x.com"}, baseURL: srv.URL, client: srv.Client()}
	if err := s.Send(ctx, Message{To: []string{"a@x.com"}}); err == nil {
		t.Fatal("ctx cancelado deveria retornar erro")
	}
}

func TestSMTPSenderRejectsInjectionInAnyRecipient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var dials atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			dials.Add(1)
			c.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	s := &smtpSender{host: "127.0.0.1", port: addr.Port, from: &mail.Address{Address: "a@localhost"}}
	cases := []Message{
		{To: []string{"ok@x.com", "b@x.com\r\nBcc: vitima@x.com"}, Subject: "s"},
		{To: []string{"ok@x.com\n"}, Subject: "s"},
		{To: []string{"ok@x.com"}, Subject: "s\r\nBcc: vitima@x.com"},
		{To: []string{"ok@x.com", "sem-arroba"}, Subject: "s"},
		{To: nil, Subject: "s"},
	}
	for i, m := range cases {
		if err := s.Send(context.Background(), m); err == nil {
			t.Errorf("caso %d: esperado erro", i)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := dials.Load(); n != 0 {
		t.Fatalf("não deveria conectar; houve %d conexões", n)
	}
}

func TestRenderTemplates(t *testing.T) {
	type link struct{ Login, URL string }
	evil := `<script>alert(1)</script>`
	cases := map[string]any{
		"password_reset": map[string]any{"Login": evil, "Link": "https://app/redefinir-senha#token=abc"},
		"email_changed":  map[string]any{"Login": evil, "NewEmail": "n***@x.com"},
		"family_invite":  map[string]any{"FamilyName": evil, "InviterLogin": "ana", "Link": "https://app/convite#token=abc"},
		"ofx_reminder": map[string]any{"FamilyName": evil, "AppURL": "https://app",
			"Unsubscribe": []link{{"ana", "https://app/descadastrar?u=1&s=a"}, {"bia", "https://app/descadastrar?u=2&s=b"}}},
	}
	for name, data := range cases {
		subject, html, err := Render(name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if subject == "" || strings.ContainsAny(subject, "\r\n") {
			t.Errorf("%s: assunto inválido %q", name, subject)
		}
		if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
			t.Errorf("%s: dado não escapado no HTML", name)
		}
	}

	_, html, _ := Render("email_changed", cases["email_changed"])
	if strings.Contains(html, "href") {
		t.Error("email_changed não deve conter link")
	}
	_, html, _ = Render("ofx_reminder", cases["ofx_reminder"])
	if strings.Count(html, "descadastrar?u=") != 2 {
		t.Error("ofx_reminder deveria ter 1 link de descadastro por destinatário")
	}
	subject, _, _ := Render("family_invite", map[string]any{"FamilyName": "Silva & Cia", "InviterLogin": "ana"})
	if !strings.Contains(subject, "Silva & Cia") {
		t.Errorf("assunto não deveria ficar escapado em HTML: %q", subject)
	}
	if _, _, err := Render("nao_existe", nil); err == nil {
		t.Error("template inexistente deveria falhar")
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"joao@x.com": "j***@x.com",
		"a@b.c":      "a***@b.c",
		"semarroba":  "***",
		"@x.com":     "***",
		"":           "***",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, esperado %q", in, got, want)
		}
	}
}

func TestUnsubscribeSig(t *testing.T) {
	secret := []byte("segredo")
	u1, u2 := uuid.New(), uuid.New()
	sig := UnsubscribeSig(secret, u1)

	if !VerifyUnsubscribe(secret, u1, sig) {
		t.Error("assinatura válida rejeitada")
	}
	tampered := []byte(sig)
	tampered[0] ^= 1
	if VerifyUnsubscribe(secret, u1, string(tampered)) {
		t.Error("assinatura adulterada aceita")
	}
	if VerifyUnsubscribe(secret, u2, sig) {
		t.Error("assinatura de outro usuário aceita")
	}
	if VerifyUnsubscribe([]byte("outro"), u1, sig) {
		t.Error("assinatura com outro segredo aceita")
	}
	if VerifyUnsubscribe(secret, u1, "") {
		t.Error("assinatura vazia aceita")
	}
}

type panicSender struct{}

func (panicSender) Send(context.Context, Message) error { panic("boom") }

type chanSender chan Message

func (c chanSender) Send(_ context.Context, m Message) error { c <- m; return nil }

func TestSendAsyncRecoversFromPanic(t *testing.T) {
	SendAsync(panicSender{}, Message{To: []string{"a@x.com"}}, "test_panic")
	SendAsync(nil, Message{To: []string{"a@x.com"}}, "test_nil") // kill switch: no-op

	ok := make(chanSender, 1)
	SendAsync(ok, Message{To: []string{"b@x.com"}, Subject: "vivo"}, "test_ok")
	select {
	case m := <-ok:
		if m.Subject != "vivo" {
			t.Fatalf("mensagem inesperada: %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("segundo SendAsync não entregou")
	}
}
