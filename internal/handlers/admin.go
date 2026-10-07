package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// AdminHandler agrupa os handlers do painel administrativo (estatísticas,
// gestão de moderadores/admins, convites de cadastro e cupons).
type AdminHandler struct {
	userRepo   *repository.UserRepository
	couponRepo *repository.CouponRepository
	inviteRepo *repository.SignupInviteRepository
	familyRepo *repository.FamilyRepository
	auditRepo  *repository.AuditRepository
	dumpRepo   *repository.DumpRepository
	sender     mailer.Sender // nil = e-mail desligado
}

// NewAdminHandler cria um AdminHandler com os repositórios injetados.
func NewAdminHandler(
	users *repository.UserRepository,
	coupons *repository.CouponRepository,
	invites *repository.SignupInviteRepository,
	families *repository.FamilyRepository,
	audit *repository.AuditRepository,
	dumps *repository.DumpRepository,
	sender mailer.Sender,
) *AdminHandler {
	return &AdminHandler{userRepo: users, couponRepo: coupons, inviteRepo: invites, familyRepo: families, auditRepo: audit, dumpRepo: dumps, sender: sender}
}

// allowedRoles é o allow-list de roles válidos. Nenhum valor de role vindo do
// cliente é aceito fora desta lista, mesmo indo parametrizado para o banco.
var allowedRoles = map[string]bool{
	"user":      true,
	"moderator": true,
	"admin":     true,
}

// allowedCouponPlans é o allow-list de planos que um cupom pode conceder.
var allowedCouponPlans = map[string]bool{
	models.PlanPro:          true,
	models.PlanLifetimeFree: true,
}

// Overview retorna a contagem total de usuários e a distribuição por plano.
// @Summary      Visão geral de estatísticas
// @Description  Retorna o total de usuários e a contagem agrupada por plano da família.
// @Tags         Admin
// @Success      200  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/stats/overview [get]
func (h *AdminHandler) Overview(w http.ResponseWriter, r *http.Request) {
	usersByPlan, err := h.userRepo.CountByPlan(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao obter estatísticas de usuários")
		return
	}

	total := 0
	for _, count := range usersByPlan {
		total += count
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_users":   total,
		"users_by_plan": usersByPlan,
	})
}

