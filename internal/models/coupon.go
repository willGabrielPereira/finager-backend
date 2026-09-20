package models

import (
	"time"

	"github.com/google/uuid"
)

// Coupon representa um cupom de desconto ou benefício promocional.
type Coupon struct {
	ID              uuid.UUID  `json:"id"`
	Code            string     `json:"code"`
	DiscountPercent int        `json:"discount_percent"`
	PlanGranted     string     `json:"plan_granted"` // PRO ou LIFETIME_FREE
	MaxUses         *int       `json:"max_uses,omitempty"`
	TimesUsed       int        `json:"times_used"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
