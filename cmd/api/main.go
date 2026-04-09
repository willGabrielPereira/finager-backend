//	@title			Finager API
//	@version		1.0
//	@description	API REST para gestão financeira: importação de extratos OFX e tagueamento de transações.
//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http https
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Informe o token no formato: Bearer {token}
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/config"
	"github.com/willGabrielPereira/finager-backend/internal/database"
	_ "github.com/willGabrielPereira/finager-backend/docs" // gerado pelo swag init
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func main() {
	// ── Configuration ────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// ── Database ─────────────────────────────────────────────────────────────
	db, err := database.Connect(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer db.Close()

	// ── Repositories \u2500───────────────────────────────────────────────────────
	txRepo := repository.NewTransactionRepository(db.DB)

	// ── Handlers ─────────────────────────────────────────────────────────────
	txHandler := handlers.NewTransactionHandler(txRepo)

	// ── Auth ─────────────────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpiresHours)
	authHandler := auth.NewHandler(authSvc, cfg.APILogin, cfg.APIPassword)
	authMiddleware := middleware.Authenticate(authSvc)

	// ── Router ───────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("GET /health", handlers.HealthHandler)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("GET /swagger/", httpSwagger.WrapHandler)

	// Protected routes — wrap handlers with authMiddleware.
	mux.Handle("GET /me", authMiddleware(http.HandlerFunc(handlers.MeHandler)))
	mux.Handle("POST /transactions/import", authMiddleware(http.HandlerFunc(txHandler.Import)))
	mux.Handle("GET /transactions", authMiddleware(http.HandlerFunc(txHandler.List)))

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start in a goroutine so we can listen for shutdown signals.
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
