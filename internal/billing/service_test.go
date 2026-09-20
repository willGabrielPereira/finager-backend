package billing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/willGabrielPereira/finager-backend/internal/billing"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

func TestPlanLimits(t *testing.T) {
	t.Run("Plano Free retorna limites padrão de 2 contas, 2 membros e 90 dias", func(t *testing.T) {
		limits := models.GetPlanLimits(models.PlanFree, models.StatusActive)
		assert.Equal(t, 2, limits.MaxAccounts)
		assert.Equal(t, 2, limits.MaxMembers)
		assert.Equal(t, 90, limits.MaxHistoryDays)
		assert.False(t, limits.IsPro)
	})

	t.Run("Plano Pro ativo retorna contas ilimitadas, 8 membros e histórico ilimitado", func(t *testing.T) {
		limits := models.GetPlanLimits(models.PlanPro, models.StatusActive)
		assert.Equal(t, -1, limits.MaxAccounts)
		assert.Equal(t, 8, limits.MaxMembers)
		assert.Equal(t, 0, limits.MaxHistoryDays)
		assert.True(t, limits.IsPro)
	})

	t.Run("Plano Lifetime Free retorna permissões do Pro", func(t *testing.T) {
		limits := models.GetPlanLimits(models.PlanLifetimeFree, models.StatusActive)
		assert.Equal(t, -1, limits.MaxAccounts)
		assert.Equal(t, 8, limits.MaxMembers)
		assert.Equal(t, 0, limits.MaxHistoryDays)
		assert.True(t, limits.IsPro)
	})

	t.Run("Plano Pro cancelado deve recair nas limitações do plano Free", func(t *testing.T) {
		limits := models.GetPlanLimits(models.PlanPro, models.StatusCanceled)
		assert.Equal(t, 2, limits.MaxAccounts)
		assert.Equal(t, 2, limits.MaxMembers)
		assert.Equal(t, 90, limits.MaxHistoryDays)
		assert.False(t, limits.IsPro)
	})
}

func TestMockBillingProvider(t *testing.T) {
	provider := billing.NewMockProvider()
	assert.Equal(t, "MOCK", provider.Name())
}
