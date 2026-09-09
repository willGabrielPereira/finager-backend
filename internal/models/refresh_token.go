package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken stores a hashed refresh token tied to a user.
// The raw token is NEVER persisted — only its SHA-256 hex digest.
// MongoDB auto-deletes expired documents via TTL index on ExpiresAt.
type RefreshToken struct {
	ID        uuid.UUID `json:"id,omitempty"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"-"` // SHA-256 hex of the raw token
	ExpiresAt time.Time `json:"expires_at"` // TTL index — auto-deleted by MongoDB
	Revoked   bool      `json:"revoked"`
}
