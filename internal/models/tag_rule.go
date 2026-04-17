package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TagRule mapeia um padrão de texto (name ou memo de uma transação) para um
// conjunto de tags que devem ser aplicadas automaticamente na importação.
//
// Regras de sistema (IsSystem = true) valem para todas as famílias.
// Regras de família (FamilyID preenchido, IsSystem = false) têm precedência
// sobre as de sistema quando há conflito de padrão.
type TagRule struct {
	ID        bson.ObjectID  `bson:"_id,omitempty"  json:"id,omitempty"`
	Pattern   string         `bson:"pattern"        json:"pattern"`   // substring case-insensitive, ex.: "amazon"
	Tags      []bson.ObjectID `bson:"tags"          json:"tags"`      // ObjectIDs das tags a aplicar
	IsSystem  bool           `bson:"is_system"      json:"is_system"` // true = regra global do sistema
	FamilyID  *bson.ObjectID `bson:"family_id"      json:"family_id,omitempty"`
	CreatedBy *bson.ObjectID `bson:"created_by"     json:"created_by,omitempty"`
	CreatedAt time.Time      `bson:"created_at"     json:"created_at"`
	UpdatedAt time.Time      `bson:"updated_at"     json:"updated_at"`
}
