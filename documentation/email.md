# E-mail — Finager API

Pacote `internal/mailer`: interface `Sender` com dois provedores stdlib (sem SDK) e um kill switch.

- `resend` — API HTTP do Resend (`POST https://api.resend.com/emails`).
- `smtp` — `net/smtp` (Mailpit em dev, qualquer relay SMTP em produção).
- `none` — `mailer.New` devolve `nil`: nenhum e-mail é enviado. Quem envia checa `sender != nil`.

## Invariante: e-mail nunca derruba o sistema

- **Todo** envio assíncrono passa por `mailer.Go(event, timeout, fn)` ou `mailer.SendAsync(sender, msg, event)`: contexto próprio (`context.Background()` + timeout, **nunca** `r.Context()`) e `recover`. Nada de `go func` cru para e-mail.
- Nenhum handler responde 5xx por causa de e-mail: a resposta nunca depende do envio.
- Loops que enviam (job OFX) isolam cada iteração com `defer mailer.RecoverPanic(event)` — **sempre como função deferida direta**. `defer func(){ mailer.RecoverPanic(event) }()` **não funciona**: `recover()` só intercepta o panic quando chamado diretamente pela função deferida.
- Falhas geram `email.failed` / `email.panic` no log (+ Sentry). Endereços sempre mascarados (`mailer.Mask`), nunca logar token nem corpo.
- Exceção declarada: erro de configuração de e-mail em `mailer.New`/`config.Load` é **fatal no boot** (`os.Exit(1)`) — isso é config inválida, não runtime.

## Variáveis de ambiente

| Variável | Default | Regra |
|---|---|---|
| `MAIL_PROVIDER` | `smtp` | `resend` \| `smtp` \| `none`. Outro valor → erro fatal. |
| `MAIL_DOMAIN` | — | Remetente = `MAIL_FROM_NAME <nao-responda@MAIL_DOMAIN>`. Obrigatório em produção (provider ≠ `none`). Domínio inválido → erro fatal. |
| `MAIL_FROM_NAME` | `Finager` | Nome exibido; acentos/vírgulas são codificados (RFC 2047) por `mail.Address.String()`. |
| `APP_BASE_URL` | `http://localhost:3000` | URL do **front** usada nos links. Em produção precisa de `https://`. |
| `RESEND_TOKEN` | — | Obrigatório se `MAIL_PROVIDER=resend` (fatal sem ele). |
| `SMTP_HOST` / `SMTP_PORT` | `localhost` / `1025` | Mailpit em dev. Produção: porta 587 e host ≠ `localhost` (fatal). |
| `SMTP_USER` / `SMTP_PASS` | — | Auth `PLAIN` só se `SMTP_USER` definido. |
| `OFX_REMINDERS_ENABLED` | `false` | Liga o job de lembrete OFX. Ignorado com `MAIL_PROVIDER=none`. |
| `OFX_REMINDERS_DAILY_CAP` | `48` | Teto de lembretes (famílias = e-mails) **por dia (execução das 19h BRT), contado no banco** (janela de 20h). |

Sem `MAIL_DOMAIN` fora de produção, o remetente é `onboarding@resend.dev` (resend) ou `nao-responda@localhost` (smtp). Em produção com `MAIL_PROVIDER=none` a API sobe só com um `Warn`.

## Como trocar de provedor

1. Ajuste `MAIL_PROVIDER` (`resend` ou `smtp`) e as variáveis do provedor.
2. Em produção garanta `APP_ENV=production`, `MAIL_DOMAIN` verificado e `APP_BASE_URL` com `https://` **antes** do deploy — a API não sobe com config inválida.
3. Para desligar todos os e-mails imediatamente: `MAIL_PROVIDER=none` e reinicie.

Diferença de semântica com vários destinatários:
- **Resend é tudo-ou-nada**: um erro (ex.: 422) rejeita o envio inteiro.
- **SMTP entrega parcial**: destinatários recusados no `RCPT TO` são logados (`email.rcpt_rejected`) e ignorados; o `To:` lista só os aceitos. Só retorna erro se nenhum for aceito.

## Mailpit em desenvolvimento

