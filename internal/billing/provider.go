package billing

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CheckoutRequest define os parâmetros para iniciar uma assinatura ou cobrança.
type CheckoutRequest struct {
	FamilyID  uuid.UUID `json:"family_id"`
	UserEmail string    `json:"user_email"`
	UserName  string    `json:"user_name"`
	Plan      string    `json:"plan"` // PRO
	ReturnURL string    `json:"return_url"`
}

// CheckoutResponse representa o retorno para o frontend redirecionar o usuário.
type CheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	SessionID   string `json:"session_id"`
}

// SubscriptionDetails resume as informações da assinatura obtidas do gateway.
type SubscriptionDetails struct {
	Status           string     `json:"status"` // ACTIVE, PAST_DUE, CANCELED
	Plan             string     `json:"plan"`
	CurrentPeriodEnd *time.Time `json:"current_period_end"`
}

// Provider padroniza a integração com qualquer gateway de pagamento (Mock, Asaas, Stripe).
type Provider interface {
	Name() string
	CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutResponse, error)
	CancelSubscription(ctx context.Context, externalID string) error
	GetSubscription(ctx context.Context, externalID string) (*SubscriptionDetails, error)
}
