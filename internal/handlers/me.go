package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
)

type meResponse struct {
	Login string `json:"login"`
}

// MeHandler retorna os dados do usuário autenticado extraídos do token JWT.
//
// @Summary      Usuário autenticado
// @Description  Retorna o login do usuário dono do token JWT
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  meResponse
// @Failure      401  {object}  map[string]string
// @Router       /me [get]
func MeHandler(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meResponse{Login: claims.Login})
}
