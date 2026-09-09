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
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/willGabrielPereira/finager-backend/docs" // gerado pelo swag init

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/config"
	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func main() {
	// ── Configuração ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// ── Banco de dados ────────────────────────────────────────────────────────
	db, err := database.Connect(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("Falha ao conectar no PostgreSQL: %v", err)
	}
	defer db.Close()

	// 3. Inicializa repositórios injetando o Pool
	repos := repository.New(db.Pool)

	// ── Índices (idempotente — seguro executar em todo startup) ───────────────
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	if err := repos.EnsureIndexes(startupCtx); err != nil {
		log.Fatalf("Failed to create database indexes: %v", err)
	}

	// ── Serviço de Auth ───────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpiresHours)

	// ── Roteamento ────────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	registerRoutes(mux, repos, authSvc, cfg.JWTRefreshExpiresHours)

	// ── Middlewares globais (Security e CORS) ─────────────────────────────────
	rootHandler := middleware.SecurityHeaders(mux)
	rootHandler = middleware.CORS(cfg.CORSAllowedOrigins)(rootHandler)

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Finager API listening on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown: %v", err)
	}
	log.Println("Server stopped.")
}
