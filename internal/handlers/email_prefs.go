package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// EmailPrefsHandler atende o descadastro de lembretes por e-mail (rota pública).
// Exceção declarada ao invariante 3: opera sobre UM usuário identificado por HMAC —
// a assinatura de um usuário nunca altera outro.
type EmailPrefsHandler struct {
	userRepo    *repository.UserRepository
	unsubSecret []byte
}

func NewEmailPrefsHandler(userRepo *repository.UserRepository, unsubSecret []byte) *EmailPrefsHandler {
	return &EmailPrefsHandler{userRepo: userRepo, unsubSecret: unsubSecret}
}

type unsubscribeRequest struct {
	U string `json:"u"` // ID do usuário
	S string `json:"s"` // assinatura HMAC (mailer.UnsubscribeSig)
}

// Unsubscribe handles POST /email/unsubscribe.
//
// @Summary      Descadastrar dos lembretes por e-mail
// @Description  Desativa os lembretes por e-mail do usuário usando o link assinado recebido no e-mail. Idempotente. É POST (não GET) porque muta estado; a página /descadastrar do front faz a chamada.
// @Tags         email
// @Accept       json
// @Param        body  body  unsubscribeRequest  true  "Usuário (u) e assinatura (s) do link"
// @Success      204
// @Failure      400  {object}  map[string]interface{}
// @Failure      429  {object}  map[string]string
// @Failure      500  {object}  map[string]interface{}
// @Router       /email/unsubscribe [post]
func (h *EmailPrefsHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_LINK", "Link de descadastro inválido")
		return
	}
	userID, err := uuid.Parse(req.U)
	if err != nil || !mailer.VerifyUnsubscribe(h.unsubSecret, userID, req.S) {
		response.Error(w, http.StatusBadRequest, "E_INVALID_LINK", "Link de descadastro inválido")
		return
	}
	if err := h.userRepo.SetRemindersOptOut(r.Context(), userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
