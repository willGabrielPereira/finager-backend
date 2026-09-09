package main

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// ensureSystemTags garante que todas as tags globais do sistema estão no banco.
// Retorna um mapa de nome → ObjectID para ser usado na criação das TagRules.
// Idempotente: pode ser executado em todo deployment.
func ensureSystemTags(ctx context.Context, repo *repository.TagRepository) map[string]bson.ObjectID {
	defs := []models.Tag{
		{Name: "Alimentação", Color: "#E53935", Icon: "restaurant", IsSystem: true},
		{Name: "Educação", Color: "#1E88E5", Icon: "school", IsSystem: true},
		{Name: "Transporte", Color: "#FDD835", Icon: "directions_car", IsSystem: true},
		{Name: "Saúde", Color: "#43A047", Icon: "local_hospital", IsSystem: true},
		{Name: "Moradia", Color: "#8E24AA", Icon: "home", IsSystem: true},
		{Name: "Salário", Color: "#00897B", Icon: "attach_money", IsSystem: true},
		{Name: "Streamings", Color: "#FF0000", Icon: "subscriptions", IsSystem: true},
		{Name: "Compras", Color: "#FB8C00", Icon: "shopping_cart", IsSystem: true},
		{Name: "Entretenimento", Color: "#AB47BC", Icon: "sports_esports", IsSystem: true},
		{Name: "Mercado", Color: "#47bc91ff", Icon: "shopping_basket", IsSystem: true},
		{Name: "Pet", Color: "#e88c0cff", Icon: "pets", IsSystem: true},
		{Name: "Vestuário", Color: "#00558bff", Icon: "checkroom", IsSystem: true},
		{Name: "Contas", Color: "#f7445eff", Icon: "receipt", IsSystem: true},
	}

	for _, tag := range defs {
		if err := repo.UpsertSystemTag(ctx, &tag); err != nil {
			log.Printf("Aviso: Falha ao upsert tag %q: %v", tag.Name, err)
		}
	}

	// Busca todas as tags de sistema para resolver nome → ObjectID
	systemTags, err := repo.FindSystemTags(ctx)
	if err != nil {
		log.Fatalf("Falha ao buscar system tags após seed: %v", err)
	}

	nameToID := make(map[string]bson.ObjectID, len(systemTags))
	for _, t := range systemTags {
		nameToID[t.Name] = t.ID
	}
	return nameToID
}
