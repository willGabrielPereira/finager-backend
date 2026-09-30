package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
)

// resendSender fala direto com a API HTTP do Resend (sem SDK).
// Tudo-ou-nada: um erro (ex.: 422) rejeita o envio para todos os destinatários.
type resendSender struct {
	token   string
	from    *mail.Address
	baseURL string // https://api.resend.com; configurável para testes com httptest
	client  *http.Client
}

func (s *resendSender) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(map[string]any{
		"from":    s.from.String(),
		"to":      m.To,
		"subject": m.Subject,
		"html":    m.HTML,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: status %d: %s", resp.StatusCode, msg)
	}
	return nil
}
