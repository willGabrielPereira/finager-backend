package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ClassifierState representa o estado serializado dos pesos e contadores estatísticos
// do classificador Naive Bayes para uma família específica.
type ClassifierState struct {
	ID              bson.ObjectID             `bson:"_id,omitempty"       json:"id,omitempty"`
	FamilyID        bson.ObjectID             `bson:"family_id"           json:"family_id"`
	TotalDocs       int                       `bson:"total_docs"          json:"total_docs"`
	ClassDocs       map[string]int            `bson:"class_docs"          json:"class_docs"`           // key é o Hex ObjectID da Tag
	ClassWordCounts map[string]map[string]int `bson:"class_word_counts"   json:"class_word_counts"`     // key é o Hex ObjectID da Tag
	ClassTotalWords map[string]int            `bson:"class_total_words"   json:"class_total_words"`     // key é o Hex ObjectID da Tag
	Vocabulary      []string                  `bson:"vocabulary"          json:"vocabulary"`
	UpdatedAt       time.Time                 `bson:"updated_at"          json:"updated_at"`
}
