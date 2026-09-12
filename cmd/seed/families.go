package main

import (
	"context"
	"log"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func ensureFamily(ctx context.Context, repo *repository.FamilyRepository, name string) *models.Family {
	family, err := repo.FindByName(ctx, name)
	if err == nil {
		return family
	}

	newFamily := &models.Family{
		Name: name,
	}
	if err := repo.Create(ctx, newFamily); err != nil {
		log.Fatalf("Create family: %v", err)
	}
	return newFamily
}