- `docker compose up -d mailpit` (ou `make up`, que já configura `SMTP_HOST=mailpit` na API).
- UI: http://localhost:8025 — SMTP em `localhost:1025` (defaults da config).
- Testes: `internal/testutil/mailpit.go` sobe o Mailpit via Testcontainers (`SetupMailpit`, `WaitForMessage`). `go test ./internal/mailer/... -v` requer Docker.

### Verificação manual: Mailpit fora do ar não derruba a API

Prova end-to-end do invariante "e-mail nunca derruba o sistema" (rodar sempre que mexer no
pacote `mailer` ou nos handlers que disparam e-mail):

1. Suba o ambiente (`make up`) com `MAIL_PROVIDER=smtp` (default) e pare só o Mailpit:
   `docker compose stop mailpit`.
2. Repita as três chamadas que disparam e-mail (forgot, convite de família, troca de e-mail)
   e confirme que a resposta HTTP continua igual à de sempre — nenhuma delas espera o envio:
   - `POST /auth/password/forgot` → **202**
   - `POST /family/invites` (com `target_email`, autenticado) → **201**
   - `PUT /me` (com `email` + `current_password`, autenticado) → **200**
3. Confira o log da API: cada chamada gera um `email.failed` (evento `password_reset`,
   `family_invite` ou `email_changed`) com o endereço mascarado, e nada além disso — sem 5xx,
   sem stack trace, sem crash.
4. `docker compose start mailpit` para religar o envio normal.

Resultado real (verificado em 2026-09-28, `finager_api` reconstruído da branch atual,
`finager_mailpit` parado via `docker compose stop mailpit`):

```
POST /auth/password/forgot           -> HTTP 202
POST /family/invites (target_email)  -> HTTP 201
PUT  /me (email + current_password)  -> HTTP 200

{"level":"ERROR","msg":"email.failed","event":"password_reset","to":"w***@gmail.com","err":"smtp: dial: dial tcp: lookup mailpit on 127.0.0.11:53: no such host"}
{"level":"ERROR","msg":"email.failed","event":"email_changed","to":"v***@example.com","err":"smtp: dial: dial tcp: lookup mailpit on 127.0.0.11:53: no such host"}
{"level":"ERROR","msg":"email.failed","event":"family_invite","to":"c***@example.com","err":"smtp: dial: dial tcp: lookup mailpit on 127.0.0.11:53: no such host"}
```

Nenhuma resposta HTTP saiu diferente do caminho feliz; o único efeito observável da queda do
Mailpit foi o log `email.failed` mascarado — invariante confirmado.

## Limitações do SMTP

- Sem TLS implícito (porta 465 **não** suportada).
- STARTTLS é usado quando o servidor anuncia.
- `PlainAuth` do Go recusa enviar credenciais sem TLS fora de `localhost`.
- Corpo em `quoted-printable` (linhas > 998 bytes e UTF-8 seguros); só HTML, sem parte `text/plain`.

## Lembrete OFX (1 e-mail por família)

- Família elegível: nenhuma transação OFX importada há 30 dias (ou criada há 30 dias sem importações) e ainda não lembrada desde a última importação.
- **Exatamente 1 envio por família**, com todos os membros elegíveis no `To` do mesmo e-mail. Cada membro tem seu próprio link de descadastro assinado (HMAC) no rodapé.
- Destinatários (`UserRepository.ListReminderRecipients`): `users.family_id = F` **e** linha em `family_members(F, u)` (ex-membro removido via `RemoveMember` ou migrado via `Join` nunca aparece), `role = 'user'`, com e-mail, sem opt-out e engajado: login/cadastro nos últimos 180 dias **ou** refresh token válido (não revogado, não expirado).
- E-mail que falha em `mail.ParseAddress` (cadastros antigos) é descartado individualmente (`ofx_reminder.invalid_recipient`, endereço mascarado); família sem destinatário válido é pulada.
- Job: `internal/jobs/ofx_reminder.go` — roda 1x/dia às 19h no fuso `America/Sao_Paulo` (fora do horário comercial). **Não** dispara no boot: sempre espera o próximo 19h, então reinícios não mudam o horário; só inicia com `MAIL_PROVIDER ≠ none` **e** `OFX_REMINDERS_ENABLED=true`. No shutdown a API espera até 15s o ciclo em andamento antes de fechar o banco.
- Descadastro: link `APP_BASE_URL/descadastrar?u=<user_id>&s=<hmac>` (HMAC com `JWT_SECRET`; rotacionar o segredo invalida links antigos). A página do front faz `POST /email/unsubscribe` com `{"u": ..., "s": ...}` → 204 (idempotente) ou 400. É POST porque GET não deve mutar estado.

