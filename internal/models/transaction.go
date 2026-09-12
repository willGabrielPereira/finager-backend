package models

import (
	"time"

	"github.com/google/uuid"
)

// Constantes de status da transação
const (
	TxStatusPosted                = "POSTED"
	TxStatusPlanned               = "PLANNED"
	TxStatusPendingReconciliation = "PENDING_RECONCILIATION"
	TxStatusReconciled            = "RECONCILED"
)

// Constantes de origem da transação
const (
	TxSourceOFX     = "OFX"
	TxSourceManual  = "MANUAL"
	TxSourceReceipt = "RECEIPT"
)

// Transaction represents a single financial transaction extracted from an OFX
// file or created manually. Tags allow the user to categorise and report on their spending.
//
// FamilyID defines the access scope: all members of the family can see this
// transaction. CreatedBy records which user imported or created it (audit trail).
type Transaction struct {
	ID                   uuid.UUID   `json:"id,omitempty"`
	FITID                string      `json:"fitid"`        // Unique ID provided by the bank or generated for manual entries
	Type                 string      `json:"type"`         // DEBIT, CREDIT, etc.
	DatePosted           time.Time   `json:"date_posted"`
	Amount               float64     `json:"amount"`
	Name                 string      `json:"name"`        // Merchant / payee name
	Memo                 string      `json:"memo"`
	Tags                 []uuid.UUID `json:"tags"`        // IDs das tags vinculadas à transação
	AccountID            uuid.UUID   `json:"account_id"` // Source Account reference
	ImportedAt           time.Time   `json:"imported_at"`
	FamilyID             uuid.UUID   `json:"family_id"`  // Access scope: all family members can see this
	CreatedBy            uuid.UUID   `json:"created_by"` // Audit: which user created/imported this transaction
	ManuallyTagged       bool        `json:"manually_tagged"`
	Status               string      `json:"status"` // POSTED, PLANNED, PENDING_RECONCILIATION, RECONCILED
	IsTransfer           bool        `json:"is_transfer"`
	DestinationAccountID *uuid.UUID  `json:"destination_account_id,omitempty"`
	Source               string      `json:"source"` // OFX, MANUAL, RECEIPT
}
