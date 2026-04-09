package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Transaction represents a single financial transaction extracted from an OFX
// file. Tags allow the user to categorise and report on their spending.
type Transaction struct {
	ID          bson.ObjectID      `bson:"_id,omitempty"   json:"id,omitempty"`
	FITID       string             `bson:"fitid"           json:"fitid"`        // Unique ID provided by the bank
	Type        string             `bson:"type"            json:"type"`         // DEBIT, CREDIT, etc.
	DatePosted  time.Time          `bson:"date_posted"     json:"date_posted"`
	Amount      float64            `bson:"amount"          json:"amount"`
	Name        string             `bson:"name"            json:"name"`         // Merchant / payee name
	Memo        string             `bson:"memo"            json:"memo"`
	Tags        []string           `bson:"tags"            json:"tags"`         // User-defined tags, e.g. ["food", "subscription"]
	AccountID   string             `bson:"account_id"      json:"account_id"`  // Source account identifier
	ImportedAt  time.Time          `bson:"imported_at"     json:"imported_at"`
}
