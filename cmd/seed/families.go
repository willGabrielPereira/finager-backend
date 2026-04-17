package main

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

func ensureFamily(ctx context.Context, db *mongo.Database, name string) *models.Family {
	col := db.Collection("families")
	var family models.Family
	err := col.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&family)
	if err == nil {
		return &family
	}

	family = models.Family{
		ID:        bson.NewObjectID(),
		Name:      name,
		MemberIDs: []bson.ObjectID{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if _, err := col.InsertOne(ctx, family); err != nil {
		log.Fatalf("InsertOne family: %v", err)
	}
	return &family
}
