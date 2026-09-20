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
		SELECT id, code, discount_percent, plan_granted, max_uses, times_used, expires_at, created_at
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
	)
	if err != nil {
		return nil, err
	}
	return &coupon, nil
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
		INSERT INTO coupons (code, discount_percent, plan_granted, max_uses, times_used, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
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
	).Scan(&coupon.ID, &coupon.CreatedAt)
	return err
}
