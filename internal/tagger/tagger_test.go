package tagger_test

import (
	"sort"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/tagger"
)

// IDs de teste fixos — representam as tags do sistema.
var (
	idCompras        = bson.NewObjectID()
	idCinema         = bson.NewObjectID()
	idEntretenimento = bson.NewObjectID()
	idSaude          = bson.NewObjectID()
	idAlimentacao    = bson.NewObjectID()
	idTransporte     = bson.NewObjectID()
	idSalario        = bson.NewObjectID()
	idStreamings     = bson.NewObjectID()
)

// systemRules simula as regras que o seed insere no banco.
var systemRules = []models.TagRule{
	{Pattern: "amazon", Tags: []bson.ObjectID{idCompras}, IsSystem: true},
	{Pattern: "shopee", Tags: []bson.ObjectID{idCompras}, IsSystem: true},
	{Pattern: "cinemark", Tags: []bson.ObjectID{idCinema, idEntretenimento}, IsSystem: true},
	{Pattern: "gnc", Tags: []bson.ObjectID{idCinema, idEntretenimento}, IsSystem: true},
	{Pattern: "droga raia", Tags: []bson.ObjectID{idSaude}, IsSystem: true},
	{Pattern: "farmacia", Tags: []bson.ObjectID{idSaude}, IsSystem: true},
	{Pattern: "ifood", Tags: []bson.ObjectID{idAlimentacao}, IsSystem: true},
	{Pattern: "uber", Tags: []bson.ObjectID{idTransporte}, IsSystem: true},
	{Pattern: "salario", Tags: []bson.ObjectID{idSalario}, IsSystem: true},
	{Pattern: "netflix", Tags: []bson.ObjectID{idStreamings, idEntretenimento}, IsSystem: true},
}

// toHexSet converte []bson.ObjectID em um set de strings para comparação order-agnostic.
func toHexSet(ids []bson.ObjectID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Hex()
	}
	sort.Strings(out)
	return out
}

func TestApply_SystemRules(t *testing.T) {
	tests := []struct {
		name    string
		txName  string
		txMemo  string
		wantIDs []bson.ObjectID
	}{
		{"amazon uppercase", "AMAZON MARKETPLACE", "", []bson.ObjectID{idCompras}},
		{"cinemark", "CINEMARK SHOPPING VIA", "", []bson.ObjectID{idCinema, idEntretenimento}},
		{"droga raia", "DROGA RAIA FILIAL 42", "", []bson.ObjectID{idSaude}},
		{"gnc", "GNC CINEMAS", "", []bson.ObjectID{idCinema, idEntretenimento}},
		{"ifood", "IFOOD*RESTAURANTE", "", []bson.ObjectID{idAlimentacao}},
		{"uber", "UBER *TRIP", "", []bson.ObjectID{idTransporte}},
		{"salario in memo", "", "CREDITO SALARIO REF ABRIL", []bson.ObjectID{idSalario}},
		{"netflix", "NETFLIX.COM", "", []bson.ObjectID{idStreamings, idEntretenimento}},
		{"no match", "TRANSFERENCIA TED", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagger.Apply(systemRules, tt.txName, tt.txMemo)

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("Apply(%q, %q) returned %d tags, want %d\ngot : %v\nwant: %v",
					tt.txName, tt.txMemo, len(got), len(tt.wantIDs), toHexSet(got), toHexSet(tt.wantIDs))
			}

			if len(tt.wantIDs) > 0 {
				gs := toHexSet(got)
				ws := toHexSet(tt.wantIDs)
				for i := range gs {
					if gs[i] != ws[i] {
						t.Errorf("tag[%d]: got %q, want %q", i, gs[i], ws[i])
					}
				}
			}
		})
	}
}

func TestApply_FamilyRuleOverridesSystem(t *testing.T) {
	idEntretenimentoFamilia := bson.NewObjectID() // ID diferente, só para distinguir

	// A família redefiniu "amazon" para apontar para seu próprio tag de entretenimento.
	familyRule := models.TagRule{
		Pattern:  "amazon",
		Tags:     []bson.ObjectID{idEntretenimentoFamilia},
		IsSystem: false,
	}
	rules := append(systemRules, familyRule)

	got := tagger.Apply(rules, "AMAZON PRIME VIDEO", "")
	if len(got) != 1 || got[0] != idEntretenimentoFamilia {
		t.Errorf("family override failed: got %v, want [%s]", toHexSet(got), idEntretenimentoFamilia.Hex())
	}
}

func TestApply_EmptyPattern(t *testing.T) {
	rules := []models.TagRule{
		{Pattern: "", Tags: []bson.ObjectID{idCompras}, IsSystem: true},
	}
	got := tagger.Apply(rules, "QUALQUER COISA", "qualquer coisa")
	if len(got) != 0 {
		t.Errorf("empty pattern should not match anything, got %v", toHexSet(got))
	}
}
