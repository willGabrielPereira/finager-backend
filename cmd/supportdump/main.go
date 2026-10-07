// cmd/supportdump/main.go — Baixa o dump SQL (DDL + COPY) de uma família via API, sobe o
// Postgres local (docker compose), restaura num banco próprio e aplica uma senha mestre
// local aos usuários, para o suporte logar como qualquer um deles.
// Uso: make support-dump FAMILY=<uuid> REASON="ticket 123" [OUT=dump.sql]
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

// Último trecho de um dump completo (repository.DumpEndMarker); duplicado aqui para
// a CLI não puxar o pacote repository (e o driver do banco) só por uma constante.
const endMarker = "-- finager dump completo"

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "erro: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	_ = godotenv.Load()

	if len(os.Args) < 3 {
		fmt.Println(`Uso: make support-dump FAMILY=<uuid> REASON="ticket 123" [OUT=dump.sql]`)
		fmt.Println("\nVariáveis: FINAGER_API_URL, FINAGER_ADMIN_LOGIN, FINAGER_ADMIN_PASSWORD (opcional), FINAGER_MASTER_PASSWORD e FINAGER_SUPPORT_DB (opcionais)")
		os.Exit(1)
	}
	familyID, reason := os.Args[1], os.Args[2]
	out := "finager-family-" + familyID + ".sql"
	if len(os.Args) > 3 && os.Args[3] != "" {
		out = os.Args[3]
	}

	apiURL := strings.TrimRight(os.Getenv("FINAGER_API_URL"), "/")
	login := os.Getenv("FINAGER_ADMIN_LOGIN")
	if apiURL == "" || login == "" {
		fatal("defina FINAGER_API_URL e FINAGER_ADMIN_LOGIN")
	}
	password := os.Getenv("FINAGER_ADMIN_PASSWORD")
	if password == "" {
		// ponytail: lê a senha com eco (sem dependência nova); use FINAGER_ADMIN_PASSWORD ou x/term se incomodar.
		fmt.Fprint(os.Stderr, "Senha (será exibida): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		password = strings.TrimSpace(line)
	}

	client := &http.Client{} // sem timeout global: o dump pode ser grande; o servidor limita pelo contexto

	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	post(client, apiURL+"/auth/login", "", map[string]string{"login": login, "password": password}, &tokens)

	var elev struct {
		ElevatedToken string `json:"elevated_token"`
	}
	post(client, apiURL+"/auth/elevate", tokens.AccessToken, map[string]string{"password": password}, &elev)

	req, _ := http.NewRequest("GET", apiURL+"/admin/families/"+familyID+"/dump?reason="+url.QueryEscape(reason), nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("X-Admin-Elevation", elev.ElevatedToken)
	resp, err := client.Do(req)
	if err != nil {
		fatal("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fatal("API respondeu %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		fatal("download interrompido: %v", err)
	}

	// Truncamento no meio do stream não muda o status HTTP: confere o marcador final.
	tail := make([]byte, min(n, 256))
	if _, err := f.ReadAt(tail, n-int64(len(tail))); err != nil || !bytes.Contains(tail, []byte(endMarker)) {
		fatal("dump incompleto (marcador final ausente); veja os logs do servidor. Arquivo: %s", out)
	}
	fmt.Printf("dump OK: %d bytes em %s (%s)\n", n, out, time.Now().Format(time.RFC3339))

	restoreLocal(out)
}

// restoreLocal sobe o serviço db do compose, recria o banco de suporte, restaura o dump e
// troca o password_hash de todos os usuários pela senha mestre. A senha mestre nunca vai
// à API: o hash de produção sai como REDACTED e o bcrypt é gerado e aplicado só aqui.
func restoreLocal(dumpFile string) {
	dbName := envOr("FINAGER_SUPPORT_DB", "finager_support")
	master := envOr("FINAGER_MASTER_PASSWORD", "finager-master")

	run(nil, "docker", "compose", "up", "-d", "--wait", "db")
	psql := func(db string, stdin io.Reader, args ...string) {
		run(stdin, "docker", append([]string{"compose", "exec", "-T", "db", "psql", "-U", "postgres", "-d", db, "-v", "ON_ERROR_STOP=1", "-q"}, args...)...)
	}
	psql("postgres", nil, "-c", fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
	psql("postgres", nil, "-c", "CREATE DATABASE "+dbName)

	f, err := os.Open(dumpFile)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()
	psql(dbName, f)

	hash, err := bcrypt.GenerateFromPassword([]byte(master), bcrypt.DefaultCost)
	if err != nil {
		fatal("%v", err)
	}
	// Hash bcrypt só tem [./A-Za-z0-9$]; vai por stdin (sem expansão de shell).
	psql(dbName, strings.NewReader(fmt.Sprintf("UPDATE users SET password_hash = '%s';\n", hash)))

	fmt.Printf(`
Banco local pronto: %[1]s (senha mestre aplicada a todos os usuários)
  Senha mestre: %[2]s   (troque com FINAGER_MASTER_PASSWORD)
  Rodar a API apontando para ele:
    DATABASE_URL=postgres://postgres:postgres@localhost:5432/%[1]s?sslmode=disable make run
  Abrir o psql:
    docker compose exec db psql -U postgres -d %[1]s
`, dbName, master)
}

func run(stdin io.Reader, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fatal("%s %s: %v", name, strings.Join(args[:min(len(args), 4)], " "), err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func post(c *http.Client, endpoint, bearer string, body, dst any) {
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.Do(req)
	if err != nil {
		fatal("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fatal("%s respondeu %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		fatal("resposta inválida de %s: %v", endpoint, err)
	}
}
