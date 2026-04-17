package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
)

type meResponse struct {
	UserID   string `json:"user_id"`
	Login    string `json:"login"`
	FamilyID string `json:"family_id"`
}

// MeHandler retorna os dados do usuário autenticado extraídos do token JWT.
//
// @Summary      Usuário autenticado
// @Description  Retorna o perfil do usuário autenticado (user_id, login, family_id) a partir das claims do JWT
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  meResponse
// @Failure      401  {object}  map[string]string
// @Router       /me [get]
func MeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	_ = json.NewEncoder(w).Encode(meResponse{
		UserID:   claims.UserID,
		Login:    claims.Login,
		FamilyID: claims.FamilyID,
	})
}
