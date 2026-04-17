package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

const refreshTokensCollection = "refresh_tokens"

// RefreshTokenRepository encapsulates persistence for refresh tokens.
type RefreshTokenRepository struct {
	col *mongo.Collection
}

// NewRefreshTokenRepository creates a RefreshTokenRepository.
func NewRefreshTokenRepository(db *mongo.Database) *RefreshTokenRepository {
	return &RefreshTokenRepository{col: db.Collection(refreshTokensCollection)}
}

// EnsureIndexes creates TTL + unique hash indexes (idempotent).
func (r *RefreshTokenRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Unique index enables fast O(1) lookup by hash.
			Keys:    bson.D{{Key: "token_hash", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			// TTL index: MongoDB deletes documents when expires_at is in the past.
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})
	return err
}

// Create stores a new refresh token record.
func (r *RefreshTokenRepository) Create(ctx context.Context, token *models.RefreshToken) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if token.ID.IsZero() {
		token.ID = bson.NewObjectID()
	}
	_, err := r.col.InsertOne(ctx, token)
	return err
}

// FindByHash returns a non-revoked refresh token by its SHA-256 hash.
// Returns mongo.ErrNoDocuments if not found or already revoked.
func (r *RefreshTokenRepository) FindByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var token models.RefreshToken
	err := r.col.FindOne(ctx, bson.D{
		{Key: "token_hash", Value: hash},
		{Key: "revoked", Value: false},
	}).Decode(&token)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// Revoke marks a single refresh token as revoked by its ID.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, tokenID bson.ObjectID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.col.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: tokenID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "revoked", Value: true}}}},
	)
	return err
}

// RevokeAllByUser marks ALL refresh tokens for a user as revoked.
// Called after a password change to force re-login on all devices.
func (r *RefreshTokenRepository) RevokeAllByUser(ctx context.Context, userID bson.ObjectID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.col.UpdateMany(ctx,
		bson.D{{Key: "user_id", Value: userID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "revoked", Value: true}}}},
	)
	return err
}
