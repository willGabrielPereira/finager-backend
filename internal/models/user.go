package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// User represents a system user.
// PasswordHash is intentionally excluded from JSON output via json:"-".
type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"  json:"id,omitempty"`
	Login        string        `bson:"login"          json:"login"`
	PasswordHash string        `bson:"password_hash"  json:"-"` // never exposed
	FamilyID     bson.ObjectID `bson:"family_id"      json:"family_id"`
	CreatedAt    time.Time     `bson:"created_at"     json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at"     json:"updated_at"`
}
