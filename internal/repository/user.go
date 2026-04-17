package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

const usersCollection = "users"

// UserRepository encapsulates persistence operations for User.
type UserRepository struct {
	col *mongo.Collection
}

// NewUserRepository creates a UserRepository pointing at the correct collection.
func NewUserRepository(db *mongo.Database) *UserRepository {
	return &UserRepository{col: db.Collection(usersCollection)}
}

// EnsureIndexes creates required indexes (idempotent — safe to call on startup).
func (r *UserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "login", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "family_id", Value: 1}},
		},
	})
	return err
}

// FindByLogin returns the user with the given login, or mongo.ErrNoDocuments.
func (r *UserRepository) FindByLogin(ctx context.Context, login string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	if err := r.col.FindOne(ctx, bson.D{{Key: "login", Value: login}}).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByID returns the user with the given ObjectID.
func (r *UserRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	if err := r.col.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// Create inserts a new user. Sets CreatedAt and UpdatedAt automatically.
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.ID.IsZero() {
		user.ID = bson.NewObjectID()
	}

	_, err := r.col.InsertOne(ctx, user)
	return err
}

// UpdatePassword sets a new bcrypt password hash for the given user.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID bson.ObjectID, newHash string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.col.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "password_hash", Value: newHash},
			{Key: "updated_at", Value: time.Now()},
		}}},
	)
	return err
}

// UpdateFamilyID reassigns the user to a different family.
// Used when a user joins an existing shared family.
func (r *UserRepository) UpdateFamilyID(ctx context.Context, userID, familyID bson.ObjectID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.col.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "family_id", Value: familyID},
			{Key: "updated_at", Value: time.Now()},
		}}},
	)
	return err
}
