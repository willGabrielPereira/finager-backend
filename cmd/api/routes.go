package main

import (
	"net/http"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// registerRoutes monta todas as rotas da API no mux fornecido.
// Mantém toda a lógica de roteamento isolada do ciclo de vida do servidor.
func registerRoutes(
	mux *http.ServeMux,
	repos *repository.Container,
	authSvc *auth.Service,
	jwtRefreshExpiresHours int,
) {
	authHandler := auth.NewHandler(
		authSvc,
		repos.Users,
		repos.Families,
		repos.RefreshTokens,
		repos.Blocklist,
		jwtRefreshExpiresHours,
	)

	authMid := middleware.Authenticate(authSvc, repos.Blocklist)

	loginLimiter := middleware.NewInMemoryRateLimiter(10, time.Minute)
	refreshLimiter := middleware.NewInMemoryRateLimiter(20, time.Minute)

	txHandler := handlers.NewTransactionHandler(repos.Transactions, repos.Accounts, repos.Tags, repos.ClassifierStates)
	tagHandler := handlers.NewTagHandler(repos.Tags)
	accHandler := handlers.NewAccountHandler(repos.Accounts)
	aiHandler := handlers.NewAIHandler(repos.Transactions, repos.Tags, repos.ClassifierStates)

	// ── Públicas ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /health", handlers.HealthHandler)
	mux.HandleFunc("GET /swagger/", httpSwagger.WrapHandler)

	// ── Auth — públicas com rate limit ────────────────────────────────────────
	mux.Handle("POST /auth/login",
		middleware.RateLimit(loginLimiter)(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /auth/register", http.HandlerFunc(authHandler.Register))
	mux.Handle("POST /auth/refresh",
		middleware.RateLimit(refreshLimiter)(http.HandlerFunc(authHandler.Refresh)))

	// ── Auth — autenticadas ───────────────────────────────────────────────────
	mux.Handle("POST /auth/logout", authMid(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("PUT /auth/password", authMid(http.HandlerFunc(authHandler.ChangePassword)))

	// ── Me ────────────────────────────────────────────────────────────────────
	mux.Handle("GET /me", authMid(http.HandlerFunc(handlers.MeHandler)))

	// ── Transações ────────────────────────────────────────────────────────────
	mux.Handle("POST /transactions/import", authMid(http.HandlerFunc(txHandler.Import)))
	mux.Handle("GET /transactions", authMid(http.HandlerFunc(txHandler.List)))
	mux.Handle("PUT /transactions/{id}", authMid(http.HandlerFunc(txHandler.Update)))
	mux.Handle("POST /transactions/{id}/suggest-tags", authMid(http.HandlerFunc(aiHandler.SuggestTags)))
	mux.Handle("POST /transactions/ai-auto-tag", authMid(http.HandlerFunc(aiHandler.AutoTagBatch)))

	// ── Tags ──────────────────────────────────────────────────────────────────
	mux.Handle("GET /tags", authMid(http.HandlerFunc(tagHandler.List)))
	mux.Handle("POST /tags", authMid(http.HandlerFunc(tagHandler.Create)))
	mux.Handle("PUT /tags/{id}", authMid(http.HandlerFunc(tagHandler.Update)))
	mux.Handle("DELETE /tags/{id}", authMid(http.HandlerFunc(tagHandler.Delete)))



	// ── Contas bancárias ──────────────────────────────────────────────────────
	mux.Handle("GET /accounts", authMid(http.HandlerFunc(accHandler.List)))
	mux.Handle("POST /accounts", authMid(http.HandlerFunc(accHandler.Create)))
	mux.Handle("PUT /accounts/{id}", authMid(http.HandlerFunc(accHandler.Update)))
	mux.Handle("DELETE /accounts/{id}", authMid(http.HandlerFunc(accHandler.Delete)))
}
