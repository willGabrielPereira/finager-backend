package database

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Client wraps the MongoDB client and exposes the target database.
type Client struct {
	client *mongo.Client
	DB     *mongo.Database
}

// Connect establishes a connection to MongoDB using the provided URI and
// database name. It validates the connection with a ping before returning.
func Connect(uri, dbName string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri)
	mongoClient, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, err
	}

	// Verify the connection is alive.
	if err := mongoClient.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Printf("Connected to MongoDB at %s (db: %s)", uri, dbName)

	return &Client{
		client: mongoClient,
		DB:     mongoClient.Database(dbName),
	}, nil
}

// Close gracefully disconnects from MongoDB.
func (c *Client) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.client.Disconnect(ctx); err != nil {
		log.Printf("Error disconnecting from MongoDB: %v", err)
	}
}

// Collection is a convenience helper to obtain a typed collection handle.
func (c *Client) Collection(name string) *mongo.Collection {
	return c.DB.Collection(name)
}
