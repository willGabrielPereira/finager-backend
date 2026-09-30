// @title			Finager API
// @version		1.0
// @description	API REST para gestão financeira: importação de extratos OFX e tagueamento de transações.
// @host			localhost:8080
// @BasePath		/
// @schemes		http https
//
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Informe o token no formato: Bearer {token}
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"

	_ "github.com/willGabrielPereira/finager-backend/docs" // gerado pelo swag init

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/config"
	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/jobs"
	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// ── Configuração ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		logger.Error("erro de configuração", "err", err)
		os.Exit(1)
	}

	// ── Sentry (error tracking) — no-op se SENTRY_DSN não estiver definido ─────
	if cfg.SentryDSN != "" {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:         cfg.SentryDSN,
			Environment: cfg.AppEnv,
		}); err != nil {
			logger.Error("falha ao inicializar sentry", "err", err)
		}
		defer sentry.Flush(2 * time.Second)
	}

	// ── Banco de dados ────────────────────────────────────────────────────────
	db, err := database.Connect(cfg.DatabaseDSN)
	if err != nil {
		logger.Error("falha ao conectar no PostgreSQL", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// 3. Inicializa repositórios injetando o Pool
	repos := repository.New(db.Pool)

	// ── Serviço de Auth ───────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpiresHours)

	// ── E-mail — nil com MAIL_PROVIDER=none (kill switch); erro = config inválida ──
	sender, err := mailer.New(cfg)
	if err != nil {
		logger.Error("configuração de e-mail inválida", "err", err)
		os.Exit(1)
	}

	// ── Roteamento ────────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	registerRoutes(mux, repos, authSvc, cfg, sender)

	// ── Middlewares globais (Security, CORS e Logging/Sentry) ──────────────────
	rootHandler := middleware.SecurityHeaders(mux)
	rootHandler = middleware.CORS(cfg.CORSAllowedOrigins)(rootHandler)
	rootHandler = middleware.RequestLogger(logger)(rootHandler)

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Jobs em segundo plano — lembrete OFX só com e-mail ligado E flag ativa ──
	jobsCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	var jobsDone <-chan struct{}
	if sender != nil && cfg.OFXRemindersEnabled {
		unsubSecret := []byte(cfg.JWTSecret)
		jobsDone = jobs.StartOFXReminders(jobsCtx, func(ctx context.Context) {
			// ponytail: pausa de 600ms ≈ 1,6 envios/s por réplica, abaixo dos ~2 rps do Resend
			if _, err := jobs.RunOFXReminders(ctx, repos.Families, repos.Users, sender, cfg.AppBaseURL, unsubSecret, cfg.OFXRemindersDailyCap, 600*time.Millisecond); err != nil && ctx.Err() == nil {
				logger.Error("ofx_reminder.failed", "err", err)
			}
		})
		logger.Info("ofx_reminder.enabled", "daily_cap", cfg.OFXRemindersDailyCap)
	}

	go func() {
		logger.Info("Finager API listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "err", err)
		os.Exit(1)
	}
	logger.Info("server stopped")

	// Para os jobs e espera o ciclo em andamento ANTES do db.Close() (defer acima),
	// para um envio não perder a conexão no meio.
	stopJobs()
	if jobsDone != nil {
		select {
		case <-jobsDone:
		case <-time.After(15 * time.Second):
			logger.Warn("jobs não terminaram em 15s; encerrando mesmo assim")
		}
	}
}
