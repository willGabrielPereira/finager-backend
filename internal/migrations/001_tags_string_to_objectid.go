package migrations

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// M001TagsStringToObjectID converte o campo `tags` de strings (nomes) para ObjectIDs
// nas collections `tag_rules` e `transactions`.
//
// Contexto: na versão 0.x, tags eram armazenadas como []string (ex: ["Compras"]).
// A partir desta versão, tags referencia o ObjectID do documento Tag, permitindo
// que renomear uma tag reflita em todo o histórico sem drift de dados.
//
// Também garante que as 3 novas tags de sistema (Compras, Cinema, Entretenimento)
// existam, pois são necessárias para resolver os nomes durante a conversão.
type M001TagsStringToObjectID struct{}

func (m *M001TagsStringToObjectID) ID() string {
	return "001_tags_string_to_objectid"
}

func (m *M001TagsStringToObjectID) Description() string {
	return "Converte tags de []string para []ObjectID em tag_rules e transactions"
}

func (m *M001TagsStringToObjectID) Up(ctx context.Context, db *mongo.Database) error {
	// Passo 1 — Garante que as novas tags de sistema desta versão existam.
	// (Compras, Cinema, Entretenimento foram introduzidas nesta release.)
	log.Println("  → Passo 1: garantindo novas tags de sistema...")
	if err := m.ensureNewSystemTags(ctx, db); err != nil {
		return fmt.Errorf("passo 1: %w", err)
	}

	// Passo 2 — Constrói o mapa nome → ObjectID a partir da collection tags.
	log.Println("  → Passo 2: carregando mapa de tags...")
	nameToID, err := m.buildNameToIDMap(ctx, db)
	if err != nil {
		return fmt.Errorf("passo 2: %w", err)
	}
	log.Printf("     %d tag(s) carregada(s)", len(nameToID))

	// Passo 3 — Converte tag_rules.tags de string → ObjectID.
	log.Println("  → Passo 3: convertendo tag_rules...")
	if err := m.migrateStringTagsToIDs(ctx, db.Collection("tag_rules"), nameToID); err != nil {
		return fmt.Errorf("passo 3 (tag_rules): %w", err)
	}

	// Passo 4 — Converte transactions.tags de string → ObjectID.
	log.Println("  → Passo 4: convertendo transactions...")
	if err := m.migrateStringTagsToIDs(ctx, db.Collection("transactions"), nameToID); err != nil {
		return fmt.Errorf("passo 4 (transactions): %w", err)
	}

	return nil
}

// ensureNewSystemTags faz upsert das 3 tags de sistema novas desta versão.
func (m *M001TagsStringToObjectID) ensureNewSystemTags(ctx context.Context, db *mongo.Database) error {
	col := db.Collection("tags")

	newTags := []struct {
		Name  string
		Color string
		Icon  string
	}{
		{"Compras", "#FB8C00", "shopping_cart"},
		{"Cinema", "#6D4C41", "movie"},
		{"Entretenimento", "#AB47BC", "sports_esports"},
	}

	for _, t := range newTags {
		filter := bson.M{"is_system": true, "name": t.Name}
		update := bson.M{
			"$set": bson.M{"color": t.Color, "icon": t.Icon},
			"$setOnInsert": bson.M{
				"is_system":  true,
				"created_at": time.Now().UTC(),
			},
		}
		_, err := col.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
		if err != nil {
			return fmt.Errorf("upsert tag %q: %w", t.Name, err)
		}
		log.Printf("     tag %q: ok", t.Name)
	}
	return nil
}

// buildNameToIDMap lê a collection tags e devolve um mapa nome → ObjectID.
func (m *M001TagsStringToObjectID) buildNameToIDMap(ctx context.Context, db *mongo.Database) (map[string]bson.ObjectID, error) {
	cursor, err := db.Collection("tags").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []struct {
		ID   bson.ObjectID `bson:"_id"`
		Name string        `bson:"name"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	m2 := make(map[string]bson.ObjectID, len(docs))
	for _, d := range docs {
		m2[d.Name] = d.ID
	}
	return m2, nil
}

// migrateStringTagsToIDs encontra documentos onde o campo `tags` contém strings
// e substitui cada nome pelo ObjectID do tag correspondente.
// Elementos que já são ObjectID são mantidos sem alteração.
func (m *M001TagsStringToObjectID) migrateStringTagsToIDs(
	ctx context.Context,
	coll *mongo.Collection,
	nameToID map[string]bson.ObjectID,
) error {
	// bson.TypeString = 0x02. Localiza documentos onde tags[] contém pelo menos uma string.
	filter := bson.M{
		"tags": bson.M{"$elemMatch": bson.M{"$type": bson.TypeString}},
	}

	cursor, err := coll.Find(ctx, filter)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var updated int
	for cursor.Next(ctx) {
		var raw bson.Raw
		if err := cursor.Decode(&raw); err != nil {
			return fmt.Errorf("decode documento: %w", err)
		}

		docID := raw.Lookup("_id").ObjectID()

		tagsRawValue, err := raw.LookupErr("tags")
		if err != nil {
			continue // sem campo tags — pula
		}

		arr, ok := tagsRawValue.ArrayOK()
		if !ok {
			continue
		}

		values, err := arr.Values()
		if err != nil {
			return fmt.Errorf("documento %s: falha ao ler array tags: %w", docID.Hex(), err)
		}

		newTagIDs := make([]bson.ObjectID, 0, len(values))
		for _, v := range values {
			switch v.Type {
			case bson.TypeString:
				name := v.StringValue()
				if tagID, found := nameToID[name]; found {
					newTagIDs = append(newTagIDs, tagID)
				} else {
					log.Printf("     ! aviso: tag %q não encontrada na collection tags — elemento ignorado", name)
				}
			case bson.TypeObjectID:
				// Já é ObjectID — mantém como está.
				newTagIDs = append(newTagIDs, v.ObjectID())
			default:
				log.Printf("     ! tipo BSON inesperado (%v) no campo tags do doc %s — elemento ignorado", v.Type, docID.Hex())
			}
		}

		_, err = coll.UpdateOne(
			ctx,
			bson.M{"_id": docID},
			bson.M{"$set": bson.M{"tags": newTagIDs}},
		)
		if err != nil {
			return fmt.Errorf("falha ao atualizar documento %s: %w", docID.Hex(), err)
		}
		updated++
	}

	if err := cursor.Err(); err != nil {
		return err
	}

	if updated == 0 {
		log.Printf("     %s: nenhum documento com tags de string encontrado", coll.Name())
	} else {
		log.Printf("     %s: %d documento(s) convertido(s)", coll.Name(), updated)
	}
	return nil
}

// init registra esta migration automaticamente no momento em que o pacote é importado.
func init() { Register(&M001TagsStringToObjectID{}) }
