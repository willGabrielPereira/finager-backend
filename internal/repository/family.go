package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

const familiesCollection = "families"

// FamilyRepository encapsulates persistence operations for Family.
type FamilyRepository struct {
	col *mongo.Collection
}

// NewFamilyRepository creates a FamilyRepository pointing at the correct collection.
func NewFamilyRepository(db *mongo.Database) *FamilyRepository {
	return &FamilyRepository{col: db.Collection(familiesCollection)}
}

// Create inserts a new family document. Sets timestamps automatically.
func (r *FamilyRepository) Create(ctx context.Context, family *models.Family) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now()
	family.CreatedAt = now
	family.UpdatedAt = now
	if family.ID.IsZero() {
		family.ID = bson.NewObjectID()
	}

	_, err := r.col.InsertOne(ctx, family)
	return err
}

// FindByID returns the family with the given ObjectID.
func (r *FamilyRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.Family, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var family models.Family
	if err := r.col.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&family); err != nil {
		return nil, err
	}
	return &family, nil
}

// AddMember appends a user ID to the family's member list (no-op if already present).
// Uses $addToSet to guarantee idempotency.
func (r *FamilyRepository) AddMember(ctx context.Context, familyID, userID bson.ObjectID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.col.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: familyID}},
		bson.D{
			{Key: "$addToSet", Value: bson.D{{Key: "member_ids", Value: userID}}},
			{Key: "$set", Value: bson.D{{Key: "updated_at", Value: time.Now()}}},
		},
	)
	return err
}
