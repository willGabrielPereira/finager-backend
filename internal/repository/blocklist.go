package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const blocklistCollection = "revoked_tokens"

// blocklistedToken is the document stored in the revoked_tokens collection.
// The raw access token is NEVER stored — only its SHA-256 hash.
type blocklistedToken struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	TokenHash string        `bson:"token_hash"`
	ExpiresAt time.Time     `bson:"expires_at"` // TTL index — auto-deleted by MongoDB
}

// BlocklistRepository manages the revocation blocklist for access tokens.
// On logout, the access token's hash is inserted here. The Authenticate
// middleware checks this collection on every protected request.
type BlocklistRepository struct {
	col *mongo.Collection
}

// NewBlocklistRepository creates a BlocklistRepository.
func NewBlocklistRepository(db *mongo.Database) *BlocklistRepository {
	return &BlocklistRepository{col: db.Collection(blocklistCollection)}
}

// EnsureIndexes creates TTL + unique hash indexes (idempotent).
func (r *BlocklistRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "token_hash", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			// MongoDB deletes the document automatically when expires_at passes.
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})
	return err
}

// Add inserts a token hash into the blocklist. The document will be
// automatically removed by MongoDB when expiresAt is reached.
func (r *BlocklistRepository) Add(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	doc := blocklistedToken{
		ID:        bson.NewObjectID(),
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	_, err := r.col.InsertOne(ctx, doc)
	return err
}

// IsBlocked reports whether the given token hash is present in the blocklist.
func (r *BlocklistRepository) IsBlocked(ctx context.Context, tokenHash string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	count, err := r.col.CountDocuments(ctx, bson.D{{Key: "token_hash", Value: tokenHash}})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
