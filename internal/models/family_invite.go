package models

import (
	"time"

	"github.com/google/uuid"
)

// FamilyInvite represents a secure, high-entropy invitation to join a family.
type FamilyInvite struct {
	ID          uuid.UUID  `json:"id"`
	FamilyID    uuid.UUID  `json:"family_id"`
	Token       string     `json:"token"`
	TargetEmail *string    `json:"target_email,omitempty"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	UsedBy      *uuid.UUID `json:"used_by,omitempty"`
}

// FamilyMemberInfo represents detailed information about a member of a family.
type FamilyMemberInfo struct {
	UserID   uuid.UUID `json:"user_id"`
	Login    string    `json:"login"`
	Email    string    `json:"email"`
	JoinedAt time.Time `json:"joined_at"`
}
