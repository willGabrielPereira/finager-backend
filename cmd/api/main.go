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

	// ── Índices (idempotente — seguro executar em todo startup) ───────────────
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	if err := repos.EnsureIndexes(startupCtx); err != nil {
		logger.Error("falha ao criar índices do banco", "err", err)
		os.Exit(1)
	}

	// ── Serviço de Auth ───────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpiresHours)

	// ── Roteamento ────────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	registerRoutes(mux, repos, authSvc, cfg.JWTRefreshExpiresHours)

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
}
