package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SetupMailpit inicia um Mailpit (SMTP fake + API HTTP) via Testcontainers.
// env opcional é repassado ao container (ex.: MP_SMTP_ALLOWED_RECIPIENTS).
func SetupMailpit(t *testing.T, env ...map[string]string) (smtpHost string, smtpPort int, apiURL string, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "axllent/mailpit:v1.31.3",
		ExposedPorts: []string{"1025/tcp", "8025/tcp"},
		// WithPort é obrigatório: sem ele a estratégia testaria a 1ª porta (SMTP).
		WaitingFor: wait.ForHTTP("/readyz").WithPort("8025/tcp").WithStartupTimeout(60 * time.Second),
	}
	if len(env) > 0 {
		req.Env = env[0]
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("Failed to start Mailpit container: %v", err)
	}

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get Mailpit host: %v", err)
	}
	sp, err := c.MappedPort(ctx, "1025/tcp")
	if err != nil {
		t.Fatalf("Failed to get Mailpit SMTP port: %v", err)
	}
	ap, err := c.MappedPort(ctx, "8025/tcp")
	if err != nil {
		t.Fatalf("Failed to get Mailpit API port: %v", err)
	}

	cleanup = func() {
		if err := c.Terminate(context.Background()); err != nil {
			t.Fatalf("Failed to terminate Mailpit container: %v", err)
		}
	}
	return host, int(sp.Num()), fmt.Sprintf("http://%s:%s", host, ap.Port()), cleanup
}

type mailpitAddr struct {
	Name    string
	Address string
}

// WaitForMessage faz polling na API do Mailpit até chegar uma mensagem para `to`
// (timeout de 10s) e devolve assunto, HTML decodificado e endereços do To.
func WaitForMessage(t *testing.T, apiURL, to string) (subject, html string, toList []string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var list struct {
			Messages []struct{ ID string }
		}
		getJSON(t, apiURL+"/api/v1/search?query="+url.QueryEscape(`to:"`+to+`"`), &list)
		if len(list.Messages) > 0 {
			var msg struct {
				Subject string
				HTML    string
				To      []mailpitAddr
			}
			getJSON(t, apiURL+"/api/v1/message/"+list.Messages[0].ID, &msg)
			for _, a := range msg.To {
				toList = append(toList, a.Address)
			}
			return msg.Subject, msg.HTML, toList
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("nenhuma mensagem para %s no Mailpit", to)
	return "", "", nil
}

func getJSON(t *testing.T, u string, out any) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", u, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: decode: %v", u, err)
	}
}
