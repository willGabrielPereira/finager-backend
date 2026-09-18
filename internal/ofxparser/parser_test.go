package ofxparser_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/ofxparser"
)

func TestParseBankStatement(t *testing.T) {
	ofxContent := `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
  <SIGNONMSGSRSV1>
    <SONRS>
      <STATUS>
        <CODE>0
        <SEVERITY>INFO
      </STATUS>
      <DTSERVER>20260910120000[-03:EST]
      <LANGUAGE>POR
    </SONRS>
  </SIGNONMSGSRSV1>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <TRNUID>1
      <STATUS><CODE>0<SEVERITY>INFO</STATUS>
      <STMTRS>
        <CURDEF>BRL
        <BANKACCTFROM>
          <BANKID>260
          <ACCTID>12345-6
          <ACCTTYPE>CHECKING
        </BANKACCTFROM>
        <BANKTRANLIST>
          <DTSTART>20260901
          <DTEND>20260910
          <STMTTRN>
            <TRNTYPE>DEBIT
            <DTPOSTED>20260905120000[-03:EST]
            <TRNAMT>-45.50
            <FITID>BANK-001
            <NAME>PADARIA CENTRAL
            <MEMO>COMPRA CARTAO DEB
          </STMTTRN>
        </BANKTRANLIST>
        <LEDGERBAL>
          <BALAMT>1500.00
          <DTASOF>20260910
        </LEDGERBAL>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>`

	accID := uuid.New()
	famID := uuid.New()
	userID := uuid.New()

	txs, err := ofxparser.Parse(strings.NewReader(ofxContent), accID, famID, userID)
	if err != nil {
		t.Fatalf("unexpected error parsing bank OFX: %v", err)
	}

	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}

	if txs[0].FITID != "BANK-001" {
		t.Errorf("expected FITID BANK-001, got %s", txs[0].FITID)
	}
	if txs[0].Amount != -45.50 {
		t.Errorf("expected amount -45.50, got %f", txs[0].Amount)
	}
	if txs[0].Name != "PADARIA CENTRAL" {
		t.Errorf("expected name PADARIA CENTRAL, got %s", txs[0].Name)
	}
}

func TestParseCreditCardStatement(t *testing.T) {
	ofxContent := `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
  <SIGNONMSGSRSV1>
    <SONRS>
      <STATUS>
        <CODE>0
        <SEVERITY>INFO
      </STATUS>
      <DTSERVER>20260910120000[-03:EST]
      <LANGUAGE>POR
    </SONRS>
  </SIGNONMSGSRSV1>
  <CREDITCARDMSGSRSV1>
    <CCSTMTTRNRS>
      <TRNUID>2
      <STATUS><CODE>0<SEVERITY>INFO</STATUS>
      <CCSTMTRS>
        <CURDEF>BRL
        <CCACCTFROM>
          <ACCTID>5502********1234
        </CCACCTFROM>
        <BANKTRANLIST>
          <DTSTART>20260815
          <DTEND>20260915
          <STMTTRN>
            <TRNTYPE>DEBIT
            <DTPOSTED>20260908183000[-03:EST]
            <TRNAMT>-32.90
            <FITID>NUBANK-CC-9981
            <NAME>UBER *TRIP
            <MEMO>SAO PAULO BR
          </STMTTRN>
        </BANKTRANLIST>
        <LEDGERBAL>
          <BALAMT>-32.90
          <DTASOF>20260915
        </LEDGERBAL>
      </CCSTMTRS>
    </CCSTMTTRNRS>
  </CREDITCARDMSGSRSV1>
</OFX>`

	accID := uuid.New()
	famID := uuid.New()
	userID := uuid.New()

	txs, err := ofxparser.Parse(strings.NewReader(ofxContent), accID, famID, userID)
	if err != nil {
		t.Fatalf("unexpected error parsing credit card OFX: %v", err)
	}

	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction from credit card OFX, got %d", len(txs))
	}

	if txs[0].FITID != "NUBANK-CC-9981" {
		t.Errorf("expected FITID NUBANK-CC-9981, got %s", txs[0].FITID)
	}
	if txs[0].Amount != -32.90 {
		t.Errorf("expected amount -32.90, got %f", txs[0].Amount)
	}
	if txs[0].Name != "UBER *TRIP" {
		t.Errorf("expected name UBER *TRIP, got %s", txs[0].Name)
	}
}

