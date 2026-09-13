package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// FamilyInviteRepository handles database operations for family invitations.
type FamilyInviteRepository struct {
	pool *pgxpool.Pool
}

// NewFamilyInviteRepository creates a new FamilyInviteRepository.
func NewFamilyInviteRepository(pool *pgxpool.Pool) *FamilyInviteRepository {
	return &FamilyInviteRepository{pool: pool}
}

// GenerateSecureToken generates a 32-byte cryptographic random hex token (256 bits).
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create generates and inserts a new FamilyInvite.
func (r *FamilyInviteRepository) Create(ctx context.Context, invite *models.FamilyInvite) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if invite.Token == "" {
		token, err := GenerateSecureToken()
		if err != nil {
			return err
		}
		invite.Token = token
	}

	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = time.Now().Add(48 * time.Hour) // 48h default
	}

	query := `
		INSERT INTO family_invites (family_id, token, target_email, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(
		ctx, query,
		invite.FamilyID, invite.Token, invite.TargetEmail, invite.CreatedBy, invite.ExpiresAt,
	).Scan(&invite.ID, &invite.CreatedAt)
}

// FindByToken retrieves an invite by its unique cryptographic token.
func (r *FamilyInviteRepository) FindByToken(ctx context.Context, token string) (*models.FamilyInvite, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var inv models.FamilyInvite
	query := `
		SELECT id, family_id, token, target_email, created_by, created_at, expires_at, used_at, used_by
		FROM family_invites
		WHERE token = $1
	`
	err := r.pool.QueryRow(ctx, query, token).Scan(
		&inv.ID, &inv.FamilyID, &inv.Token, &inv.TargetEmail, &inv.CreatedBy,
		&inv.CreatedAt, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy,
	)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// MarkAsUsed records that an invite was claimed by a specific user.
func (r *FamilyInviteRepository) MarkAsUsed(ctx context.Context, inviteID, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE family_invites SET used_at = now(), used_by = $1 WHERE id = $2 AND used_at IS NULL`
	cmdTag, err := r.pool.Exec(ctx, query, userID, inviteID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return errors.New("convite já utilizado ou inexistente")
	}
	return nil
}

// ListPendingByFamily lists all unused, non-expired invites created for a family.
func (r *FamilyInviteRepository) ListPendingByFamily(ctx context.Context, familyID uuid.UUID) ([]models.FamilyInvite, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, family_id, token, target_email, created_by, created_at, expires_at, used_at, used_by
		FROM family_invites
		WHERE family_id = $1 AND used_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.FamilyInvite
	for rows.Next() {
		var inv models.FamilyInvite
		if err := rows.Scan(
			&inv.ID, &inv.FamilyID, &inv.Token, &inv.TargetEmail, &inv.CreatedBy,
			&inv.CreatedAt, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy,
		); err != nil {
			return nil, err
		}
		list = append(list, inv)
	}

	if list == nil {
		list = []models.FamilyInvite{}
	}
	return list, nil
}

// Revoke deletes or cancels a pending invite.
func (r *FamilyInviteRepository) Revoke(ctx context.Context, inviteID, familyID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `DELETE FROM family_invites WHERE id = $1 AND family_id = $2`
	_, err := r.pool.Exec(ctx, query, inviteID, familyID)
	return err
}
