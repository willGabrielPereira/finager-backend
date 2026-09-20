package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/billing"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// BillingHandler gerencia as requisições de planos, assinaturas e cupons.
type BillingHandler struct {
	service *billing.Service
}

// NewBillingHandler cria um BillingHandler com o serviço injetado.
func NewBillingHandler(service *billing.Service) *BillingHandler {
	return &BillingHandler{service: service}
}

// GetPlan retorna o plano atual da família e seus limites.
// @Summary      Consultar plano da família
// @Tags         billing
// @Router       /billing/plan [get]
func (h *BillingHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	planStatus, err := h.service.GetFamilyPlan(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao obter status do plano")
		return
	}

	response.JSON(w, http.StatusOK, planStatus)
}

type applyCouponRequest struct {
	Code string `json:"code"`
}

// ApplyCoupon aplica um código promocional para a família.
// @Summary      Aplicar cupom promocional
// @Tags         billing
// @Router       /billing/coupons/apply [post]
func (h *BillingHandler) ApplyCoupon(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	var req applyCouponRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Código de cupom obrigatório")
		return
	}

	coupon, err := h.service.ApplyCoupon(r.Context(), familyID, req.Code)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_COUPON_ERROR", err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"message":      "Cupom aplicado com sucesso!",
		"plan_granted": coupon.PlanGranted,
		"discount":     coupon.DiscountPercent,
	})
}

// SimulateUpgrade permite testar a alteração imediata para o plano PRO (modo simulado).
// @Summary      Simular upgrade para PRO (testes)
// @Tags         billing
// @Router       /billing/simulate-upgrade [post]
func (h *BillingHandler) SimulateUpgrade(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	if err := h.service.SimulateUpgrade(r.Context(), familyID, "PRO"); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao simular upgrade")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "Upgrade para o plano Pro realizado com sucesso (ambiente de teste)!",
	})
}

// SimulateDowngrade permite testar a alteração imediata para o plano FREE (modo simulado).
// @Summary      Simular downgrade para FREE (testes)
// @Tags         billing
// @Router       /billing/simulate-downgrade [post]
func (h *BillingHandler) SimulateDowngrade(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	if err := h.service.SimulateDowngrade(r.Context(), familyID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao simular downgrade")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "Plano rebaixado para Free com sucesso (ambiente de teste)!",
	})
}
