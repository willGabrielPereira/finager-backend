package models

import (
	"time"

	"github.com/google/uuid"
)

// Transaction represents a single financial transaction extracted from an OFX
// file. Tags allow the user to categorise and report on their spending.
//
// FamilyID defines the access scope: all members of the family can see this
// transaction. CreatedBy records which user imported it (audit trail).
type Transaction struct {
	ID         uuid.UUID   `json:"id,omitempty"`
	FITID      string      `json:"fitid"`        // Unique ID provided by the bank
	Type       string      `json:"type"`         // DEBIT, CREDIT, etc.
	DatePosted time.Time   `json:"date_posted"`
	Amount     float64     `json:"amount"`
	Name       string      `json:"name"`        // Merchant / payee name
	Memo       string      `json:"memo"`
	Tags       []uuid.UUID `json:"tags"`        // IDs das tags vinculadas à transação
	AccountID  uuid.UUID   `json:"account_id"` // Source Account reference
	ImportedAt time.Time   `json:"imported_at"`
	FamilyID   uuid.UUID   `json:"family_id"`  // Access scope: all family members can see this
	CreatedBy  uuid.UUID   `json:"created_by"` // Audit: which user imported this transaction
}

