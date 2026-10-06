package csvparser_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/csvparser"
)

func parse(t *testing.T, s string) []struct {
	fitid, typ, name string
	amount           float64
	date             time.Time
} {
	t.Helper()
	txs, err := csvparser.Parse([]byte(s), uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	var out []struct {
		fitid, typ, name string
		amount           float64
		date             time.Time
	}
	for _, x := range txs {
		out = append(out, struct {
			fitid, typ, name string
			amount           float64
			date             time.Time
		}{x.FITID, x.Type, x.Name, x.Amount, x.DatePosted})
	}
	return out
}

func TestInter(t *testing.T) {
	got := parse(t, " Extrato Conta Corrente \nConta ;76123863\nSaldo ;5.514,57\n\n"+
		"Data Lançamento;Histórico;Descrição;Valor;Saldo\n"+
		"13/09/2026;Pix enviado ;Nic Br;-76,00;5.514,57\n"+
		"03/08/2026;Transferência recebida;Caixa Economica Federal;2.556,93;4.568,92\n")
	if len(got) != 2 || got[0].amount != -76 || got[0].typ != "DEBIT" || got[1].amount != 2556.93 || got[1].typ != "CREDIT" {
		t.Fatalf("inter: %+v", got)
	}
}

func TestNubank(t *testing.T) {
	got := parse(t, "Data,Valor,Identificador,Descrição\n"+
		"08/08/2026,-105.00,6a775fb8-1cf5-4f74-bc40-0cf1437a87fe,Compra no débito - Alessandra\n"+
		"21/08/2026,1.08,6a8805d1,Crédito em conta\n")
	if len(got) != 2 || got[0].fitid != "6a775fb8-1cf5-4f74-bc40-0cf1437a87fe" || got[0].amount != -105 || got[1].amount != 1.08 {
		t.Fatalf("nubank: %+v", got)
	}
}

func TestFlash(t *testing.T) {
	got := parse(t, "Data,Hora,Movimentação,Valor,Meio de Pagamento,Saldo\n"+
		"06/08/2026,19:57,BRASIL ATACADISTA,\"-R$ 630,06\",Cartão,\"R$ 138,24\"\n"+
		"06/08/2026,00:33,Depósito transferido,\"R$ 50,00\",Depósito,\"R$ 768,30\"\n")
	if len(got) != 2 || got[0].amount != -630.06 || got[0].date.Hour() != 19 || got[1].amount != 50 {
		t.Fatalf("flash: %+v", got)
	}
}

func TestBradescoSkipsOpeningBalanceAndFooter(t *testing.T) {
	got := parse(t, "FeffExtrato de: Ag: 367 | Conta: 50883-7;;;;;\n"+
		"Data;Histórico;Docto.;Crédito (R$);Débito (R$);Saldo (R$)\n"+
		"31/12/2025;COD. LANC. 0;0;0,00; ;1.148,06\n"+
		"05/01/2026;PIX RECEBIDO;1918162;1.600,00; ;2.748,06\n"+
		"05/01/2026;SAQUE DINHEIRO ATM;6546390; ;1.520,00;1.928,11\n"+
		"05/01/2026;SAQUE DINHEIRO ATM;6546390; ;1.520,00;1.928,11\n"+
		";;;;;\nÚltimos Lancamentos;;;;;\nData;Histórico;Docto.;Crédito (R$);Débito (R$);\n;;Total;0,00;0,00;0,00;\n")
	if len(got) != 3 || got[0].amount != 1600 || got[1].amount != -1520 {
		t.Fatalf("bradesco: %+v", got)
	}
	if got[1].fitid == got[2].fitid {
		t.Fatal("linhas idênticas devem ter FITIDs distintos")
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := csvparser.Parse([]byte("a,b,c\n1,2,3\n"), uuid.New(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("esperava erro")
	}
}
