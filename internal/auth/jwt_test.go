package auth_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

func TestAuthService_TokenGeneration(t *testing.T) {
	secret := "test-super-secret"
	svc := auth.NewService(secret, 1) // 1 hora de expiração

	user := &models.User{
		ID:       uuid.New(),
		Login:    "auth_tester",
		FamilyID: uuid.New(),
	}

	t.Run("Generate and Validate Access Token", func(t *testing.T) {
		tokenStr, err := svc.GenerateToken(user)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenStr)

		claims, err := svc.ValidateToken(tokenStr)
		require.NoError(t, err)
		assert.Equal(t, user.ID.String(), claims.UserID)
		assert.Equal(t, user.Login, claims.Login)
		assert.Equal(t, user.FamilyID.String(), claims.FamilyID)
		
		// Validar que o token expira no futuro
		assert.True(t, claims.ExpiresAt.Time.After(time.Now()))
	})

	t.Run("Generate and Hash Refresh Token", func(t *testing.T) {
		rawToken, hashedToken, err := svc.GenerateRefreshToken()
		require.NoError(t, err)
		assert.NotEmpty(t, rawToken)
		assert.NotEmpty(t, hashedToken)

		// O hash gerado deve bater com um sha256 real calculado na mão em hex
		expectedHashSlice := sha256.Sum256([]byte(rawToken))
		expectedHashHex := hex.EncodeToString(expectedHashSlice[:])

		assert.Equal(t, expectedHashHex, hashedToken)
		
		// O Validate do service deve gerar o mesmo também
		assert.Equal(t, hashedToken, svc.HashToken(rawToken))
	})

	t.Run("Validate Token With Wrong Secret", func(t *testing.T) {
		tokenStr, _ := svc.GenerateToken(user)

		wrongSvc := auth.NewService("wrong-secret", 1)
		_, err := wrongSvc.ValidateToken(tokenStr)
		require.Error(t, err)
		assert.ErrorContains(t, err, "signature is invalid")
	})
}
