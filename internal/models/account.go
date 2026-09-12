package models

import (
	"time"

	"github.com/google/uuid"
)

// Account is a real Bank Account (Institution) attached to a family.
// AllowedUsers dictate the access level:
//   - If len(AllowedUsers) == 0: Public to all members of the family (Shared Account).
//   - If len(AllowedUsers) > 0: Private/Restricted to just those IDs present in the array.
type Account struct {
	ID           uuid.UUID   `json:"id,omitempty"`
	Name         string      `json:"name"` // Ex: "Nubank Conjunto" ou "Caixa-2"
	Institution  string      `json:"institution"`
	FamilyID     uuid.UUID   `json:"family_id"`
	CreatedBy    uuid.UUID   `json:"created_by"`
	AllowedUsers []uuid.UUID `json:"allowed_users"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}
