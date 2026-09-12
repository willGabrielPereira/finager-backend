package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a system user.
// PasswordHash is intentionally excluded from JSON output via json:"-".
type User struct {
	ID           uuid.UUID `json:"id,omitempty"`
	Login        string    `json:"login"`
	PasswordHash string    `json:"-"` // never exposed
	FamilyID     uuid.UUID `json:"family_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