### Diagnóstico antes de ligar `OFX_REMINDERS_ENABLED` em produção

Rode esta query e avalie o resultado **antes** de ativar o lembrete:

```sql
SELECT count(*) FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM family_members fm
    WHERE fm.user_id = u.id AND fm.family_id = u.family_id
);
```

Usuários retornados por ela têm `users.family_id` apontando para uma família sem a linha correspondente em `family_members` (ex.: removidos via `RemoveMember`, que não atualiza `users.family_id`). Eles **não recebem o lembrete** — mesmo comportamento de já não aparecerem em `GET /family/members`. Isso é intencional (impede vazar e-mail de ex-membro no `To:`); se a contagem for inesperadamente alta, investigue a consistência dos dados antes de ligar o job.

### Cálculo de cota

- Lembretes/dia ≤ `OFX_REMINDERS_DAILY_CAP`. O teto é medido no banco (`ofx_reminder_sent_at` nas últimas 20h — folga que cobre "1 execução por dia" sem cálculo de dia-calendário em SQL), então reinícios e réplicas não o multiplicam. Corrida residual: réplicas rodando no mesmo instante podem somar até +`DAILY_CAP` por réplica extra naquele disparo.
- Famílias reivindicadas sem destinatário elegível também contam no teto.
- Regra: `DAILY_CAP ≤ COTA_DIÁRIA − reserva_segurança`, com **reserva ≥ 50% da cota diária** para e-mails de segurança (reset de senha, aviso de troca de e-mail, convites).
- Plano grátis do Resend (100/dia): default `48` → ≤ 48 lembretes/dia, sobrando ≥ 52 para segurança. Plano pago → recalcular.
- Ritmo: pausa de 600ms entre envios → 48 famílias ≈ 29s num único ciclo, abaixo dos ~2 rps do Resend.

## Bounces e blacklist — decisão G-A (não implementar agora)

Não há tabela de supressão nem webhook. Motivo: com Resend HTTP e SMTP via relay o bounce é **assíncrono** — a chamada síncrona aceita o endereço e não revela o bounce. Endereços são validados no cadastro e no `PUT /me`, o volume é baixo e o Resend mantém supressão própria de hard bounces/complaints.

Reabrir a decisão (tabela `email_suppressions` + adaptador de webhook por provedor) se ocorrer qualquer gatilho:
1. plano pago ou volume > ~1.000 e-mails/dia;
2. painel do provedor com bounce > 2% ou complaint > 0,1%;
3. troca para provedor sem supressão própria;
4. aviso de reputação/suspensão do provedor.

## Verificação de domínio no Resend (passo a passo)

1. Use um subdomínio dedicado como `MAIL_DOMAIN` (ex.: `mail.seudominio.com.br`) para isolar a reputação do domínio principal.
2. No painel do Resend: *Domains → Add Domain* com o valor de `MAIL_DOMAIN`.
3. No DNS, crie os registros que o Resend mostrar **no `MAIL_DOMAIN`**: SPF (TXT/MX do subdomínio de envio) e DKIM (TXT `resend._domainkey`).
4. Crie o DMARC no **domínio principal**: TXT `_dmarc.seudominio.com.br` = `v=DMARC1; p=none; rua=mailto:dmarc@seudominio.com.br` (endurecer para `quarantine` depois de acompanhar os relatórios).
5. Volte ao painel e clique em *Verify*; aguarde todos os registros ficarem verdes.
6. Configure `MAIL_PROVIDER=resend`, `RESEND_TOKEN`, `MAIL_DOMAIN`, `APP_ENV=production`, `APP_BASE_URL=https://...` e reinicie.
7. Teste: dispare um e-mail real (ex.: recuperação de senha) para uma caixa Gmail e abra *Mostrar original* — confira `SPF: PASS`, `DKIM: PASS` e `DMARC: PASS`.
