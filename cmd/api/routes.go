package main

import (
	"net/http"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/billing"
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
	// elevateLimiter é por usuário (não por IP como o loginLimiter), então precisa
	// da sua própria instância — reaproveitar o loginLimiter misturaria as duas chaves.
	elevateLimiter := middleware.NewInMemoryRateLimiter(10, 15*time.Minute)

	authHandler := auth.NewHandler(
		authSvc,
		repos.Users,
		repos.Families,
		repos.RefreshTokens,
		repos.Blocklist,
		repos.Invites,
		repos.SignupInvites,
		jwtRefreshExpiresHours,
		elevateLimiter,
	)

	authMid := middleware.Authenticate(authSvc, repos.Blocklist)

	loginLimiter := middleware.NewInMemoryRateLimiter(10, time.Minute)
	refreshLimiter := middleware.NewInMemoryRateLimiter(20, time.Minute)

	mockBillingProvider := billing.NewMockProvider()
	billingSvc := billing.NewService(mockBillingProvider, repos.Families, repos.Coupons)
	billingHandler := handlers.NewBillingHandler(billingSvc)

	txHandler := handlers.NewTransactionHandler(repos.Transactions, repos.Accounts, repos.Tags, repos.ClassifierStates, repos.MerchantMappings, repos.Families)
	tagHandler := handlers.NewTagHandler(repos.Tags)
	accHandler := handlers.NewAccountHandler(repos.Accounts, billingSvc)
	aiHandler := handlers.NewAIHandler(repos.Transactions, repos.Tags, repos.ClassifierStates, repos.MerchantMappings)
	profileHandler := handlers.NewProfileHandler(repos.Users, repos.Families)
	familyHandler := handlers.NewFamilyHandler(repos.Families, repos.Invites, repos.Users, billingSvc)
	adminHandler := handlers.NewAdminHandler(repos.Users, repos.Coupons, repos.SignupInvites)

	// Moderador e admin podem ver estatísticas, listar admins/moderadores e gerenciar convites.
	adminOrModMid := middleware.RequireRole(repos.Users, "admin", "moderator")
	// Só admin pode promover/rebaixar usuários e gerenciar cupons.
	adminOnlyMid := middleware.RequireRole(repos.Users, "admin")
	// Exige reautenticação recente (step-up) além do papel, em todas as rotas /admin/*.
	elevatedMid := middleware.RequireElevated(authSvc, repos.Blocklist)

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
	mux.Handle("POST /auth/elevate", authMid(http.HandlerFunc(authHandler.Elevate)))

	// ── Perfil do Usuário & LGPD ──────────────────────────────────────────────
	mux.Handle("GET /me", authMid(http.HandlerFunc(profileHandler.Get)))
	mux.Handle("PUT /me", authMid(http.HandlerFunc(profileHandler.Update)))
	mux.Handle("PATCH /me/onboarding", authMid(http.HandlerFunc(profileHandler.UpdateOnboarding)))
	mux.Handle("DELETE /me", authMid(http.HandlerFunc(profileHandler.Delete)))

	// ── Planos & Faturamento (Billing) ────────────────────────────────────────
	mux.Handle("GET /billing/plan", authMid(http.HandlerFunc(billingHandler.GetPlan)))
	mux.Handle("POST /billing/coupons/apply", authMid(http.HandlerFunc(billingHandler.ApplyCoupon)))
	mux.Handle("POST /billing/simulate-upgrade", authMid(http.HandlerFunc(billingHandler.SimulateUpgrade)))
	mux.Handle("POST /billing/simulate-downgrade", authMid(http.HandlerFunc(billingHandler.SimulateDowngrade)))

	// ── Família & Convites ────────────────────────────────────────────────────
	mux.HandleFunc("GET /family/invites/validate", familyHandler.ValidateInvite)
	mux.Handle("GET /family/members", authMid(http.HandlerFunc(familyHandler.GetMembers)))
	mux.Handle("POST /family/invites", authMid(http.HandlerFunc(familyHandler.CreateInvite)))
	mux.Handle("POST /family/join", authMid(http.HandlerFunc(familyHandler.Join)))
	mux.Handle("DELETE /family/members/{id}", authMid(http.HandlerFunc(familyHandler.RemoveMember)))

	// ── Transações ────────────────────────────────────────────────────────────
	mux.Handle("POST /transactions", authMid(http.HandlerFunc(txHandler.Create)))
	mux.Handle("POST /transactions/import", authMid(http.HandlerFunc(txHandler.Import)))
	mux.Handle("GET /transactions", authMid(http.HandlerFunc(txHandler.List)))
	mux.Handle("PUT /transactions/{id}", authMid(http.HandlerFunc(txHandler.Update)))
	mux.Handle("DELETE /transactions/{id}", authMid(http.HandlerFunc(txHandler.Delete)))
	mux.Handle("POST /transactions/{id}/apply-similar", authMid(http.HandlerFunc(txHandler.ApplySimilar)))
	mux.Handle("POST /transactions/{id}/suggest-tags", authMid(http.HandlerFunc(aiHandler.SuggestTags)))
	mux.Handle("POST /transactions/ai-auto-tag", authMid(http.HandlerFunc(aiHandler.AutoTagBatch)))

	// ── Tags ──────────────────────────────────────────────────────────────────
	mux.Handle("GET /tags", authMid(http.HandlerFunc(tagHandler.List)))
	mux.Handle("GET /tags/frequent", authMid(http.HandlerFunc(tagHandler.ListFrequent)))
	mux.Handle("POST /tags", authMid(http.HandlerFunc(tagHandler.Create)))
	mux.Handle("PUT /tags/{id}", authMid(http.HandlerFunc(tagHandler.Update)))
	mux.Handle("DELETE /tags/{id}", authMid(http.HandlerFunc(tagHandler.Delete)))

	// ── Regras de Estabelecimento (Layer 1) ──────────────────────────────────
	mux.Handle("GET /merchant-rules", authMid(http.HandlerFunc(aiHandler.ListRules)))
	mux.Handle("POST /merchant-rules", authMid(http.HandlerFunc(aiHandler.CreateRule)))
	mux.Handle("DELETE /merchant-rules/{id}", authMid(http.HandlerFunc(aiHandler.DeleteRule)))
	// ── Contas bancárias ──────────────────────────────────────────────────────
	mux.Handle("GET /accounts", authMid(http.HandlerFunc(accHandler.List)))
	mux.Handle("POST /accounts", authMid(http.HandlerFunc(accHandler.Create)))
	mux.Handle("PUT /accounts/{id}", authMid(http.HandlerFunc(accHandler.Update)))
	mux.Handle("DELETE /accounts/{id}", authMid(http.HandlerFunc(accHandler.Delete)))

	// ── Administração ─────────────────────────────────────────────────────────
	mux.Handle("GET /admin/stats/overview", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.Overview)))))
	mux.Handle("GET /admin/stats/activity", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.Activity)))))
	mux.Handle("GET /admin/users", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.ListAdmins)))))
	mux.Handle("PATCH /admin/users/{id}/role", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.UpdateUserRole)))))
	mux.Handle("POST /admin/invites", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.CreateInvite)))))
	mux.Handle("GET /admin/invites", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.ListInvites)))))
	mux.Handle("DELETE /admin/invites/{id}", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.RevokeInvite)))))
	mux.Handle("GET /admin/coupons", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.ListCoupons)))))
	mux.Handle("POST /admin/coupons", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.CreateCoupon)))))
	mux.Handle("PATCH /admin/coupons/{id}", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.UpdateCoupon)))))
}
