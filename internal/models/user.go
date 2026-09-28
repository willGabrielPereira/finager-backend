package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a system user.
// PasswordHash is intentionally excluded from JSON output via json:"-".
type User struct {
	ID                  uuid.UUID  `json:"id,omitempty"`
	Login               string     `json:"login"`
	Email               string     `json:"email"`
	PasswordHash        string     `json:"-"` // never exposed
	FamilyID            uuid.UUID  `json:"family_id"`
	OnboardingCompleted bool       `json:"onboarding_completed"`
	OnboardingStep      int        `json:"onboarding_step"`
	Role                string     `json:"role"` // "user", "moderator" ou "admin"
	LastLoginAt         *time.Time `json:"last_login_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
