package models

const (
	PlanFree         = "FREE"
	PlanPro          = "PRO"
	PlanLifetimeFree = "LIFETIME_FREE"

	StatusActive   = "ACTIVE"
	StatusPastDue  = "PAST_DUE"
	StatusCanceled = "CANCELED"

	FreeMaxAccounts = 2
	FreeMaxMembers  = 2
	FreeHistoryDays = 90

	ProMaxMembers = 8
)

// PlanLimits define as quotas e permissões do plano da família.
type PlanLimits struct {
	Plan           string `json:"plan"`
	Status         string `json:"status"`
	MaxAccounts    int    `json:"max_accounts"`     // -1 = ilimitado
	MaxMembers     int    `json:"max_members"`      // 2 no Free, 8 no Pro
	MaxHistoryDays int    `json:"max_history_days"` // 90 no Free, 0 = ilimitado
	IsPro          bool   `json:"is_pro"`
}

// GetPlanLimits retorna as limitações com base no identificador de plano.
func GetPlanLimits(plan string, status string) PlanLimits {
	if status != "" && status != StatusActive {
		// Se o plano Pro foi cancelado ou expirou, recai nas limitações do plano Free
		plan = PlanFree
	}

	switch plan {
	case PlanPro, PlanLifetimeFree:
		return PlanLimits{
			Plan:           plan,
			Status:         status,
			MaxAccounts:    -1, // ilimitado
			MaxMembers:     ProMaxMembers,
			MaxHistoryDays: 0, // ilimitado
			IsPro:          true,
		}
	default:
		return PlanLimits{
			Plan:           PlanFree,
			Status:         StatusActive,
			MaxAccounts:    FreeMaxAccounts,
			MaxMembers:     FreeMaxMembers,
			MaxHistoryDays: FreeHistoryDays,
			IsPro:          false,
		}
	}
}