func TestParseNubankBankStatementWithoutName(t *testing.T) {
	// OFX de conta corrente do Nubank não inclui a tag <NAME>, apenas <MEMO>
	ofxContent := `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:UTF-8
CHARSET:NONE
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
  <SIGNONMSGSRSV1>
    <SONRS>
      <STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS>
      <DTSERVER>20260914023456[0:GMT]</DTSERVER>
      <LANGUAGE>POR</LANGUAGE>
      <FI><ORG>NU PAGAMENTOS S.A.</ORG><FID>260</FID></FI>
    </SONRS>
  </SIGNONMSGSRSV1>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <TRNUID>1</TRNUID>
      <STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS>
      <STMTRS>
        <CURDEF>BRL</CURDEF>
        <BANKACCTFROM>
          <BANKID>0260</BANKID>
          <BRANCHID>1</BRANCHID>
          <ACCTID>61405066-1</ACCTID>
          <ACCTTYPE>CHECKING</ACCTTYPE>
        </BANKACCTFROM>
        <BANKTRANLIST>
          <DTSTART>20260801000000[-3:BRT]</DTSTART>
          <DTEND>20260831000000[-3:BRT]</DTEND>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20260808000000[-3:BRT]</DTPOSTED>
            <TRNAMT>-105.00</TRNAMT>
            <FITID>6a775fb8-1cf5-4f74-bc40-0cf1437a87fe</FITID>
            <MEMO>Compra no débito - AlessandraWippel</MEMO>
          </STMTTRN>
          <STMTTRN>
            <TRNTYPE>CREDIT</TRNTYPE>
            <DTPOSTED>20260810000000[-3:BRT]</DTPOSTED>
            <TRNAMT>4000.00</TRNAMT>
            <FITID>6a79e856-3667-43fd-8b3d-370bd6abf085</FITID>
            <MEMO>Transferência recebida pelo Pix - WILLIAM GABRIEL PEREIRA - •••.225.699-•• - BANCO INTER (0077) Agência: 1 Conta: 7612386-3</MEMO>
          </STMTTRN>
        </BANKTRANLIST>
        <LEDGERBAL>
          <BALAMT>3895.00</BALAMT>
          <DTASOF>20260831000000[-3:BRT]</DTASOF>
        </LEDGERBAL>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>`

	accID := uuid.New()
	famID := uuid.New()
	userID := uuid.New()

	txs, err := ofxparser.Parse(strings.NewReader(ofxContent), accID, famID, userID)
	if err != nil {
		t.Fatalf("unexpected error parsing Nubank OFX: %v", err)
	}

	if len(txs) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txs))
	}

	// Verifica se a primeira transação usou o memo como name
	if txs[0].Name != "Compra no débito - AlessandraWippel" {
		t.Errorf("expected name 'Compra no débito - AlessandraWippel', got %q", txs[0].Name)
	}
	if txs[0].Memo != "Compra no débito - AlessandraWippel" {
		t.Errorf("expected memo 'Compra no débito - AlessandraWippel', got %q", txs[0].Memo)
	}

	// Verifica se a segunda transação usou o memo como name
	expectedPix := "Transferência recebida pelo Pix - WILLIAM GABRIEL PEREIRA - •••.225.699-•• - BANCO INTER (0077) Agência: 1 Conta: 7612386-3"
	if txs[1].Name != expectedPix {
		t.Errorf("expected name %q, got %q", expectedPix, txs[1].Name)
	}
}

