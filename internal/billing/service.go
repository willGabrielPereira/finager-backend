package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

var (
	ErrAccountLimitReached = errors.New("o plano gratuito permite até 2 contas bancárias; faça upgrade para o plano Pro para contas ilimitadas")
	ErrMemberLimitReached  = errors.New("o plano gratuito permite até 2 membros na família; faça upgrade para o plano Pro para convidar até 8 pessoas")
	ErrMaxMemberReached    = errors.New("o limite máximo de 8 membros por família foi atingido")
	ErrCouponNotFound      = errors.New("cupom inválido ou não encontrado")
	ErrCouponExpired       = errors.New("este cupom já expirou")
	ErrCouponMaxUses       = errors.New("este cupom já atingiu o limite máximo de utilizações")
	ErrCouponInactive      = errors.New("este cupom está desativado")
)

// FamilyPlanStatus agrega o plano, as limitações e o consumo atual de recursos.
type FamilyPlanStatus struct {
	FamilyID              uuid.UUID         `json:"family_id"`
	Plan                  string            `json:"plan"`
	SubscriptionStatus    string            `json:"subscription_status"`
	SubscriptionExpiresAt *time.Time        `json:"subscription_expires_at,omitempty"`
	Limits                models.PlanLimits `json:"limits"`
	CurrentUsage          FamilyUsage       `json:"current_usage"`
}

// FamilyUsage informa os recursos atualmente em uso pela família.
type FamilyUsage struct {
	AccountsCount int `json:"accounts_count"`
	MembersCount  int `json:"members_count"`
}

// Service encapsula a lógica de planos, cotas e billing.
type Service struct {
	provider   Provider
	familyRepo *repository.FamilyRepository
	couponRepo *repository.CouponRepository
}

// NewService cria uma instância do serviço de billing.
func NewService(provider Provider, familyRepo *repository.FamilyRepository, couponRepo *repository.CouponRepository) *Service {
	return &Service{
		provider:   provider,
		familyRepo: familyRepo,
		couponRepo: couponRepo,
	}
}

// GetFamilyPlan retorna as informações de plano e consumo da família.
func (s *Service) GetFamilyPlan(ctx context.Context, familyID uuid.UUID) (*FamilyPlanStatus, error) {
	family, err := s.familyRepo.FindByID(ctx, familyID)
	if err != nil {
		return nil, err
	}

	accCount, err := s.familyRepo.CountAccounts(ctx, familyID)
	if err != nil {
		accCount = 0
	}

	memberCount, err := s.familyRepo.CountMembers(ctx, familyID)
	if err != nil {
		memberCount = len(family.MemberIDs)
	}

	limits := models.GetPlanLimits(family.Plan, family.SubscriptionStatus)

	return &FamilyPlanStatus{
		FamilyID:              family.ID,
		Plan:                  family.Plan,
		SubscriptionStatus:    family.SubscriptionStatus,
		SubscriptionExpiresAt: family.SubscriptionExpiresAt,
		Limits:                limits,
		CurrentUsage: FamilyUsage{
			AccountsCount: accCount,
			MembersCount:  memberCount,
		},
	}, nil
}

// CheckCanAddAccount verifica se a família tem cota para cadastrar mais uma conta bancária.
func (s *Service) CheckCanAddAccount(ctx context.Context, familyID uuid.UUID) error {
	family, err := s.familyRepo.FindByID(ctx, familyID)
	if err != nil {
		return err
	}

	limits := models.GetPlanLimits(family.Plan, family.SubscriptionStatus)
	if limits.MaxAccounts < 0 {
		return nil // Ilimitado
	}

	currentCount, err := s.familyRepo.CountAccounts(ctx, familyID)
	if err != nil {
		return err
	}

	if currentCount >= limits.MaxAccounts {
		return ErrAccountLimitReached
	}

	return nil
}

// CheckCanAddMember verifica se a família pode convidar ou adicionar mais um membro.
func (s *Service) CheckCanAddMember(ctx context.Context, familyID uuid.UUID) error {
	family, err := s.familyRepo.FindByID(ctx, familyID)
	if err != nil {
		return err
	}

	limits := models.GetPlanLimits(family.Plan, family.SubscriptionStatus)
	currentCount, err := s.familyRepo.CountMembers(ctx, familyID)
	if err != nil {
		currentCount = len(family.MemberIDs)
	}

	if currentCount >= models.ProMaxMembers {
		return ErrMaxMemberReached
	}

	if currentCount >= limits.MaxMembers {
		return ErrMemberLimitReached
	}

	return nil
}

// ApplyCoupon valida e aplica um cupom promocional à família.
func (s *Service) ApplyCoupon(ctx context.Context, familyID uuid.UUID, code string) (*models.Coupon, error) {
	coupon, err := s.couponRepo.FindByCode(ctx, code)
	if err != nil {
		return nil, ErrCouponNotFound
	}

	if !coupon.Active {
		return nil, ErrCouponInactive
	}

	if coupon.ExpiresAt != nil && coupon.ExpiresAt.Before(time.Now()) {
		return nil, ErrCouponExpired
	}

	if coupon.MaxUses != nil && coupon.TimesUsed >= *coupon.MaxUses {
		return nil, ErrCouponMaxUses
	}

	planToSet := coupon.PlanGranted
	if planToSet == "" {
		planToSet = models.PlanPro
	}

	providerName := "COUPON_" + coupon.Code
	var expiresAt *time.Time
	if planToSet == models.PlanLifetimeFree {
		expiresAt = nil // Perpétuo
	} else {
		t := time.Now().AddDate(0, 1, 0) // 1 mês de vigência default
		expiresAt = &t
	}

	if err := s.familyRepo.UpdatePlan(ctx, familyID, planToSet, models.StatusActive, expiresAt, &providerName); err != nil {
		return nil, fmt.Errorf("falha ao atualizar plano da família: %w", err)
	}

	_ = s.couponRepo.IncrementUsage(ctx, coupon.ID)

	return coupon, nil
}

// SimulateUpgrade atualiza a família diretamente para o plano informado (usado em testes com MockProvider).
func (s *Service) SimulateUpgrade(ctx context.Context, familyID uuid.UUID, plan string) error {
	if plan == "" {
		plan = models.PlanPro
	}
	expires := time.Now().AddDate(0, 1, 0)
	provider := "MOCK"
	return s.familyRepo.UpdatePlan(ctx, familyID, plan, models.StatusActive, &expires, &provider)
}

// SimulateDowngrade rebaixa a família de volta ao plano FREE.
func (s *Service) SimulateDowngrade(ctx context.Context, familyID uuid.UUID) error {
	provider := "MOCK"
	return s.familyRepo.UpdatePlan(ctx, familyID, models.PlanFree, models.StatusActive, nil, &provider)
}
