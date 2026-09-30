package handlers_test

import (
	"net/http"
	"net/mail"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/config"
	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// TestPasswordResetE2EMailpit: servidor HTTP real (httptest.NewServer) + smtpSender
// real no Mailpit. Prova que o envio em goroutine não depende de r.Context().
func TestPasswordResetE2EMailpit(t *testing.T) {
	host, port, apiURL, cleanup := testutil.SetupMailpit(t)
	t.Cleanup(cleanup)

	sender, err := mailer.New(&config.Config{
		MailProvider: "smtp",
		MailFrom:     &mail.Address{Name: "Finager", Address: "nao-responda@localhost"},
		SMTPHost:     host,
		SMTPPort:     port,
	})
	require.NoError(t, err)

	e := setupResetEnvDefault(t, sender)
	u := e.createUser(t)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	subject, html, to := testutil.WaitForMessage(t, apiURL, u.Email)
	assert.Contains(t, subject, "Redefinição de senha")
	assert.Equal(t, []string{u.Email}, to)
	token := tokenFromHTML(t, html)

	code, _ := e.reset(t, token, "senha-nova-456")
	require.Equal(t, http.StatusNoContent, code)

	code, _ = e.login(t, u.Email, "senha-nova-456")
	assert.Equal(t, http.StatusOK, code)
	code, _ = e.login(t, u.Email, u.Password)
	assert.Equal(t, http.StatusUnauthorized, code)
}
