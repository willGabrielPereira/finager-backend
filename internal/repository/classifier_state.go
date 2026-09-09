package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type ClassifierStateRepository struct {
	pool *pgxpool.Pool
}

func NewClassifierStateRepository(pool *pgxpool.Pool) *ClassifierStateRepository {
	return &ClassifierStateRepository{pool: pool}
}

func (r *ClassifierStateRepository) EnsureIndexes(ctx context.Context) error {
	return nil
}

func (r *ClassifierStateRepository) FindByFamilyID(ctx context.Context, familyID *uuid.UUID) (*models.ClassifierState, error) {
	query := `
		SELECT id, family_id, total_docs, class_docs, class_word_counts, class_total_words, vocabulary, updated_at
		FROM classifier_states
		WHERE 
	`
	args := []interface{}{}
	if familyID == nil {
		query += `family_id IS NULL`
	} else {
		query += `family_id = $1`
		args = append(args, familyID)
	}

	var state models.ClassifierState
	var classDocsJSON, classWordCountsJSON, classTotalWordsJSON, vocabularyJSON []byte

	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&state.ID, &state.FamilyID, &state.TotalDocs,
		&classDocsJSON, &classWordCountsJSON, &classTotalWordsJSON, &vocabularyJSON,
		&state.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		// Retornar um estado vazio válido caso não exista
		return &models.ClassifierState{
			FamilyID:        familyID,
			ClassDocs:       make(map[string]int),
			ClassWordCounts: make(map[string]map[string]int),
			ClassTotalWords: make(map[string]int),
			Vocabulary:      make([]string, 0),
		}, nil
	} else if err != nil {
		return nil, err
	}

	json.Unmarshal(classDocsJSON, &state.ClassDocs)
	json.Unmarshal(classWordCountsJSON, &state.ClassWordCounts)
	json.Unmarshal(classTotalWordsJSON, &state.ClassTotalWords)
	json.Unmarshal(vocabularyJSON, &state.Vocabulary)

	if state.ClassDocs == nil {
		state.ClassDocs = make(map[string]int)
	}
	if state.ClassWordCounts == nil {
		state.ClassWordCounts = make(map[string]map[string]int)
	}
	if state.ClassTotalWords == nil {
		state.ClassTotalWords = make(map[string]int)
	}
	if state.Vocabulary == nil {
		state.Vocabulary = make([]string, 0)
	}

	return &state, nil
}

func (r *ClassifierStateRepository) UpsertState(ctx context.Context, state *models.ClassifierState) error {
	classDocsJSON, _ := json.Marshal(state.ClassDocs)
	classWordCountsJSON, _ := json.Marshal(state.ClassWordCounts)
	classTotalWordsJSON, _ := json.Marshal(state.ClassTotalWords)
	vocabularyJSON, _ := json.Marshal(state.Vocabulary)

	query := `
		INSERT INTO classifier_states (family_id, total_docs, class_docs, class_word_counts, class_total_words, vocabulary, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (family_id) 
		DO UPDATE SET 
			total_docs = EXCLUDED.total_docs,
			class_docs = EXCLUDED.class_docs,
			class_word_counts = EXCLUDED.class_word_counts,
			class_total_words = EXCLUDED.class_total_words,
			vocabulary = EXCLUDED.vocabulary,
			updated_at = EXCLUDED.updated_at
	`
	// Handle NULL for global state correctly with ON CONFLICT NULL. 
	// PostgreSQL doesn't consider NULLs equal in UNIQUE constraints by default until v15 with NULLS NOT DISTINCT.
	// We'll use a manual check if familyID is nil.
	
	if state.FamilyID == nil {
		var existingID uuid.UUID
		err := r.pool.QueryRow(ctx, `SELECT id FROM classifier_states WHERE family_id IS NULL`).Scan(&existingID)
		if err == pgx.ErrNoRows {
			// Insert
			insertQ := `INSERT INTO classifier_states (family_id, total_docs, class_docs, class_word_counts, class_total_words, vocabulary, updated_at) VALUES (NULL, $1, $2, $3, $4, $5, now()) RETURNING id`
			return r.pool.QueryRow(ctx, insertQ, state.TotalDocs, classDocsJSON, classWordCountsJSON, classTotalWordsJSON, vocabularyJSON).Scan(&state.ID)
		} else if err == nil {
			// Update
			updateQ := `UPDATE classifier_states SET total_docs = $1, class_docs = $2, class_word_counts = $3, class_total_words = $4, vocabulary = $5, updated_at = now() WHERE id = $6`
			_, err = r.pool.Exec(ctx, updateQ, state.TotalDocs, classDocsJSON, classWordCountsJSON, classTotalWordsJSON, vocabularyJSON, existingID)
			state.ID = existingID
			return err
		}
		return err
	}

	_, err := r.pool.Exec(ctx, query, state.FamilyID, state.TotalDocs, classDocsJSON, classWordCountsJSON, classTotalWordsJSON, vocabularyJSON)
	return err
}
