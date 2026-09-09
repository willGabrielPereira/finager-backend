package main

import (
	"context"
	"log"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// ensureUser encontra um usuário pelo login ou o cria com a senha fornecida.
// Se o usuário já existe, garante que ele está vinculado à família correta.
func ensureUser(ctx context.Context, repo *repository.UserRepository, login, password string, familyID uuid.UUID) *models.User {
	user, err := repo.FindByLogin(ctx, login)
	if err == nil {
		if err := repo.UpdateFamilyID(ctx, user.ID, familyID); err != nil {
			log.Fatalf("UpdateFamilyID(%s): %v", login, err)
		}
		user.FamilyID = familyID
		return user
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("bcrypt(%s): %v", login, err)
	}

	newUser := &models.User{
		Login:        login,
		PasswordHash: string(hash),
		FamilyID:     familyID,
	}
	if err := repo.Create(ctx, newUser); err != nil {
		log.Fatalf("Create user(%s): %v", login, err)
	}
	return newUser
}
