package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type TagRepository struct {
	pool *pgxpool.Pool
}

func NewTagRepository(pool *pgxpool.Pool) *TagRepository {
	return &TagRepository{pool: pool}
}

// FindAllVisible busca tags do sistema (compartilhadas globalmente) 
// somadas às tags customizadas exclusivas desta família.
func (r *TagRepository) FindAllVisible(ctx context.Context, familyID uuid.UUID) ([]*models.Tag, error) {
	query := `
		SELECT id, name, color, icon, family_id, is_system, created_at
		FROM tags
		WHERE is_system = true OR family_id = $1
		ORDER BY name ASC
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Icon, &tag.FamilyID, &tag.IsSystem, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, &tag)
	}

	if tags == nil {
		tags = []*models.Tag{}
	}

	return tags, nil
}

// Create permite criar tags customizadas
func (r *TagRepository) Create(ctx context.Context, tag *models.Tag) error {
	query := `
		INSERT INTO tags (name, color, icon, family_id, is_system)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	err := r.pool.QueryRow(ctx, query, tag.Name, tag.Color, tag.Icon, tag.FamilyID, tag.IsSystem).Scan(&tag.ID, &tag.CreatedAt)
	return err
}

// FindByID busca uma tag especifica.
func (r *TagRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Tag, error) {
	var tag models.Tag
	query := `SELECT id, name, color, icon, family_id, is_system, created_at FROM tags WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Icon, &tag.FamilyID, &tag.IsSystem, &tag.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// Update altera configurações visuais de uma tag (Color, Icon, Name)
func (r *TagRepository) Update(ctx context.Context, id uuid.UUID, updates map[string]interface{}) error {
	// Updates suportados: name, color, icon
	// Simplificado para atualizar a tag inteira já que a rota de update passa todos os campos
	// Mas como a assinatura antiga usava bson.M, vamos implementar com map provisoriamente
	// É melhor atualizar os campos nomeados ou iterar no map.
	// Por simplicidade, assumimos que o chamador só envia name, color e icon.
	
	name, okName := updates["name"].(string)
	color, okColor := updates["color"].(string)
	icon, okIcon := updates["icon"].(string)

	query := `UPDATE tags SET `
	args := []interface{}{}
	i := 1

	if okName {
		query += `name = $` + string(rune(48+i)) + `, `
		args = append(args, name)
		i++
	}
	if okColor {
		query += `color = $` + string(rune(48+i)) + `, `
		args = append(args, color)
		i++
	}
	if okIcon {
		query += `icon = $` + string(rune(48+i)) + ` `
		args = append(args, icon)
		i++
	}

	// Remove trailing comma if present and finish query
	if query[len(query)-2:] == ", " {
		query = query[:len(query)-2]
	}
	query += ` WHERE id = $` + string(rune(48+i))
	args = append(args, id)

	_, err := r.pool.Exec(ctx, query, args...)
	return err
}

// Delete remove uma tag permanentemente.
func (r *TagRepository) Delete(ctx context.Context, id uuid.UUID, familyID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM tags WHERE id = $1 AND family_id = $2`, id, familyID)
	return err
}

// UpsertSystemTag insere ou atualiza uma tag global baseada no seu nome de forma IDEMPOTENTE.
// Excelente pra ser usado pelo script de Seed durante cada deployment.
func (r *TagRepository) UpsertSystemTag(ctx context.Context, tag *models.Tag) error {

	// O Postgres precisa de UNIQUE index para o ON CONFLICT funcionar sem quebrar,
	// vamos fazer um upsert manual com verificação se não houver index único
	
	var existingID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM tags WHERE is_system = true AND name = $1`, tag.Name).Scan(&existingID)
	if err == pgx.ErrNoRows {
		// Insert
		insertQuery := `INSERT INTO tags (name, color, icon, is_system) VALUES ($1, $2, $3, true) RETURNING id`
		return r.pool.QueryRow(ctx, insertQuery, tag.Name, tag.Color, tag.Icon).Scan(&tag.ID)
	} else if err == nil {
		// Update
		updateQuery := `UPDATE tags SET color = $1, icon = $2 WHERE id = $3`
		_, err = r.pool.Exec(ctx, updateQuery, tag.Color, tag.Icon, existingID)
		tag.ID = existingID
		return err
	}
	return err
}

// FindSystemTags retorna todas as tags globais de sistema.
// Usado pelo seed para resolver nome → ObjectID antes de criar as TagRules.
func (r *TagRepository) FindSystemTags(ctx context.Context) ([]*models.Tag, error) {
	query := `SELECT id, name, color, icon, family_id, is_system, created_at FROM tags WHERE is_system = true`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Icon, &tag.FamilyID, &tag.IsSystem, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, &tag)
	}
	return tags, nil
}
