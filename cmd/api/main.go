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

	// ── Repositories ─────────────────────────────────────────────────────────
	repos := repository.New(db.DB)

	// ── Indexes (idempotent — safe to run on every startup) ──────────────────
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	if err := repos.EnsureIndexes(startupCtx); err != nil {
		log.Fatalf("Failed to create database indexes: %v", err)
	}

	// ── Auth ─────────────────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpiresHours)
	authHandler := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, cfg.JWTRefreshExpiresHours)

	// ── Middleware ────────────────────────────────────────────────────────────
	authMiddleware := middleware.Authenticate(authSvc, repos.Blocklist)

	// Rate limiters for auth endpoints (interface-based — swap for Redis anytime).
	loginLimiter := middleware.NewInMemoryRateLimiter(10, time.Minute)   // 10 req/min per IP
	refreshLimiter := middleware.NewInMemoryRateLimiter(20, time.Minute) // 20 req/min per IP

	// ── Handlers ─────────────────────────────────────────────────────────────
	txHandler := handlers.NewTransactionHandler(repos.Transactions, repos.Accounts)
	tagHandler := handlers.NewTagHandler(repos.Tags)
	accHandler := handlers.NewAccountHandler(repos.Accounts)

	// ── Router ───────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("GET /health", handlers.HealthHandler)
	mux.HandleFunc("GET /swagger/", httpSwagger.WrapHandler)

	// Auth routes — rate-limited but public
	mux.Handle("POST /auth/login",
		middleware.RateLimit(loginLimiter)(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /auth/register", http.HandlerFunc(authHandler.Register))
	mux.Handle("POST /auth/refresh",
		middleware.RateLimit(refreshLimiter)(http.HandlerFunc(authHandler.Refresh)))

	// Auth routes — authenticated
	mux.Handle("POST /auth/logout",
		authMiddleware(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("PUT /auth/password",
		authMiddleware(http.HandlerFunc(authHandler.ChangePassword)))

	// Protected routes
	mux.Handle("GET /me",
		authMiddleware(http.HandlerFunc(handlers.MeHandler)))
	mux.Handle("POST /transactions/import",
		authMiddleware(http.HandlerFunc(txHandler.Import)))
	mux.Handle("GET /transactions",
		authMiddleware(http.HandlerFunc(txHandler.List)))

	// ── Tags ─────────────────────────────────────────────────────────────────
	mux.Handle("GET /tags", authMiddleware(http.HandlerFunc(tagHandler.List)))
	mux.Handle("POST /tags", authMiddleware(http.HandlerFunc(tagHandler.Create)))
	mux.Handle("PUT /tags/{id}", authMiddleware(http.HandlerFunc(tagHandler.Update)))
	mux.Handle("DELETE /tags/{id}", authMiddleware(http.HandlerFunc(tagHandler.Delete)))

	// ── Accounts ─────────────────────────────────────────────────────────────
	mux.Handle("GET /accounts", authMiddleware(http.HandlerFunc(accHandler.List)))
	mux.Handle("POST /accounts", authMiddleware(http.HandlerFunc(accHandler.Create)))
	mux.Handle("PUT /accounts/{id}", authMiddleware(http.HandlerFunc(accHandler.Update)))
	mux.Handle("DELETE /accounts/{id}", authMiddleware(http.HandlerFunc(accHandler.Delete)))

	// ── Security headers (applied globally) ──────────────────────────────────
	rootHandler := middleware.SecurityHeaders(mux)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      rootHandler,
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
