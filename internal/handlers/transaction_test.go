package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

func TestTransactionIntegrationAndSecurity(t *testing.T) {
	// 1. Inicia o MongoDB Real (Descartável do Testcontainers)
	db := testutil.SetupMongoDB(t)
	repos := repository.New(db)
	err := repos.EnsureIndexes(context.Background())
	require.NoError(t, err)

	// 2. Mock do AuthService para geração de tokens para nossos HTTP Tests
	authSvc := auth.NewService("teste-secret", 1)

	// 3. Monta e prepara o Router com os Handlers + Middlewares Exatos de Produção
	mux := http.NewServeMux()
	
	// Middleware de autenticação real (vai validar os tokens e bloquear blocklist)
	authMid := middleware.Authenticate(authSvc, repos.Blocklist)
	
	txHandler := handlers.NewTransactionHandler(repos.Transactions, repos.Accounts, repos.Tags, repos.ClassifierStates)
	
	mux.Handle("POST /transactions/import", authMid(http.HandlerFunc(txHandler.Import)))
	mux.Handle("GET /transactions", authMid(http.HandlerFunc(txHandler.List)))

	// 4. Criação de cenários Multi-Tenant
	familyA_ID := bson.NewObjectID()
	familyB_ID := bson.NewObjectID()

	userA := &models.User{ID: bson.NewObjectID(), Login: "alice", FamilyID: familyA_ID}
	userB := &models.User{ID: bson.NewObjectID(), Login: "bob", FamilyID: familyB_ID}
	userCharlie := &models.User{ID: bson.NewObjectID(), Login: "charlie", FamilyID: familyA_ID}

	// Gera os Bearer tokens simulando um Login de sucesso
	tokenUserA, _ := authSvc.GenerateToken(userA)
	tokenUserB, _ := authSvc.GenerateToken(userB)
	tokenUserCharlie, _ := authSvc.GenerateToken(userCharlie)

	// =====================================================================================
	// TESTE A: Importação de OFX pela Alice (Família A)
	// =====================================================================================
	// 5. Instanciar Contas Bancárias para o Teste
	accountSharedA := &models.Account{ID: bson.NewObjectID(), Name: "Shared Alice/Charlie", FamilyID: familyA_ID, AllowedUsers: []bson.ObjectID{}}
	accountPrivateB := &models.Account{ID: bson.NewObjectID(), Name: "Private Bob", FamilyID: familyB_ID, AllowedUsers: []bson.ObjectID{userB.ID}}

	_ = repos.Accounts.Create(context.Background(), accountSharedA)
	_ = repos.Accounts.Create(context.Background(), accountPrivateB)

	t.Run("Alice (Família A) consegue importar o arquivo OFX informando a Conta", func(t *testing.T) {
		// Abre o arquivo fake de OFX criado com as 2 transações
		ofxPath := filepath.Join("..", "testutil", "testdata", "sample.ofx")
		file, err := os.Open(ofxPath)
		require.NoError(t, err)
		defer file.Close()

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		
		// Insere campo obrigatório Account ID
		_ = writer.WriteField("account_id", accountSharedA.ID.Hex())
		
		part, _ := writer.CreateFormFile("file", filepath.Base(ofxPath))
		io.Copy(part, file)
		writer.Close() // Fecha para gravar o boundary final

		req := httptest.NewRequest("POST", "/transactions/import", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+tokenUserA)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code, "A importação deveria ter retornado OK: %s", rr.Body.String())

		// Como o arquivo possuía 2 transações e o banco está vazio, o Inserted deve ser 2.
		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)
		assert.Equal(t, float64(2), resp["inserted"])
		assert.Equal(t, float64(0), resp["skipped"])
	})

	// =====================================================================================
	// TESTE B: Re-Importação Ida e Volta (Idempotência / Deduplicação)
	// =====================================================================================
	t.Run("Alice envia o mesmo arquivo novamente - Deduplicação", func(t *testing.T) {
		ofxPath := filepath.Join("..", "testutil", "testdata", "sample.ofx")
		file, _ := os.Open(ofxPath)
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		_ = w.WriteField("account_id", accountSharedA.ID.Hex())
		part, _ := w.CreateFormFile("file", filepath.Base(ofxPath))
		io.Copy(part, file)
		w.Close()

		req := httptest.NewRequest("POST", "/transactions/import", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+tokenUserA)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		
		// Como já estavam no banco da familia A, ele checa fitid+account+family_id e skippa todos.
		var resp map[string]interface{}
		json.NewDecoder(rr.Body).Decode(&resp)
		assert.Equal(t, float64(0), resp["inserted"])
		assert.Equal(t, float64(2), resp["skipped"])
	})

	// =====================================================================================
	// TESTE C: Segurança de Isolamento / Acessibilidade (Família B)
	// =====================================================================================
	t.Run("Bob (Família B) lista transações - Base deve vir VAZIA pra ele", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/transactions", nil)
		// Aqui injectamos o token do Bob!
		req.Header.Set("Authorization", "Bearer "+tokenUserB)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var listResp repository.PagedResult
		json.NewDecoder(rr.Body).Decode(&listResp)

		// ALERTA: O Bob não tem transações, então mesmo com 2 na base, a Família dele retornará Vazio.
		assert.Equal(t, int64(0), listResp.Total)
		assert.Len(t, listResp.Data, 0)
	})

	// =====================================================================================
	// TESTE D: Validação Listagem Alice
	// =====================================================================================
	t.Run("Alice lista e atesta que tem 2 transações", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/transactions", nil)
		req.Header.Set("Authorization", "Bearer "+tokenUserA)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		var listResp repository.PagedResult
		json.NewDecoder(rr.Body).Decode(&listResp)

		assert.Equal(t, int64(2), listResp.Total)
		assert.Len(t, listResp.Data, 2)
		
		// Garantir que a query foi carimbada corretamente pelo handler da Alice (criado por e da família)
		assert.Equal(t, userA.ID, listResp.Data[0].CreatedBy)
		assert.Equal(t, familyA_ID, listResp.Data[0].FamilyID)
	})

	// =====================================================================================
	// TESTE E: Compartilhamento de dados na mesma Família
	// =====================================================================================
	t.Run("Charlie (Mesma família que Alice) pode ver os dados que Alice enviou", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/transactions", nil)
		// Usamos o token do Charlie, que tem o mesmo familyA_ID que a Alice
		req.Header.Set("Authorization", "Bearer "+tokenUserCharlie)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		var listResp repository.PagedResult
		json.NewDecoder(rr.Body).Decode(&listResp)

		// Ele deve ver as mesmíssimas 2 transações
		assert.Equal(t, int64(2), listResp.Total)
		assert.Len(t, listResp.Data, 2)
		
		// E atestamos que a autoria real base (CreatedBy) continua selada na Alice!
		assert.Equal(t, userA.ID, listResp.Data[0].CreatedBy)
		assert.Equal(t, familyA_ID, listResp.Data[0].FamilyID)
	})

	// =====================================================================================
	// TESTE F: Revogação (Acesso Bloqueado após Logout/Revoke)
	// =====================================================================================
	t.Run("Tentativa vazia sem Authorization é recusada", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/transactions", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("Tentativa com um Token que foi posto na Blocklist é recusada", func(t *testing.T) {
		// Adicionando o Token de Alice na Blocklist "na mão" para simular que ela deu Logout no Handler
		tokenHash := authSvc.HashToken(tokenUserA)
		_ = repos.Blocklist.Add(context.Background(), tokenHash, time.Now().Add(1*time.Hour))

		req := httptest.NewRequest("GET", "/transactions", nil)
		req.Header.Set("Authorization", "Bearer "+tokenUserA) // Usando Token que foi "expulso"

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		
		// O Middleware intercepta!
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "token has been revoked")
	})
}
