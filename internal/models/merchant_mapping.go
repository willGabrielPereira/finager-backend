package models

import (
	"time"

	"github.com/google/uuid"
)

// MerchantMapping mapeia nomes normalizados de estabelecimentos para tags fixas por família (Camada 1 de alta precisão).
type MerchantMapping struct {
	ID        uuid.UUID `json:"id,omitempty"`
	FamilyID  uuid.UUID `json:"family_id"`
	Pattern   string    `json:"pattern"` // Ex: "UBER", "PADARIA CENTRAL", "IFOOD"
	TagID     uuid.UUID `json:"tag_id"`
	Tag       *Tag      `json:"tag,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