// Activity retorna a lista paginada de atividade de usuários.
// @Summary      Atividade de usuários (paginada)
// @Tags         Admin
// @Param        page   query     int  false  "Página (padrão 1)"
// @Param        limit  query     int  false  "Itens por página (padrão 20)"
// @Success      200  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/stats/activity [get]
func (h *AdminHandler) Activity(w http.ResponseWriter, r *http.Request) {
	const maxPage = 1_000_000
	const maxLimit = 100

	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= maxPage {
			page = parsed
		}
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= maxLimit {
			limit = parsed
		}
	}

	items, total, err := h.userRepo.ListActivity(r.Context(), page, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao obter atividade de usuários")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// adminUserResponse é o DTO de resposta de usuário sem campos sensíveis.
type adminUserResponse struct {
	ID        uuid.UUID `json:"id"`
	Login     string    `json:"login"`
	Email     string    `json:"email"`
	FamilyID  uuid.UUID `json:"family_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// ListAdmins retorna os usuários com role moderator ou admin.
// @Summary      Listar moderadores e administradores
// @Tags         Admin
// @Success      200  {array}   adminUserResponse
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/users [get]
func (h *AdminHandler) ListAdmins(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListByRole(r.Context(), []string{"moderator", "admin"})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao listar administradores")
		return
	}

	dto := make([]adminUserResponse, 0, len(users))
	for _, u := range users {
		dto = append(dto, adminUserResponse{
			ID:        u.ID,
			Login:     u.Login,
			Email:     u.Email,
			FamilyID:  u.FamilyID,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}

	response.JSON(w, http.StatusOK, dto)
}

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

// UpdateUserRole altera o role de um usuário, impedindo remover o último admin.
// @Summary      Atualizar role de usuário
// @Tags         Admin
// @Param        id    path      string                  true  "ID do usuário"
// @Param        role  body      updateUserRoleRequest  true  "Novo role"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/users/{id}/role [patch]
func (h *AdminHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de usuário inválido")
		return
	}

	var req updateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if !allowedRoles[req.Role] {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ROLE", "Role inválido. Valores aceitos: user, moderator, admin")
		return
	}

	target, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil || target == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Usuário não encontrado")
		return
	}

	if target.Role == "admin" && req.Role != "admin" {
		adminCount, err := h.userRepo.CountAdmins(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao verificar administradores")
			return
		}
		if adminCount <= 1 {
			response.Error(w, http.StatusBadRequest, "E_LAST_ADMIN", "Não é possível remover o último administrador")
			return
		}
	}

	if err := h.userRepo.UpdateRole(r.Context(), userID, req.Role); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar role do usuário")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Role atualizado com sucesso"})
}

type adminCreateInviteRequest struct {
	ExpiresInDays int `json:"expires_in_days"`
}

// CreateInvite gera um convite de cadastro com plano vitalício gratuito.
// @Summary      Criar convite de cadastro
// @Tags         Admin
// @Param        body  body      adminCreateInviteRequest  false  "Dias até expiração (padrão 7, máx 30)"
// @Success      201  {object}  models.SignupInvite
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/invites [post]
func (h *AdminHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	createdBy, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	var req adminCreateInviteRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	expiresInDays := req.ExpiresInDays
	if expiresInDays <= 0 {
		expiresInDays = 7
	}
	if expiresInDays > 30 {
		expiresInDays = 30
	}

	token, err := models.GenerateSignupInviteToken()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao gerar token do convite")
		return
	}

	invite := &models.SignupInvite{
		Token:       token,
		PlanGranted: models.PlanLifetimeFree,
		CreatedBy:   createdBy,
		ExpiresAt:   time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour),
	}

	if err := h.inviteRepo.Create(r.Context(), invite); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao salvar convite")
		return
	}

	response.JSON(w, http.StatusCreated, invite)
}

// ListInvites retorna todos os convites de cadastro.
// @Summary      Listar convites de cadastro
// @Tags         Admin
// @Success      200  {array}   models.SignupInvite
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/invites [get]
func (h *AdminHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := h.inviteRepo.List(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao listar convites")
		return
	}

	response.JSON(w, http.StatusOK, invites)
}

// RevokeInvite revoga (exclui) um convite de cadastro ainda não utilizado.
// @Summary      Revogar convite de cadastro
// @Tags         Admin
// @Param        id  path  string  true  "ID do convite"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/invites/{id} [delete]
func (h *AdminHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de convite inválido")
		return
	}

	if err := h.inviteRepo.Delete(r.Context(), inviteID); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_INVITE", err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Convite revogado com sucesso"})
}

// ListCoupons retorna todos os cupons cadastrados.
// @Summary      Listar cupons
// @Tags         Admin
// @Success      200  {array}   models.Coupon
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/coupons [get]
func (h *AdminHandler) ListCoupons(w http.ResponseWriter, r *http.Request) {
	coupons, err := h.couponRepo.List(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao listar cupons")
		return
	}

	response.JSON(w, http.StatusOK, coupons)
}

type createCouponRequest struct {
	Code            string     `json:"code"`
	DiscountPercent int        `json:"discount_percent"`
	PlanGranted     string     `json:"plan_granted"`
	MaxUses         *int       `json:"max_uses,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

// CreateCoupon cria um novo cupom promocional.
// @Summary      Criar cupom
// @Tags         Admin
// @Param        body  body      createCouponRequest  true  "Dados do cupom"
// @Success      201  {object}  models.Coupon
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/coupons [post]
func (h *AdminHandler) CreateCoupon(w http.ResponseWriter, r *http.Request) {
	var req createCouponRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if req.Code == "" {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Código do cupom é obrigatório")
		return
	}

	if !allowedCouponPlans[req.PlanGranted] {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PLAN", "Plano inválido. Valores aceitos: PRO, LIFETIME_FREE")
		return
	}

	coupon := &models.Coupon{
		Code:            req.Code,
		DiscountPercent: req.DiscountPercent,
		PlanGranted:     req.PlanGranted,
		MaxUses:         req.MaxUses,
		ExpiresAt:       req.ExpiresAt,
	}

	if err := h.couponRepo.Create(r.Context(), coupon); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao criar cupom")
		return
	}

	response.JSON(w, http.StatusCreated, coupon)
}

type updateCouponRequest struct {
	Active    *bool      `json:"active,omitempty"`
	MaxUses   *int       `json:"max_uses,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// UpdateCoupon atualiza o estado ativo e/ou os limites de um cupom existente.
// @Summary      Atualizar cupom
// @Tags         Admin
// @Param        id    path      string                true  "ID do cupom"
// @Param        body  body      updateCouponRequest  true  "Campos a atualizar"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/coupons/{id} [patch]
func (h *AdminHandler) UpdateCoupon(w http.ResponseWriter, r *http.Request) {
	couponID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de cupom inválido")
		return
	}

	var req updateCouponRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if req.Active != nil {
		if err := h.couponRepo.UpdateActive(r.Context(), couponID, *req.Active); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar status do cupom")
			return
		}
	}

	if req.MaxUses != nil || req.ExpiresAt != nil {
		coupon := &models.Coupon{ID: couponID, MaxUses: req.MaxUses, ExpiresAt: req.ExpiresAt}
		if err := h.couponRepo.Update(r.Context(), coupon); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar limites do cupom")
			return
		}
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Cupom atualizado com sucesso"})
}

// FamilyDump devolve um script psql (DDL + COPY) com os dados de uma família.
// Só funciona enquanto a própria família mantiver a concessão de acesso do suporte.
// @Summary      Dump SQL dos dados de uma família (suporte)
// @Description  Exige concessão ativa da família (PUT /family/support-access) e o parâmetro reason. Registra auditoria e avisa os membros por e-mail.
// @Tags         Admin
// @Produce      plain
// @Param        id      path   string  true  "ID da família"
// @Param        reason  query  string  true  "Motivo/ticket (máx. 200 caracteres)"
// @Success      200  {string}  string
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Security     BearerAuth
// @Router       /admin/families/{id}/dump [get]
func (h *AdminHandler) FamilyDump(w http.ResponseWriter, r *http.Request) {
	familyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de família inválido")
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if reason == "" || len([]rune(reason)) > 200 {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "reason é obrigatório (máx. 200 caracteres)")
		return
	}
	adminID, err := uuid.Parse(middleware.GetUserID(r))
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	family, err := h.familyRepo.FindByID(r.Context(), familyID)
	if err != nil {
		// Inexistente e sem concessão respondem igual: não revela quais famílias existem.
		response.Error(w, http.StatusForbidden, "E_SUPPORT_ACCESS_DENIED", "A família não concedeu acesso ao suporte")
		return
	}
	if family.SupportAccessUntil == nil || !family.SupportAccessUntil.After(time.Now()) {
		response.Error(w, http.StatusForbidden, "E_SUPPORT_ACCESS_DENIED", "A família não concedeu acesso ao suporte")
		return
	}

	// Auditoria antes de qualquer byte de dado: sem registro, sem dump.
	if err := h.auditRepo.Insert(r.Context(), adminID, familyID, "family_dump", reason); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao registrar auditoria")
		return
	}
	slog.Info("support.dump", "admin_id", adminID, "family_id", familyID, "reason", reason)
	h.notifySupportDump(familyID, reason, *family.SupportAccessUntil)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="finager-family-%s.sql"`, familyID))
	w.Header().Set("Cache-Control", "no-store")
	if err := h.dumpRepo.WriteFamilyDump(r.Context(), w, familyID); err != nil {
		// Status 200 já foi enviado: o arquivo fica sem o marcador final e a CLI detecta.
		slog.Error("support.dump_failed", "family_id", familyID, "err", err)
		sentry.CaptureException(fmt.Errorf("support dump failed: %w", err))
	}
}

