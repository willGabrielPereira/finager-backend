package ofxparser

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/aclindsa/ofxgo"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// Parse reads an OFX file from the given reader and returns a slice of
// Transaction models ready to be persisted.
func Parse(r io.Reader) ([]models.Transaction, error) {
	resp, err := ofxgo.ParseResponse(r)
	if err != nil {
		return nil, fmt.Errorf("ofxparser: failed to parse OFX response: %w", err)
	}

	var transactions []models.Transaction

	for _, msg := range resp.Bank {
		stmt, ok := msg.(*ofxgo.StatementResponse)
		if !ok {
			continue
		}

		for _, rawTx := range stmt.BankTranList.Transactions {
			amount, err := strconv.ParseFloat(rawTx.TrnAmt.String(), 64)
			if err != nil {
				amount = 0
			}

			tx := models.Transaction{
				FITID:      string(rawTx.FiTID),
				Type:       rawTx.TrnType.String(),
				DatePosted: rawTx.DtPosted.Time,
				Amount:     amount,
				Name:       string(rawTx.Name),
				Memo:       string(rawTx.Memo),
				Tags:       []string{},
				ImportedAt: time.Now().UTC(),
			}

			transactions = append(transactions, tx)
		}
	}

	return transactions, nil
}
