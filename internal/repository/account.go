package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type AccountRepository struct {
	coll *mongo.Collection
}

func NewAccountRepository(db *mongo.Database) *AccountRepository {
	return &AccountRepository{
		coll: db.Collection("accounts"),
	}
}

// FindVisibleAccounts aplica a Regra de Negócio de Permissão de Visualização.
// A conta entra na lista se: 
// a) Pertencer à Família
// b) E: [For publicamente compartilhada (Lista AllowedUsers vazia) OU O ID do usuario estiver lá dentro].
func (r *AccountRepository) FindVisibleAccounts(ctx context.Context, familyID, userID bson.ObjectID) ([]*models.Account, error) {
	filter := bson.M{
		"family_id": familyID,
		"$or": []bson.M{
			{"allowed_users": bson.M{"$size": 0}},       // Compartilhado com todos
			{"allowed_users": bson.M{"$exists": false}}, // Garantia extra legada
			{"allowed_users": userID},                   // Restrição respeita acesso explícito
		},
	}

	cursor, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var accounts []*models.Account
	if err := cursor.All(ctx, &accounts); err != nil {
		return nil, err
	}
	
	if accounts == nil {
		accounts = []*models.Account{}
	}
	
	return accounts, nil
}

// Create salva uma nova conta
func (r *AccountRepository) Create(ctx context.Context, acc *models.Account) error {
	acc.CreatedAt = time.Now()
	acc.UpdatedAt = time.Now()
	
	// Previne nil slices atrapalhando queries de empty
	if acc.AllowedUsers == nil {
		acc.AllowedUsers = []bson.ObjectID{} 
	}

	res, err := r.coll.InsertOne(ctx, acc)
	if err != nil {
		return err
	}
	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		acc.ID = id
	}
	return nil
}

// FindByID busca a conta crua. Usado em validações onde o middleware ou lógicas já blindaram scopes.
func (r *AccountRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.Account, error) {
	var a models.Account
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Update altera metadados e privacidades da conta
func (r *AccountRepository) Update(ctx context.Context, id bson.ObjectID, updates bson.M) error {
	updates["updated_at"] = time.Now()
	_, err := r.coll.UpdateByID(ctx, id, bson.M{"$set": updates})
	return err
}

// Delete limpa a conta
func (r *AccountRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
