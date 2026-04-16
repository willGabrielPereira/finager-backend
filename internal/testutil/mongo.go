package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SetupMongoDB sobe um container Docker do MongoDB (Ephemeral) específico 
// para o teste atual, garante a conexão, retorna o ponteiro pro banco e 
// registra o cleanup pra destruir o container no final.
func SetupMongoDB(t *testing.T) *mongo.Database {
	t.Helper()
	ctx := context.Background()

	// Sobe a imagem oficial do mongo 7.0
	mongodbContainer, err := mongodb.Run(ctx, "mongo:7.0")
	require.NoError(t, err, "não foi possível subir o container do mongodb")

	// Garante que o container morra no final do teste
	t.Cleanup(func() {
		if err := mongodbContainer.Terminate(ctx); err != nil {
			t.Fatalf("falha ao destruir o container do mongodb: %s", err)
		}
	})

	// Pega a connection string gerada (com a porta mapeada aleatória)
	endpoint, err := mongodbContainer.ConnectionString(ctx)
	require.NoError(t, err)

	clientOpts := options.Client().ApplyURI(endpoint)
	
	// Conecta via driver
	clientCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	client, err := mongo.Connect(clientOpts)
	require.NoError(t, err, "falha ao conectar no mongo")
	
	err = client.Ping(clientCtx, nil)
	require.NoError(t, err, "falha no ping do mongo")

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})

	// Um banco de dados isolado por execução (podemos gerar randômico, mas como é 1 container por teste, "finager_test" serve)
	return client.Database("finager_test")
}
