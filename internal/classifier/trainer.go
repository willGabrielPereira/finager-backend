package classifier

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// TrainForFamily inicializa e treina o classificador local para uma família específica.
// Se o familyID for bson.NilObjectID, treina o Classificador Global do Sistema, filtrando
// e mantendo estritamente apenas tags globais de sistema (anti-poisoning e RLS isolation).
func TrainForFamily(
	ctx context.Context,
	familyID *uuid.UUID,
	tagRepo *repository.TagRepository,
	txRepo *repository.TransactionRepository,
) (*Classifier, error) {
	c := New()

	var tags []*models.Tag
	var err error

	isGlobalTraining := familyID == nil

	// 1. Carrega as tags visíveis dependendo do escopo (Global ou Familiar)
	if isGlobalTraining {
		// IA Global: Carrega apenas tags nativas de sistema
		tags, err = tagRepo.FindSystemTags(ctx)
	} else {
		// IA Local: Carrega todas as tags visíveis da família
		tags, err = tagRepo.FindAllVisible(ctx, *familyID)
	}
	if err != nil {
		return nil, err
	}

	// Mapeia Nome da Tag -> ObjectID
	tagMap := make(map[string]uuid.UUID)
	allowedTagIDs := make(map[uuid.UUID]struct{})

	for _, t := range tags {
		tagMap[strings.ToLower(t.Name)] = t.ID
		allowedTagIDs[t.ID] = struct{}{}
	}

	// 2. Treina o classificador com a base estática de dados para Cold-Start
	for _, data := range DefaultTrainingData {
		targetNameLow := strings.ToLower(data.TagName)
		if tagID, exists := tagMap[targetNameLow]; exists {
			c.Train(data.Text, tagID)
		}
	}

	// 3. Treina o classificador com as transações categorizadas
	taggedTransactions, err := txRepo.FindAllTagged(ctx, familyID)
	if err != nil {
		// Retorna o cold-start estático em caso de falha de conexão com o banco
		return c, nil
	}

	for _, tx := range taggedTransactions {
		for _, tagID := range tx.Tags {
			// Filtro de Segurança Anti-Envenenamento / Anti-Vazamento:
			// Se for treinamento global, ignora qualquer tag customizada criada por famílias individuais.
			if isGlobalTraining {
				if _, allowed := allowedTagIDs[tagID]; !allowed {
					continue
				}
			}
			c.Train(tx.Name+" "+tx.Memo, tagID)
		}
	}

	return c, nil
}

// GetOrBuildForFamily busca o classificador persistido no MongoDB.
// Se for uma família nova (Day 0), herda e clona o Estado Global compilado coletivamente.
func GetOrBuildForFamily(
	ctx context.Context,
	familyID *uuid.UUID,
	tagRepo *repository.TagRepository,
	txRepo *repository.TransactionRepository,
	stateRepo *repository.ClassifierStateRepository,
) (*Classifier, error) {
	// 1. Tenta buscar o estado persistido da própria família
	state, err := stateRepo.FindByFamilyID(ctx, familyID)
	if err == nil && state != nil {
		// O(1) Cache hit local
		return RestoreState(state), nil
	}

	// 2. Cache miss da família: Se for Day 0, tenta herdar o Estado Global coletivo
	if familyID != nil {
		globalState, err := stateRepo.FindByFamilyID(ctx, nil)
		if err == nil && globalState != nil {
			// Clona o Estado Global como o ponto de partida inteligente (Day 0) da nova família
			globalState.ID = uuid.Nil // Garante a criação de um novo registro
			globalState.FamilyID = familyID
			_ = stateRepo.UpsertState(ctx, globalState)

			return RestoreState(globalState), nil
		}
	}

	// 3. Se nem o estado global existir, reconstrói o estado global em segundo plano
	if familyID != nil {
		go func() {
			_, _ = RebuildStateForFamily(context.Background(), nil, tagRepo, txRepo, stateRepo)
		}()
	}

	// Reconstrói e salva o estado da família localmente
	return RebuildStateForFamily(ctx, familyID, tagRepo, txRepo, stateRepo)
}

// RebuildStateForFamily lê o histórico do banco, reconstrói o classificador Naive Bayes,
// compila os contadores estatísticos e salva na collection classifier_states do MongoDB.
func RebuildStateForFamily(
	ctx context.Context,
	familyID *uuid.UUID,
	tagRepo *repository.TagRepository,
	txRepo *repository.TransactionRepository,
	stateRepo *repository.ClassifierStateRepository,
) (*Classifier, error) {
	c, err := TrainForFamily(ctx, familyID, tagRepo, txRepo)
	if err != nil {
		return nil, err
	}

	// Compila e exporta os contadores do classificador
	state := c.ExportState(familyID)

	// Salva na collection classifier_states do MongoDB
	err = stateRepo.UpsertState(ctx, state)
	if err != nil {
		return c, nil
	}

	// Se acabamos de atualizar o classificador de uma família local, disparamos de forma assíncrona
	// a reconstrução do Cérebro Global para incorporar os novos padrões aprendidos de forma coletiva!
	if familyID != nil {
		go func() {
			_, _ = RebuildStateForFamily(context.Background(), nil, tagRepo, txRepo, stateRepo)
		}()
	}

	return c, nil
}
