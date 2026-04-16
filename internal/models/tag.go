package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Tag representa categorias usadas pelos usuários para gerir os extratos.
// Se IsSystem for true, então FamilyID provavelmente será nil (Tags Globais).
type Tag struct {
	ID        bson.ObjectID  `bson:"_id,omitempty"  json:"id,omitempty"`
	Name      string         `bson:"name"           json:"name"`
	Color     string         `bson:"color"          json:"color"`     // Hex color like #FF55AA
	Icon      string         `bson:"icon"           json:"icon"`      // Mdi/Phosphor icon string
	FamilyID  *bson.ObjectID `bson:"family_id"      json:"family_id,omitempty"`
	IsSystem  bool           `bson:"is_system"      json:"is_system"`
	CreatedAt time.Time      `bson:"created_at"     json:"created_at"`
}
