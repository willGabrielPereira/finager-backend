package main

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func ensureAccount(ctx context.Context, repo *repository.AccountRepository, name, institution string, familyID, creatorID bson.ObjectID, allowedUsers []bson.ObjectID) *models.Account {
	// Simula idempotência pela instituição + nome dentro da família
	visible, _ := repo.FindVisibleAccounts(ctx, familyID, creatorID)
	for _, acc := range visible {
		if acc.Name == name && acc.Institution == institution {
			return acc
		}
	}

	acc := &models.Account{
		Name:         name,
		Institution:  institution,
		FamilyID:     familyID,
		CreatedBy:    creatorID,
		AllowedUsers: allowedUsers,
	}

	if err := repo.Create(ctx, acc); err != nil {
		log.Fatalf("Failed to create account %q: %v", name, err)
	}

	return acc
}
