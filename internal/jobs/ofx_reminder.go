// Package jobs contém tarefas periódicas executadas em segundo plano pela API.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata" // embute o banco IANA de fusos no binário: nextDailyRun não depende do
	// SO/imagem ter tzdata instalado (dev local, outra distro, imagem mínima futura).

	"github.com/getsentry/sentry-go"

	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// ofxStaleAfter é o tempo sem importação OFX que torna a família elegível ao lembrete.
const ofxStaleAfter = 30 * 24 * time.Hour

type unsubLink struct {
	Login string
	URL   string
}

type ofxReminderData struct {
	FamilyName  string
	AppURL      string
	Unsubscribe []unsubLink
}

// RunOFXReminders executa um ciclo do lembrete OFX: reivindica até dailyCap famílias
// (teto diário medido no banco, janela de 20h) e envia EXATAMENTE 1 e-mail por família, com todos os
// destinatários elegíveis no To e um link de descadastro assinado por destinatário.
// Cada família é isolada por recover: um panic numa família não interrompe as demais.
// Retorna ctx.Err() se cancelado durante a pausa entre famílias.
func RunOFXReminders(
	ctx context.Context,
	families *repository.FamilyRepository,
	users *repository.UserRepository,
	sender mailer.Sender,
	appBaseURL string,
	unsubSecret []byte,
	dailyCap int,
	pause time.Duration,
) (sent int, err error) {
	start := time.Now()
	appBaseURL = strings.TrimRight(appBaseURL, "/")

	claimed, err := families.ClaimStaleOFXReminders(ctx, time.Now().Add(-ofxStaleAfter), dailyCap)
	if err != nil {
		return 0, err
	}

	recipientsTotal := 0
	defer func() {
		slog.Info("ofx_reminder.run", "families_claimed", len(claimed), "emails_sent", sent,
			"recipients_total", recipientsTotal, "duration_ms", time.Since(start).Milliseconds())
	}()

	for i, fam := range claimed {
		if i > 0 {
			// Pausa cancelável entre famílias (respeita o rate limit do provedor).
			select {
			case <-ctx.Done():
				return sent, ctx.Err()
			case <-time.After(pause):
			}
		}

		func() {
			defer mailer.RecoverPanic("ofx_reminder") // panic nesta família não interrompe as demais

			recips, err := users.ListReminderRecipients(ctx, fam.ID)
			if err != nil {
				slog.Error("ofx_reminder.failed", "family_id", fam.ID, "err", err)
				return
			}

			data := ofxReminderData{FamilyName: fam.Name, AppURL: appBaseURL}
			to := make([]string, 0, len(recips))
			for _, r := range recips {
				// Endereços antigos só passaram pela validação fraca: descarta o inválido
				// em vez de derrubar o envio da família inteira.
				addr, err := mail.ParseAddress(r.Email)
				if err != nil {
					slog.Warn("ofx_reminder.invalid_recipient", "family_id", fam.ID, "to", mailer.Mask(r.Email))
					continue
				}
				to = append(to, addr.Address)
				data.Unsubscribe = append(data.Unsubscribe, unsubLink{
					Login: r.Login,
					URL:   appBaseURL + "/descadastrar?u=" + r.ID.String() + "&s=" + mailer.UnsubscribeSig(unsubSecret, r.ID),
				})
			}
			if len(to) == 0 {
				slog.Info("ofx_reminder.no_recipients", "family_id", fam.ID)
				return
			}

			subject, html, err := mailer.Render("ofx_reminder", data)
			if err != nil {
				slog.Error("ofx_reminder.failed", "family_id", fam.ID, "err", err)
				sentry.CaptureException(err)
				return
			}

			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := sender.Send(sendCtx, mailer.Message{To: to, Subject: subject, HTML: html}); err != nil {
				slog.Error("ofx_reminder.send_failed", "family_id", fam.ID, "to_count", len(to), "err", err)
				sentry.CaptureException(fmt.Errorf("email failed (ofx_reminder): %w", err))
				return
			}
			sent++
			recipientsTotal += len(to)
		}()
	}
	return sent, nil
}

// scheduledHour e scheduledMinute definem o horário diário do lembrete OFX: 19h no
// fuso de Brasília (fora do horário comercial — usuários costumam estar disponíveis
// para importar o extrato).
const (
	scheduledHour   = 19
	scheduledMinute = 0
)

var reminderLocation = mustLoadLocation("America/Sao_Paulo")

// mustLoadLocation só falha se o SO não tiver dados de fuso horário — erro de
// ambiente/config, detectável no boot, não uma condição de runtime a tratar.
func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("jobs: fuso horário inválido " + name + ": " + err.Error())
	}
	return loc
}

// nextDailyRun devolve o próximo horário (hoje às scheduledHour, ou amanhã se já
// passou) em que o lembrete deve rodar, calculado no fuso reminderLocation.
func nextDailyRun(now time.Time) time.Time {
	local := now.In(reminderLocation)
	next := time.Date(local.Year(), local.Month(), local.Day(), scheduledHour, scheduledMinute, 0, 0, reminderLocation)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// StartOFXReminders roda run todo dia às scheduledHour (fuso reminderLocation), até
// ctx ser cancelado. NÃO dispara logo após o boot — sempre espera o próximo horário
// agendado, para o horário do lembrete ficar previsível independente de reinícios.
// Cada ciclo é isolado por recover: um panic nunca impede o próximo dia.
// O channel devolvido é fechado quando o job termina.
func StartOFXReminders(ctx context.Context, run func(context.Context)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		cycle := func() {
			defer mailer.RecoverPanic("ofx_reminder.cycle")
			run(ctx)
		}
		for {
			wait := time.Until(nextDailyRun(time.Now()))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			cycle()
		}
	}()
	return done
}
