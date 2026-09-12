package models

import (
	"time"

	"github.com/google/uuid"
)

// ClassifierState representa o estado serializado dos pesos e contadores estatísticos
// do classificador Naive Bayes para uma família específica.
type ClassifierState struct {
	ID              uuid.UUID                   `json:"id,omitempty"`
	FamilyID        *uuid.UUID                  `json:"family_id"`
	TotalDocs       int                         `json:"total_docs"`
	ClassDocs       map[string]int              `json:"class_docs"`           // key é o Hex ObjectID da Tag
	ClassWordCounts map[string]map[string]int   `json:"class_word_counts"`     // key é o Hex ObjectID da Tag
	ClassTotalWords map[string]int              `json:"class_total_words"`     // key é o Hex ObjectID da Tag
	Vocabulary      []string                    `json:"vocabulary"`
	UpdatedAt       time.Time                   `json:"updated_at"`
}
