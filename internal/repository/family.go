package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// FamilyRepository encapsulates persistence operations for Family.
type FamilyRepository struct {
	pool *pgxpool.Pool
}

// NewFamilyRepository creates a FamilyRepository pointing at the correct database.
func NewFamilyRepository(pool *pgxpool.Pool) *FamilyRepository {
	return &FamilyRepository{pool: pool}
}

// Create inserts a new family document. Sets timestamps automatically.
func (r *FamilyRepository) Create(ctx context.Context, family *models.Family) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if family.Plan == "" {
		family.Plan = models.PlanFree
	}
	if family.SubscriptionStatus == "" {
		family.SubscriptionStatus = models.StatusActive
	}

	query := `
		INSERT INTO families (name, plan, subscription_status, subscription_expires_at, subscription_provider)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, family.Name, family.Plan, family.SubscriptionStatus, family.SubscriptionExpiresAt, family.SubscriptionProvider).Scan(&family.ID, &family.CreatedAt, &family.UpdatedAt)
	return err
}

// FindByID returns the family with the given UUID.
func (r *FamilyRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Family, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var family models.Family
	query := `SELECT id, name, plan, subscription_status, subscription_expires_at, subscription_provider, created_at, updated_at FROM families WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&family.ID, &family.Name, &family.Plan, &family.SubscriptionStatus, &family.SubscriptionExpiresAt, &family.SubscriptionProvider, &family.CreatedAt, &family.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Fetch member IDs
	membersQuery := `SELECT user_id FROM family_members WHERE family_id = $1`
	rows, err := r.pool.Query(ctx, membersQuery, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberIDs []uuid.UUID
	for rows.Next() {
		var memberID uuid.UUID
		if err := rows.Scan(&memberID); err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, memberID)
	}
	family.MemberIDs = memberIDs

	return &family, nil
}

// FindByName returns the family with the given name.
func (r *FamilyRepository) FindByName(ctx context.Context, name string) (*models.Family, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var family models.Family
	query := `SELECT id, name, plan, subscription_status, subscription_expires_at, subscription_provider, created_at, updated_at FROM families WHERE name = $1 LIMIT 1`
	err := r.pool.QueryRow(ctx, query, name).Scan(
		&family.ID, &family.Name, &family.Plan, &family.SubscriptionStatus, &family.SubscriptionExpiresAt, &family.SubscriptionProvider, &family.CreatedAt, &family.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &family, nil
}

// AddMember appends a user ID to the family's member list (no-op if already present).
func (r *FamilyRepository) AddMember(ctx context.Context, familyID, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO family_members (family_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	_, err := r.pool.Exec(ctx, query, familyID, userID)
	if err != nil {
		return err
	}

	updateQuery := `UPDATE families SET updated_at = now() WHERE id = $1`
	_, err = r.pool.Exec(ctx, updateQuery, familyID)
	return err
}

// UpdateName atualiza o nome da família.
func (r *FamilyRepository) UpdateName(ctx context.Context, familyID uuid.UUID, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE families SET name = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, name, familyID)
	return err
}

// GetMembers retorna informações detalhadas dos membros da família.
func (r *FamilyRepository) GetMembers(ctx context.Context, familyID uuid.UUID) ([]models.FamilyMemberInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT u.id, u.login, coalesce(u.email, ''), u.created_at
		FROM family_members fm
		JOIN users u ON u.id = fm.user_id
		WHERE fm.family_id = $1
		ORDER BY u.created_at ASC
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.FamilyMemberInfo
	for rows.Next() {
		var m models.FamilyMemberInfo
		if err := rows.Scan(&m.UserID, &m.Login, &m.Email, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}

	if members == nil {
		members = []models.FamilyMemberInfo{}
	}
	return members, nil
}

// RemoveMember remove um membro da família.
func (r *FamilyRepository) RemoveMember(ctx context.Context, familyID, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `DELETE FROM family_members WHERE family_id = $1 AND user_id = $2`
	_, err := r.pool.Exec(ctx, query, familyID, userID)
	return err
}

// UpdatePlan altera o plano, status de assinatura e datas de vigência da família.
func (r *FamilyRepository) UpdatePlan(ctx context.Context, familyID uuid.UUID, plan, status string, expiresAt *time.Time, provider *string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		UPDATE families
		SET plan = $1, subscription_status = $2, subscription_expires_at = $3, subscription_provider = $4, updated_at = now()
		WHERE id = $5
	`
	_, err := r.pool.Exec(ctx, query, plan, status, expiresAt, provider, familyID)
	return err
}

// CountAccounts retorna a quantidade de contas bancárias ativas cadastradas pela família.
func (r *FamilyRepository) CountAccounts(ctx context.Context, familyID uuid.UUID) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE family_id = $1`, familyID).Scan(&count)
	return count, err
}

// CountMembers retorna a quantidade de membros associados à família.
func (r *FamilyRepository) CountMembers(ctx context.Context, familyID uuid.UUID) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM family_members WHERE family_id = $1`, familyID).Scan(&count)
	return count, err
}