// notifySupportDump avisa cada membro da família que o suporte baixou os dados (melhor esforço).
func (h *AdminHandler) notifySupportDump(familyID uuid.UUID, reason string, until time.Time) {
	if h.sender == nil {
		return
	}
	brt := time.FixedZone("BRT", -3*3600)
	const layout = "02/01/2006 15:04"
	when, untilStr := time.Now().In(brt).Format(layout), until.In(brt).Format(layout)

	mailer.Go("support_export", 15*time.Second, func(ctx context.Context) {
		members, err := h.userRepo.ListFamilyEmails(ctx, familyID)
		if err != nil {
			slog.Error("email.failed", "event", "support_export", "err", err)
			sentry.CaptureException(fmt.Errorf("email failed (support_export): %w", err))
			return
		}
		for _, m := range members {
			subject, body, err := mailer.Render("support_export", map[string]string{
				"Login": m.Login, "When": when, "Until": untilStr, "Reason": reason,
			})
			if err == nil {
				err = h.sender.Send(ctx, mailer.Message{To: []string{m.Email}, Subject: subject, HTML: body})
			}
			if err != nil {
				slog.Error("email.failed", "event", "support_export", "to", mailer.Mask(m.Email), "err", err)
				sentry.CaptureException(fmt.Errorf("email failed (support_export): %w", err))
				continue
			}
			slog.Info("email.sent", "event", "support_export", "to", mailer.Mask(m.Email))
		}
	})
}
