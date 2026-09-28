package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// SignupInvite representa um convite de cadastro que concede um plano
// específico ao ser utilizado.
type SignupInvite struct {
	ID             uuid.UUID  `json:"id"`
	Token          string     `json:"token"`
	PlanGranted    string     `json:"plan_granted"`
	CreatedBy      uuid.UUID  `json:"created_by"`
	ExpiresAt      time.Time  `json:"expires_at"`
	UsedAt         *time.Time `json:"used_at,omitempty"`
	UsedByFamilyID *uuid.UUID `json:"used_by_family_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// GenerateSignupInviteToken gera um token criptograficamente seguro de 256
// bits, codificado em hexadecimal, seguindo a mesma abordagem usada em
// repository.GenerateSecureToken para os convites de família.
func GenerateSignupInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
