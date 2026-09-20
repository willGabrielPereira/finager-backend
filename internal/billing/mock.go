package billing

import (
	"context"
	"fmt"
	"time"
)

// MockProvider implementa Provider para desenvolvimento local e testes sem gateway real.
type MockProvider struct{}

// NewMockProvider instancia um MockProvider.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) Name() string {
	return "MOCK"
}

func (m *MockProvider) CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutResponse, error) {
	sessionID := fmt.Sprintf("mock_sess_%d", time.Now().UnixNano())
	checkoutURL := fmt.Sprintf("/billing/mock-checkout?session_id=%s&family_id=%s", sessionID, req.FamilyID)
	return &CheckoutResponse{
		CheckoutURL: checkoutURL,
		SessionID:   sessionID,
	}, nil
}

func (m *MockProvider) CancelSubscription(ctx context.Context, externalID string) error {
	return nil
}

func (m *MockProvider) GetSubscription(ctx context.Context, externalID string) (*SubscriptionDetails, error) {
	expires := time.Now().AddDate(0, 1, 0)
	return &SubscriptionDetails{
		Status:           "ACTIVE",
		Plan:             "PRO",
		CurrentPeriodEnd: &expires,
	}, nil
}
