package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Family represents a group of users that share financial data.
// Every user belongs to exactly one family. A solo user has a family of one.
// When members are added in the future, all family transactions become visible
// to the new member automatically (filtered by family_id on Transaction).
type Family struct {
	ID        bson.ObjectID   `bson:"_id,omitempty"  json:"id,omitempty"`
	Name      string          `bson:"name"           json:"name"`
	MemberIDs []bson.ObjectID `bson:"member_ids"     json:"member_ids"`
	CreatedAt time.Time       `bson:"created_at"     json:"created_at"`
	UpdatedAt time.Time       `bson:"updated_at"     json:"updated_at"`
}
