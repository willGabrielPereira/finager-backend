package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// RefreshToken stores a hashed refresh token tied to a user.
// The raw token is NEVER persisted — only its SHA-256 hex digest.
// MongoDB auto-deletes expired documents via TTL index on ExpiresAt.
type RefreshToken struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    bson.ObjectID `bson:"user_id"`
	TokenHash string        `bson:"token_hash"` // SHA-256 hex of the raw token
	ExpiresAt time.Time     `bson:"expires_at"` // TTL index — auto-deleted by MongoDB
	Revoked   bool          `bson:"revoked"`
}
