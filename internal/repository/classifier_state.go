package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// ClassifierStateRepository encapsula a persistência do estado da IA.
type ClassifierStateRepository struct {
	coll *mongo.Collection
}

// NewClassifierStateRepository cria uma nova instância de ClassifierStateRepository.
func NewClassifierStateRepository(db *mongo.Database) *ClassifierStateRepository {
	return &ClassifierStateRepository{
		coll: db.Collection("classifier_states"),
	}
}

// FindByFamilyID busca o estado persistido do classificador de uma família específica.
func (r *ClassifierStateRepository) FindByFamilyID(ctx context.Context, familyID bson.ObjectID) (*models.ClassifierState, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var state models.ClassifierState
	err := r.coll.FindOne(ctx, bson.M{"family_id": familyID}).Decode(&state)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// UpsertState insere ou atualiza de forma idempotente o estado compilado do classificador.
func (r *ClassifierStateRepository) UpsertState(ctx context.Context, state *models.ClassifierState) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	state.UpdatedAt = time.Now()

	filter := bson.M{"family_id": state.FamilyID}
	update := bson.M{
		"$set": state,
	}

	opts := options.UpdateOne().SetUpsert(true)
	res, err := r.coll.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return err
	}

	if res.UpsertedID != nil {
		if id, ok := res.UpsertedID.(bson.ObjectID); ok {
			state.ID = id
		}
	}

	return nil
}

// EnsureIndexes cria um índice único no campo family_id para buscas instantâneas.
func (r *ClassifierStateRepository) EnsureIndexes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	model := mongo.IndexModel{
		Keys:    bson.D{{Key: "family_id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uid_family_classifier_state"),
	}

	_, err := r.coll.Indexes().CreateOne(ctx, model)
	return err
}
