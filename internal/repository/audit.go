package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository grava ações administrativas sobre dados de famílias.
type AuditRepository struct {
	pool *pgxpool.Pool
}

// NewAuditRepository instancia o repositório de auditoria.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// Insert registra que adminID executou action sobre familyID, com a justificativa informada.
func (r *AuditRepository) Insert(ctx context.Context, adminID, familyID uuid.UUID, action, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO admin_audit_log (admin_id, family_id, action, reason) VALUES ($1, $2, $3, $4)`,
		adminID, familyID, action, reason)
	return err
}
