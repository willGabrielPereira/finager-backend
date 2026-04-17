package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type TagRepository struct {
	coll *mongo.Collection
}

func NewTagRepository(db *mongo.Database) *TagRepository {
	return &TagRepository{
		coll: db.Collection("tags"),
	}
}

// FindAllVisible busca tags do sistema (compartilhadas globalmente) 
// somadas às tags customizadas exclusivas desta família.
func (r *TagRepository) FindAllVisible(ctx context.Context, familyID bson.ObjectID) ([]*models.Tag, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"is_system": true},
			{"family_id": familyID},
		},
	}

	cursor, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tags []*models.Tag
	if err := cursor.All(ctx, &tags); err != nil {
		return nil, err
	}

	// Se não veio nada, protege o frontend mandando um array vazio e não null.
	if tags == nil {
		tags = []*models.Tag{}
	}

	return tags, nil
}

// Create permite criar tags customizadas
func (r *TagRepository) Create(ctx context.Context, tag *models.Tag) error {
	res, err := r.coll.InsertOne(ctx, tag)
	if err != nil {
		return err
	}
	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		tag.ID = id
	}
	return nil
}

// FindByID busca uma tag especifica.
func (r *TagRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.Tag, error) {
	var tag models.Tag
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&tag)
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// Update altera configurações visuais de uma tag (Color, Icon, Name)
func (r *TagRepository) Update(ctx context.Context, id bson.ObjectID, updates bson.M) error {
	_, err := r.coll.UpdateByID(ctx, id, bson.M{"$set": updates})
	return err
}

// Delete remove uma tag permanentemente.
func (r *TagRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// UpsertSystemTag insere ou atualiza uma tag global baseada no seu nome de forma IDEMPOTENTE.
// Excelente pra ser usado pelo script de Seed durante cada deployment.
func (r *TagRepository) UpsertSystemTag(ctx context.Context, tag *models.Tag) error {
	filter := bson.M{
		"is_system": true,
		"name":      tag.Name,
	}
	
	update := bson.M{
		"$set": bson.M{
			"color": tag.Color,
			"icon":  tag.Icon,
		},
		"$setOnInsert": bson.M{
			"created_at": time.Now(),
		},
	}
	
	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// FindSystemTags retorna todas as tags globais de sistema.
// Usado pelo seed para resolver nome → ObjectID antes de criar as TagRules.
func (r *TagRepository) FindSystemTags(ctx context.Context) ([]*models.Tag, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"is_system": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tags []*models.Tag
	if err := cursor.All(ctx, &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

