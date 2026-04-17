package main

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// tagRuleDef define uma regra de sistema em termos de nomes de tag legíveis.
// Os nomes são resolvidos para ObjectIDs em tempo de seed via nameToID.
type tagRuleDef struct {
	Pattern  string
	TagNames []string
}

// systemTagRuleDefs é a lista mestre de regras globais de auto-tagging.
// Para adicionar novos estabelecimentos, basta acrescentar uma linha aqui.
var systemTagRuleDefs = []tagRuleDef{
	// ── Compras ──────────────────────────────────────────────────────────────
	{Pattern: "amazon", TagNames: []string{"Compras"}},
	{Pattern: "shopee", TagNames: []string{"Compras"}},
	{Pattern: "americanas", TagNames: []string{"Compras"}},
	{Pattern: "mercado livre", TagNames: []string{"Compras"}},
	{Pattern: "magalu", TagNames: []string{"Compras"}},
	{Pattern: "magazine", TagNames: []string{"Compras"}},
	{Pattern: "aliexpress", TagNames: []string{"Compras"}},
	{Pattern: "shein", TagNames: []string{"Compras"}},
	// ── Cinema / Entretenimento ───────────────────────────────────────────────
	{Pattern: "cinemark", TagNames: []string{"Cinema", "Entretenimento"}},
	{Pattern: "cinepolis", TagNames: []string{"Cinema", "Entretenimento"}},
	{Pattern: "ingresso", TagNames: []string{"Cinema", "Entretenimento"}},
	{Pattern: "gnc", TagNames: []string{"Cinema", "Entretenimento"}},
	// ── Streamings ───────────────────────────────────────────────────────────
	{Pattern: "netflix", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "spotify", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "youtube", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "hbo", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "disney", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "globoplay", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "apple tv", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "deezer", TagNames: []string{"Streamings", "Entretenimento"}},
	{Pattern: "twitch", TagNames: []string{"Streamings", "Entretenimento"}},
	// ── Saúde ─────────────────────────────────────────────────────────────────
	{Pattern: "droga raia", TagNames: []string{"Saúde"}},
	{Pattern: "drogasil", TagNames: []string{"Saúde"}},
	{Pattern: "ultrafarma", TagNames: []string{"Saúde"}},
	{Pattern: "drogaria", TagNames: []string{"Saúde"}},
	{Pattern: "farmacia", TagNames: []string{"Saúde"}},
	{Pattern: "unimed", TagNames: []string{"Saúde"}},
	{Pattern: "amil", TagNames: []string{"Saúde"}},
	// ── Alimentação ──────────────────────────────────────────────────────────
	{Pattern: "ifood", TagNames: []string{"Alimentação"}},
	{Pattern: "rappi", TagNames: []string{"Alimentação"}},
	{Pattern: "uber eats", TagNames: []string{"Alimentação"}},
	{Pattern: "mcdonalds", TagNames: []string{"Alimentação"}},
	{Pattern: "mcdonald", TagNames: []string{"Alimentação"}},
	{Pattern: "subway", TagNames: []string{"Alimentação"}},
	{Pattern: "burger", TagNames: []string{"Alimentação"}},
	{Pattern: "habib", TagNames: []string{"Alimentação"}},
	{Pattern: "pao de acucar", TagNames: []string{"Alimentação"}},
	{Pattern: "carrefour", TagNames: []string{"Alimentação"}},
	{Pattern: "supermercado", TagNames: []string{"Alimentação"}},
	// ── Transporte ────────────────────────────────────────────────────────────
	{Pattern: "posto", TagNames: []string{"Transporte"}},
	{Pattern: "shell", TagNames: []string{"Transporte"}},
	{Pattern: "ipiranga", TagNames: []string{"Transporte"}},
	{Pattern: "uber", TagNames: []string{"Transporte"}},
	{Pattern: "99app", TagNames: []string{"Transporte"}},
	{Pattern: "passagem", TagNames: []string{"Transporte"}},
	// ── Moradia ───────────────────────────────────────────────────────────────
	{Pattern: "energia eletrica", TagNames: []string{"Moradia"}},
	{Pattern: "saneamento", TagNames: []string{"Moradia"}},
	{Pattern: "enel", TagNames: []string{"Moradia"}},
	{Pattern: "comgas", TagNames: []string{"Moradia"}},
	{Pattern: "sabesp", TagNames: []string{"Moradia"}},
	// ── Educação ──────────────────────────────────────────────────────────────
	{Pattern: "udemy", TagNames: []string{"Educação"}},
	{Pattern: "coursera", TagNames: []string{"Educação"}},
	{Pattern: "alura", TagNames: []string{"Educação"}},
	{Pattern: "descomplica", TagNames: []string{"Educação"}},
	// ── Salário ───────────────────────────────────────────────────────────────
	{Pattern: "salario", TagNames: []string{"Salário"}},
	{Pattern: "pagamento salario", TagNames: []string{"Salário"}},
}

// ensureSystemTagRules popula as regras globais de auto-tagging.
// Recebe nameToID (gerado por ensureSystemTags) para resolver nomes → ObjectIDs.
// Idempotente: pode ser executado em todo deployment.
func ensureSystemTagRules(ctx context.Context, repo *repository.TagRuleRepository, nameToID map[string]bson.ObjectID) {
	for _, def := range systemTagRuleDefs {
		tagIDs := make([]bson.ObjectID, 0, len(def.TagNames))
		for _, name := range def.TagNames {
			id, ok := nameToID[name]
			if !ok {
				log.Printf("Aviso: tag %q não encontrada no banco — regra %q ignorada", name, def.Pattern)
				continue
			}
			tagIDs = append(tagIDs, id)
		}

		if len(tagIDs) == 0 {
			continue
		}

		rule := &models.TagRule{
			Pattern:  def.Pattern,
			Tags:     tagIDs,
			IsSystem: true,
		}
		if err := repo.UpsertSystemRule(ctx, rule); err != nil {
			log.Printf("Aviso: Falha ao upsert system tag rule %q: %v", def.Pattern, err)
		}
	}
}
