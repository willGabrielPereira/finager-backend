package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Transaction represents a single financial transaction extracted from an OFX
// file. Tags allow the user to categorise and report on their spending.
//
// FamilyID defines the access scope: all members of the family can see this
// transaction. CreatedBy records which user imported it (audit trail).
type Transaction struct {
	ID         bson.ObjectID `bson:"_id,omitempty"  json:"id,omitempty"`
	FITID      string        `bson:"fitid"          json:"fitid"`        // Unique ID provided by the bank
	Type       string        `bson:"type"           json:"type"`         // DEBIT, CREDIT, etc.
	DatePosted time.Time     `bson:"date_posted"    json:"date_posted"`
	Amount     float64       `bson:"amount"         json:"amount"`
	Name       string        `bson:"name"           json:"name"`        // Merchant / payee name
	Memo       string        `bson:"memo"           json:"memo"`
	Tags       []string      `bson:"tags"           json:"tags"`        // User-defined tags, e.g. ["food", "subscription"]
	AccountID  bson.ObjectID `bson:"account_id"     json:"account_id"` // Source Account reference
	ImportedAt time.Time     `bson:"imported_at"    json:"imported_at"`
	FamilyID   bson.ObjectID `bson:"family_id"      json:"family_id"`  // Access scope: all family members can see this
	CreatedBy  bson.ObjectID `bson:"created_by"     json:"created_by"` // Audit: which user imported this transaction
}

