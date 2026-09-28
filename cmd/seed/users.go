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
	return ensureUserWithEmail(ctx, repo, login, "", password, familyID)
}

// ensureUserWithEmail é como ensureUser, mas também define o e-mail na criação
// (email vazio é permitido, seguindo o mesmo padrão de Create/ProfileResponse).
func ensureUserWithEmail(ctx context.Context, repo *repository.UserRepository, login, email, password string, familyID uuid.UUID) *models.User {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("bcrypt(%s): %v", login, err)
	}
	return ensureUserWithHash(ctx, repo, login, email, string(hash), familyID)
}

// ensureUserWithHash é como ensureUserWithEmail, mas recebe o hash bcrypt já pronto em vez
// de uma senha em texto puro (útil para reproduzir um hash específico vindo do banco).
func ensureUserWithHash(ctx context.Context, repo *repository.UserRepository, login, email, passwordHash string, familyID uuid.UUID) *models.User {
	user, err := repo.FindByLogin(ctx, login)
	if err == nil {
		if err := repo.UpdateFamilyID(ctx, user.ID, familyID); err != nil {
			log.Fatalf("UpdateFamilyID(%s): %v", login, err)
		}
		user.FamilyID = familyID
		return user
	}

	newUser := &models.User{
		Login:        login,
		Email:        email,
		PasswordHash: passwordHash,
		FamilyID:     familyID,
	}
	if err := repo.Create(ctx, newUser); err != nil {
		log.Fatalf("Create user(%s): %v", login, err)
	}
	return newUser
}
