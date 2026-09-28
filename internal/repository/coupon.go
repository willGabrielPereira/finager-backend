package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// CouponRepository gerencia a persistência de cupons promocionais.
type CouponRepository struct {
	pool *pgxpool.Pool
}

// NewCouponRepository instancia o repositório de cupons.
func NewCouponRepository(pool *pgxpool.Pool) *CouponRepository {
	return &CouponRepository{pool: pool}
}

// FindByCode busca um cupom pelo código (case-insensitive).
func (r *CouponRepository) FindByCode(ctx context.Context, code string) (*models.Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var coupon models.Coupon
	query := `
		SELECT id, code, discount_percent, plan_granted, max_uses, times_used, expires_at, created_at, active
		FROM coupons
		WHERE lower(code) = lower($1)
		LIMIT 1
	`
	err := r.pool.QueryRow(ctx, query, strings.TrimSpace(code)).Scan(
		&coupon.ID,
		&coupon.Code,
		&coupon.DiscountPercent,
		&coupon.PlanGranted,
		&coupon.MaxUses,
		&coupon.TimesUsed,
		&coupon.ExpiresAt,
		&coupon.CreatedAt,
		&coupon.Active,
	)
	if err != nil {
		return nil, err
	}
	return &coupon, nil
}

// List retorna todos os cupons cadastrados, mais recentes primeiro.
func (r *CouponRepository) List(ctx context.Context) ([]models.Coupon, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, code, discount_percent, plan_granted, max_uses, times_used, expires_at, created_at, active
		FROM coupons
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	coupons := []models.Coupon{}
	for rows.Next() {
		var coupon models.Coupon
		if err := rows.Scan(
			&coupon.ID,
			&coupon.Code,
			&coupon.DiscountPercent,
			&coupon.PlanGranted,
			&coupon.MaxUses,
			&coupon.TimesUsed,
			&coupon.ExpiresAt,
			&coupon.CreatedAt,
			&coupon.Active,
		); err != nil {
			return nil, err
		}
		coupons = append(coupons, coupon)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return coupons, nil
}

// UpdateActive ativa ou desativa um cupom.
func (r *CouponRepository) UpdateActive(ctx context.Context, couponID uuid.UUID, active bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE coupons SET active = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, active, couponID)
	return err
}

// Update atualiza o limite de usos e a expiração de um cupom existente.
func (r *CouponRepository) Update(ctx context.Context, coupon *models.Coupon) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// COALESCE preserva o valor atual quando o campo não foi enviado no PATCH,
	// evitando que atualizar só um dos dois campos zere o outro sem querer.
	query := `UPDATE coupons SET max_uses = COALESCE($1, max_uses), expires_at = COALESCE($2, expires_at) WHERE id = $3`
	_, err := r.pool.Exec(ctx, query, coupon.MaxUses, coupon.ExpiresAt, coupon.ID)
	return err
}

// IncrementUsage contabiliza o uso de um cupom.
func (r *CouponRepository) IncrementUsage(ctx context.Context, couponID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE coupons SET times_used = times_used + 1 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, couponID)
	return err
}

// Create insere um novo cupom.
func (r *CouponRepository) Create(ctx context.Context, coupon *models.Coupon) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO coupons (code, discount_percent, plan_granted, max_uses, times_used, expires_at, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	err := r.pool.QueryRow(
		ctx, query,
		strings.ToUpper(strings.TrimSpace(coupon.Code)),
		coupon.DiscountPercent,
		coupon.PlanGranted,
		coupon.MaxUses,
		coupon.TimesUsed,
		coupon.ExpiresAt,
		true,
	).Scan(&coupon.ID, &coupon.CreatedAt)
	if err != nil {
		return err
	}
	// O cupom é sempre criado ativo (hardcoded acima); refletimos isso na struct
	// retornada para que o chamador (ex: resposta JSON do handler) não reporte
	// active=false incorretamente (zero value do bool).
	coupon.Active = true
	return nil
}
