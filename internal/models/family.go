package models

import (
	"time"

	"github.com/google/uuid"
)

// Family represents a group of users that share financial data.
// Every user belongs to exactly one family. A solo user has a family of one.
// When members are added in the future, all family transactions become visible
// to the new member automatically (filtered by family_id on Transaction).
type Family struct {
	ID        uuid.UUID   `json:"id,omitempty"`
	Name      string      `json:"name"`
	MemberIDs []uuid.UUID `json:"member_ids"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}
