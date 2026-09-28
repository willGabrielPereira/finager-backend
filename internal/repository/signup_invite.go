package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// SignupInviteRepository gerencia a persistência de convites de cadastro.
type SignupInviteRepository struct {
	pool *pgxpool.Pool
}

// NewSignupInviteRepository instancia o repositório de convites de cadastro.
func NewSignupInviteRepository(pool *pgxpool.Pool) *SignupInviteRepository {
	return &SignupInviteRepository{pool: pool}
}

// Create insere um novo convite de cadastro.
func (r *SignupInviteRepository) Create(ctx context.Context, invite *models.SignupInvite) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO signup_invites (token, plan_granted, created_by, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(
		ctx, query,
		invite.Token, invite.PlanGranted, invite.CreatedBy, invite.ExpiresAt,
	).Scan(&invite.ID, &invite.CreatedAt)
}

// GetByToken busca um convite de cadastro pelo token.
func (r *SignupInviteRepository) GetByToken(ctx context.Context, token string) (*models.SignupInvite, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var invite models.SignupInvite
	query := `
		SELECT id, token, plan_granted, created_by, expires_at, used_at, used_by_family_id, created_at
		FROM signup_invites
		WHERE token = $1
	`
	err := r.pool.QueryRow(ctx, query, token).Scan(
		&invite.ID, &invite.Token, &invite.PlanGranted, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.UsedAt, &invite.UsedByFamilyID, &invite.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &invite, nil
}

// ClaimByToken reivindica um convite de cadastro de forma atômica: marca used_at = now()
// somente se o convite existir, não estiver usado e não estiver expirado, numa única
// operação condicional (evita corrida entre requisições concorrentes usando o mesmo token).
// Reivindicar ANTES de criar usuário/família é o que evita tanto o reuso indevido do
// convite (fail-open) quanto a criação de uma conta órfã quando duas requisições disputam
// o mesmo token.
// Retorna o convite reivindicado, ou (nil, nil) se a condição não bateu (o chamador deve
// então investigar o motivo específico com GetByToken para dar a mensagem de erro correta).
func (r *SignupInviteRepository) ClaimByToken(ctx context.Context, token string) (*models.SignupInvite, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var invite models.SignupInvite
	query := `
		UPDATE signup_invites
		SET used_at = now()
		WHERE token = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING id, token, plan_granted, created_by, expires_at, used_at, used_by_family_id, created_at
	`
	err := r.pool.QueryRow(ctx, query, token).Scan(
		&invite.ID, &invite.Token, &invite.PlanGranted, &invite.CreatedBy,
		&invite.ExpiresAt, &invite.UsedAt, &invite.UsedByFamilyID, &invite.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &invite, nil
}

// SetUsedFamily grava qual família usou o convite (bookkeeping, não crítico para segurança —
// a reivindicação em si já aconteceu via ClaimByToken).
func (r *SignupInviteRepository) SetUsedFamily(ctx context.Context, inviteID, familyID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE signup_invites SET used_by_family_id = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, familyID, inviteID)
	return err
}

// ReleaseClaim reverte uma reivindicação quando a criação da conta falha DEPOIS do claim
// bem-sucedido, para não desperdiçar um convite válido por um erro transitório não relacionado.
func (r *SignupInviteRepository) ReleaseClaim(ctx context.Context, inviteID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE signup_invites SET used_at = NULL, used_by_family_id = NULL WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, inviteID)
	return err
}

// List retorna todos os convites de cadastro, mais recentes primeiro.
func (r *SignupInviteRepository) List(ctx context.Context) ([]models.SignupInvite, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, token, plan_granted, created_by, expires_at, used_at, used_by_family_id, created_at
		FROM signup_invites
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.SignupInvite
	for rows.Next() {
		var invite models.SignupInvite
		if err := rows.Scan(
			&invite.ID, &invite.Token, &invite.PlanGranted, &invite.CreatedBy,
			&invite.ExpiresAt, &invite.UsedAt, &invite.UsedByFamilyID, &invite.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, invite)
	}

	if list == nil {
		list = []models.SignupInvite{}
	}
	return list, nil
}

// Delete revoga um convite de cadastro ainda não utilizado.
func (r *SignupInviteRepository) Delete(ctx context.Context, inviteID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `DELETE FROM signup_invites WHERE id = $1 AND used_at IS NULL`
	cmdTag, err := r.pool.Exec(ctx, query, inviteID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return errors.New("não é possível revogar um convite já utilizado ou inexistente")
	}
	return nil
}
