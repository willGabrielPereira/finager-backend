package models

import (
	"time"

	"github.com/google/uuid"
)

// Tag representa categorias usadas pelos usuários para gerir os extratos.
// Se IsSystem for true, então FamilyID provavelmente será nil (Tags Globais).
type Tag struct {
	ID        uuid.UUID  `json:"id,omitempty"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`     // Hex color like #FF55AA
	Icon      string     `json:"icon"`      // Mdi/Phosphor icon string
	FamilyID  *uuid.UUID `json:"family_id,omitempty"`
	IsSystem  bool       `json:"is_system"`
	CreatedAt time.Time  `json:"created_at"`
}
