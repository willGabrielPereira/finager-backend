package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type MerchantMappingRepository struct {
	pool *pgxpool.Pool
}

func NewMerchantMappingRepository(pool *pgxpool.Pool) *MerchantMappingRepository {
	return &MerchantMappingRepository{pool: pool}
}

// Upsert salva ou atualiza uma regra de mapeamento de comerciante para a família.
func (r *MerchantMappingRepository) Upsert(ctx context.Context, familyID uuid.UUID, pattern string, tagID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cleanPattern := strings.ToUpper(strings.TrimSpace(pattern))
	if cleanPattern == "" {
		return nil
	}

	query := `
		INSERT INTO merchant_mappings (family_id, pattern, tag_id, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (family_id, pattern)
		DO UPDATE SET tag_id = EXCLUDED.tag_id, updated_at = now()
	`
	_, err := r.pool.Exec(ctx, query, familyID, cleanPattern, tagID)
	return err
}

// FindMatch busca se o texto fornecido casa com algum padrão de comerciante da família.
// Ordena por tamanho do padrão decrescente para priorizar matches mais específicos (ex: "POSTO SHELL" antes de "POSTO").
func (r *MerchantMappingRepository) FindMatch(ctx context.Context, familyID uuid.UUID, text string) (*uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	upperText := strings.ToUpper(strings.TrimSpace(text))
	if upperText == "" {
		return nil, nil
	}

	query := `
		SELECT tag_id
		FROM merchant_mappings
		WHERE family_id = $1 AND $2 ILIKE '%' || pattern || '%'
		ORDER BY LENGTH(pattern) DESC
		LIMIT 1
	`

	var tagID uuid.UUID
	err := r.pool.QueryRow(ctx, query, familyID, upperText).Scan(&tagID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &tagID, nil
}

// ListByFamily retorna todos os mapeamentos cadastrados para a família com dados da tag.
func (r *MerchantMappingRepository) ListByFamily(ctx context.Context, familyID uuid.UUID) ([]models.MerchantMapping, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT m.id, m.family_id, m.pattern, m.tag_id, m.created_at, m.updated_at,
		       t.id, t.name, t.color
		FROM merchant_mappings m
		LEFT JOIN tags t ON t.id = m.tag_id
		WHERE m.family_id = $1
		ORDER BY m.pattern ASC
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []models.MerchantMapping
	for rows.Next() {
		var m models.MerchantMapping
		var tagID *uuid.UUID
		var tagName *string
		var tagColor *string
		if err := rows.Scan(&m.ID, &m.FamilyID, &m.Pattern, &m.TagID, &m.CreatedAt, &m.UpdatedAt, &tagID, &tagName, &tagColor); err != nil {
			return nil, err
		}
		if tagID != nil && tagName != nil {
			color := ""
			if tagColor != nil {
				color = *tagColor
			}
			m.Tag = &models.Tag{
				ID:    *tagID,
				Name:  *tagName,
				Color: color,
			}
		}
		mappings = append(mappings, m)
	}

	return mappings, nil
}

// Delete remove uma regra de mapeamento da família pelo ID.
func (r *MerchantMappingRepository) Delete(ctx context.Context, familyID uuid.UUID, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `DELETE FROM merchant_mappings WHERE id = $1 AND family_id = $2`
	_, err := r.pool.Exec(ctx, query, id, familyID)
	return err
}

