package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Account is a real Bank Account (Institution) attached to a family.
// AllowedUsers dictate the access level:
//   - If len(AllowedUsers) == 0: Public to all members of the family (Shared Account).
//   - If len(AllowedUsers) > 0: Private/Restricted to just those IDs present in the array.
type Account struct {
	ID           bson.ObjectID   `bson:"_id,omitempty"    json:"id,omitempty"`
	Name         string          `bson:"name"             json:"name"` // Ex: "Nubank Conjunto" ou "Caixa-2"
	Institution  string          `bson:"institution"      json:"institution"`
	FamilyID     bson.ObjectID   `bson:"family_id"        json:"family_id"`
	CreatedBy    bson.ObjectID   `bson:"created_by"       json:"created_by"`
	AllowedUsers []bson.ObjectID `bson:"allowed_users"    json:"allowed_users"`
	CreatedAt    time.Time       `bson:"created_at"       json:"created_at"`
	UpdatedAt    time.Time       `bson:"updated_at"       json:"updated_at"`
}
