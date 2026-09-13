package ofxparser

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/aclindsa/ofxgo"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// Parse reads an OFX file from the given reader and returns a slice of
// Transaction models ready to be persisted.
func Parse(r io.Reader, accountID, familyID uuid.UUID, createdBy uuid.UUID) ([]models.Transaction, error) {
	resp, err := ofxgo.ParseResponse(r)
	if err != nil {
		return nil, fmt.Errorf("ofxparser: failed to parse OFX response: %w", err)
	}

	var transactions []models.Transaction

	// Processa extratos de contas correntes / poupança (resp.Bank)
	for _, msg := range resp.Bank {
		stmt, ok := msg.(*ofxgo.StatementResponse)
		if !ok || stmt.BankTranList == nil {
			continue
		}

		for _, rawTx := range stmt.BankTranList.Transactions {
			transactions = append(transactions, convertTransaction(rawTx, accountID, familyID, createdBy))
		}
	}

	// Processa extratos de cartões de crédito (resp.CreditCard)
	for _, msg := range resp.CreditCard {
		stmt, ok := msg.(*ofxgo.CCStatementResponse)
		if !ok || stmt.BankTranList == nil {
			continue
		}

		for _, rawTx := range stmt.BankTranList.Transactions {
			transactions = append(transactions, convertTransaction(rawTx, accountID, familyID, createdBy))
		}
	}

	return transactions, nil
}

func convertTransaction(rawTx ofxgo.Transaction, accountID, familyID, createdBy uuid.UUID) models.Transaction {
	amount, err := strconv.ParseFloat(rawTx.TrnAmt.String(), 64)
	if err != nil {
		amount = 0
	}

	return models.Transaction{
		FITID:      string(rawTx.FiTID),
		Type:       rawTx.TrnType.String(),
		DatePosted: rawTx.DtPosted.Time,
		Amount:     amount,
		Name:       string(rawTx.Name),
		Memo:       string(rawTx.Memo),
		Tags:       []uuid.UUID{},
		AccountID:  accountID,
		FamilyID:   familyID,
		CreatedBy:  createdBy,
		ImportedAt: time.Now().UTC(),
	}
}
