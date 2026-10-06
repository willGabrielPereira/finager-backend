// Package csvparser converte extratos CSV de bancos (Inter, Nubank, Flash, Bradesco)
// em transações, com detecção automática do formato pelo cabeçalho.
package csvparser

import (
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// row é uma linha de extrato já normalizada, independente do banco.
type row struct {
	date   time.Time
	name   string
	memo   string
	amount float64
	fitid  string // vazio = gerado por hash do registro
}

// layout descreve um formato de banco: como reconhecer o cabeçalho e ler cada registro.
type layout struct {
	bank   string
	prefix string // prefixo do cabeçalho normalizado (minúsculo, sem acento)
	comma  rune
	parse  func(rec []string) (row, bool)
}

var layouts = []layout{
	{"inter", "data lancamento;historico;descricao;valor", ';', parseInter},
	{"nubank", "data,valor,identificador", ',', parseNubank},
	{"flash", "data,hora,movimentacao,valor", ',', parseFlash},
	{"bradesco", "data;historico;docto.;credito", ';', parseBradesco},
}

var accentless = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e",
	"í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")

// Parse lê um extrato CSV e devolve as transações prontas para persistência.
// Linhas fora da tabela (preâmbulo, rodapé, saldo inicial) são ignoradas.
func Parse(data []byte, accountID, familyID, createdBy uuid.UUID) ([]models.Transaction, error) {
	text := strings.TrimPrefix(string(data), "Feff")
	text = strings.ReplaceAll(text, " ", " ")

	// Localiza a linha de cabeçalho e o layout correspondente
	var lay *layout
	start := 0
	for pos := 0; pos < len(text); {
		end := strings.IndexByte(text[pos:], '\n')
		if end < 0 {
			end = len(text) - pos
		}
		norm := strings.ToLower(accentless.Replace(strings.TrimSpace(text[pos:pos+end])))
		for i := range layouts {
			if strings.HasPrefix(norm, layouts[i].prefix) {
				lay, start = &layouts[i], pos
				break
			}
		}
		if lay != nil {
			break
		}
		pos += end + 1
	}
	if lay == nil {
		return nil, errors.New("csvparser: formato CSV não reconhecido")
	}

	r := csv.NewReader(strings.NewReader(text[start:]))
	r.Comma = lay.comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	var txs []models.Transaction
	seen := map[string]int{} // linhas idênticas no mesmo arquivo recebem FITIDs distintos
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csvparser: falha ao ler CSV: %w", err)
		}
		rw, ok := lay.parse(rec)
		if !ok || rw.amount == 0 {
			continue
		}

		fitid := rw.fitid
		if fitid == "" {
			sum := sha1.Sum([]byte(lay.bank + "|" + strings.Join(rec, "|")))
			base := "CSV_" + hex.EncodeToString(sum[:])
			seen[base]++
			fitid = fmt.Sprintf("%s_%d", base, seen[base])
		}

		name := strings.TrimSpace(rw.name)
		memo := strings.TrimSpace(rw.memo)
		if name == "" {
			name = memo
		}
		if memo == "" {
			memo = name
		}
		typ := "DEBIT"
		if rw.amount > 0 {
			typ = "CREDIT"
		}

		txs = append(txs, models.Transaction{
			FITID:      fitid,
			Type:       typ,
			DatePosted: rw.date,
			Amount:     rw.amount,
			Name:       name,
			Memo:       memo,
			Tags:       []uuid.UUID{},
			AccountID:  accountID,
			FamilyID:   familyID,
			CreatedBy:  createdBy,
			ImportedAt: time.Now().UTC(),
		})
	}
	return txs, nil
}

func field(rec []string, i int) string {
	if i < len(rec) {
		return strings.TrimSpace(rec[i])
	}
	return ""
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("02/01/2006", s)
	return t, err == nil
}

// parseBRL converte "-R$ 1.234,56", "2.556,93" ou "" (=0) em float.
func parseBRL(s string) (float64, bool) {
	s = strings.NewReplacer("R$", "", " ", "", ".", "", ",", ".").Replace(s)
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// Inter: Data Lançamento;Histórico;Descrição;Valor;Saldo
func parseInter(rec []string) (row, bool) {
	d, ok := parseDate(field(rec, 0))
	v, ok2 := parseBRL(field(rec, 3))
	if !ok || !ok2 {
		return row{}, false
	}
	hist, desc := field(rec, 1), field(rec, 2)
	return row{date: d, name: desc, memo: strings.TrimSpace(hist + " - " + desc), amount: v}, true
}

// Nubank: Data,Valor,Identificador,Descrição (valor com ponto decimal)
func parseNubank(rec []string) (row, bool) {
	d, ok := parseDate(field(rec, 0))
	v, err := strconv.ParseFloat(field(rec, 1), 64)
	if !ok || err != nil {
		return row{}, false
	}
	return row{date: d, name: field(rec, 3), amount: v, fitid: field(rec, 2)}, true
}

// Flash: Data,Hora,Movimentação,Valor,Meio de Pagamento,Saldo
func parseFlash(rec []string) (row, bool) {
	d, ok := parseDate(field(rec, 0))
	v, ok2 := parseBRL(field(rec, 3))
	if !ok || !ok2 {
		return row{}, false
	}
	if h, err := time.Parse("15:04", field(rec, 1)); err == nil {
		d = d.Add(time.Duration(h.Hour())*time.Hour + time.Duration(h.Minute())*time.Minute)
	}
	return row{date: d, name: field(rec, 2), amount: v}, true
}

// Bradesco: Data;Histórico;Docto.;Crédito (R$);Débito (R$);Saldo (R$)
func parseBradesco(rec []string) (row, bool) {
	d, ok := parseDate(field(rec, 0))
	cred, ok2 := parseBRL(field(rec, 3))
	deb, ok3 := parseBRL(field(rec, 4))
	if !ok || !ok2 || !ok3 {
		return row{}, false
	}
	return row{date: d, name: field(rec, 1), amount: cred - deb}, true
}
