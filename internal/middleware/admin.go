package middleware

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// RequireRole é um middleware HTTP que restringe o acesso a usuários cujo
// papel (role) esteja entre os informados. O role é sempre buscado fresco no
// banco de dados — nunca confiamos em um campo de role vindo do JWT, pois o
// token atual não carrega essa claim e, mesmo que carregasse, o role pode
// mudar entre requisições sem que o token seja invalidado.
//
// Deve ser aplicado APÓS o middleware Authenticate, que injeta o user ID no
// contexto da requisição.
func RequireRole(userRepo *repository.UserRepository, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := uuid.Parse(GetUserID(r))
			if err != nil {
				forbidden(w)
				return
			}

			user, err := userRepo.FindByID(r.Context(), userID)
			if err != nil || user == nil {
				forbidden(w)
				return
			}

			allowed := false
			for _, role := range roles {
				if user.Role == role {
					allowed = true
					break
				}
			}
			if !allowed {
				forbidden(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// forbidden escreve a resposta 403 padrão de acesso negado usada pelo RequireRole.
func forbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "acesso negado"})
}

// RequireElevated exige um token de step-up válido (audiência "admin", TTL curto)
// além do access token comum e do papel do usuário — é a camada que impede que
// uma sessão de usuário comum, mesmo com o papel certo, acesse rotas /admin/*
// sem ter reautenticado recentemente com a senha. Deve vir DEPOIS de RequireRole
// na cadeia, pra um usuário sem papel adequado receber 403 de papel, não de elevação.
func RequireElevated(authSvc *auth.Service, blocklist *repository.BlocklistRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			elevationDenied := func() {
				response.Error(w, http.StatusForbidden, "E_ELEVATION_REQUIRED", "reautenticação necessária")
			}

			// O token elevado nunca vai no header Authorization, que já foi
			// consumido pelo Authenticate para o access token comum.
			token := r.Header.Get("X-Admin-Elevation")
			if token == "" {
				elevationDenied()
				return
			}

			elevatedClaims, err := authSvc.ValidateToken(token)
			if err != nil {
				elevationDenied()
				return
			}

			// Garante que o token enviado é realmente um token elevado (aud "admin"),
			// e não o access token comum reaproveitado nesse header.
			if !slices.Contains(elevatedClaims.Audience, "admin") {
				elevationDenied()
				return
			}

			// Garante que o token elevado pertence ao mesmo usuário do access token
			// desta requisição (injetado no contexto pelo Authenticate).
			if elevatedClaims.UserID != GetUserID(r) {
				elevationDenied()
				return
			}

			// Consulta a blocklist para permitir que o logout invalide o token elevado.
			hash := authSvc.HashToken(token)
			blocked, err := blocklist.Exists(r.Context(), hash)
			if err != nil || blocked {
				elevationDenied()
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
