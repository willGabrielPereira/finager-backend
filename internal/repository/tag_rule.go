package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type TagRuleRepository struct {
	coll *mongo.Collection
}

func NewTagRuleRepository(db *mongo.Database) *TagRuleRepository {
	return &TagRuleRepository{coll: db.Collection("tag_rules")}
}

// EnsureIndexes cria os índices necessários para a collection tag_rules.
// Idiempotente — pode ser chamado em todo startup.
func (r *TagRuleRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Unicidade de padrão por família (regras de família não se duplicam)
		{
			Keys: bson.D{
				{Key: "family_id", Value: 1},
				{Key: "pattern", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetSparse(true). // permite family_id null (system rules)
				SetName("idx_tagrule_family_pattern"),
		},
		// Unicidade de padrão para regras de sistema
		{
			Keys: bson.D{
				{Key: "is_system", Value: 1},
				{Key: "pattern", Value: 1},
			},
			Options: options.Index().SetName("idx_tagrule_system_pattern"),
		},
	})
	return err
}

// FindAllVisible retorna regras do sistema + regras da família indicada,
// ordenadas: sistema primeiro, família depois (para que o Apply() dê
// precedência às regras de família de forma natural).
func (r *TagRuleRepository) FindAllVisible(ctx context.Context, familyID bson.ObjectID) ([]*models.TagRule, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"is_system": true},
			{"family_id": familyID},
		},
	}

	// Sistema sempre antes de família para o Apply() funcionar corretamente.
	opts := options.Find().SetSort(bson.D{
		{Key: "is_system", Value: -1}, // true (1) antes de false (0)
		{Key: "created_at", Value: 1},
	})

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []*models.TagRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, err
	}

	if rules == nil {
		rules = []*models.TagRule{}
	}

	return rules, nil
}

// Create persiste uma nova regra de família.
func (r *TagRuleRepository) Create(ctx context.Context, rule *models.TagRule) error {
	res, err := r.coll.InsertOne(ctx, rule)
	if err != nil {
		return err
	}
	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		rule.ID = id
	}
	return nil
}

// FindByID busca uma regra pelo seu ObjectID.
func (r *TagRuleRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.TagRule, error) {
	var rule models.TagRule
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// Update altera os campos de uma regra (tags e/ou padrão).
func (r *TagRuleRepository) Update(ctx context.Context, id bson.ObjectID, updates bson.M) error {
	updates["updated_at"] = time.Now().UTC()
	_, err := r.coll.UpdateByID(ctx, id, bson.M{"$set": updates})
	return err
}

// Delete remove uma regra de família permanentemente.
func (r *TagRuleRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// UpsertSystemRule insere ou atualiza uma regra de sistema de forma idempotente,
// usando o padrão como chave de unicidade.
func (r *TagRuleRepository) UpsertSystemRule(ctx context.Context, rule *models.TagRule) error {
	filter := bson.M{
		"is_system": true,
		"pattern":   rule.Pattern,
	}

	update := bson.M{
		"$set": bson.M{
			"tags":       rule.Tags,
			"updated_at": time.Now().UTC(),
		},
		"$setOnInsert": bson.M{
			"is_system":  true,
			"family_id":  nil,
			"created_by": nil,
			"created_at": time.Now().UTC(),
		},
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, update, opts)
	return err
}
