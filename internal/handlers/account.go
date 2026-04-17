package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

type AccountHandler struct {
	accRepo *repository.AccountRepository
}

func NewAccountHandler(accRepo *repository.AccountRepository) *AccountHandler {
	return &AccountHandler{accRepo: accRepo}
}

// createAccountRequest DTO
type createAccountRequest struct {
	Name         string   `json:"name"          validate:"required,min=2"`
	Institution  string   `json:"institution"   validate:"required"`
	AllowedUsers []string `json:"allowed_users" validate:"omitempty"`
}

// updateAccountRequest DTO
type updateAccountRequest struct {
	Name         string   `json:"name"          validate:"omitempty,min=2"`
	Institution  string   `json:"institution"   validate:"omitempty"`
	AllowedUsers []string `json:"allowed_users" validate:"omitempty"`
}

// List
// @Summary      Listar Contas
// @Description  Retorna todas as contas atreladas e visíveis que você tem privilégios de ver
// @Router       /accounts [get]
func (h *AccountHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)
	userID, _ := bson.ObjectIDFromHex(claims.UserID)

	accounts, err := h.accRepo.FindVisibleAccounts(r.Context(), familyID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao ler contas conectadas")
		return
	}

	response.JSON(w, http.StatusOK, accounts)
}

// Create
// @Summary      Criar Conta
// @Description  Adiciona uma nova conta financeira.
// @Router       /accounts [post]
func (h *AccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}

	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)
	userID, _ := bson.ObjectIDFromHex(claims.UserID)

	// Converte array de String pro padrao DB
	var dbAllowedUsers []bson.ObjectID
	if req.AllowedUsers != nil {
		// Validamos na raça se esses IDs existem ou são valídos
		for _, rawHash := range req.AllowedUsers {
			hash, err := bson.ObjectIDFromHex(rawHash)
			if err != nil {
				response.Validations(w, response.ValidationError{Field: "allowed_users", Rule: "objectid", Message: "Formato corrompido de id na Array."})
				return
			}
			dbAllowedUsers = append(dbAllowedUsers, hash)
		}
	} else {
		dbAllowedUsers = []bson.ObjectID{} 
	}

	account := &models.Account{
		Name:         req.Name,
		Institution:  req.Institution,
		FamilyID:     familyID,
		CreatedBy:    userID,
		AllowedUsers: dbAllowedUsers,
	}

	if err := h.accRepo.Create(r.Context(), account); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao registrar conta bacária no Database")
		return
	}

	response.JSON(w, http.StatusCreated, account)
}

// Update
// @Summary      Atualizar Conta Bancária e Permissões
// @Router       /accounts/{id} [put]
func (h *AccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)
	userID, _ := bson.ObjectIDFromHex(claims.UserID)
	
	accIDHex := r.PathValue("id")
	accID, err := bson.ObjectIDFromHex(accIDHex)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "O ID da conta informado via URL é inoperável")
		return
	}

	acc, err := h.accRepo.FindByID(r.Context(), accID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "A conta solicitada para edição não foi confirmada no disco")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha")
		return
	}

	// BLINDAGEM DE RLS
	if acc.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Essa conta pertence a outra família.")
		return
	}
	
	// Apenas o Crador original da conta (ou os visíveis, depedendo da regra de negocios). 
	// Para edição de contas financeiras restritas: Só os membros permitidos podem editar
	isAllowedToEdit := false
	if len(acc.AllowedUsers) == 0 {
		isAllowedToEdit = true
	} else {
		for _, aUID := range acc.AllowedUsers {
			if aUID == userID {
				isAllowedToEdit = true
			}
		}
	}
	if !isAllowedToEdit {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Você não está autorizado a modificar informações ou listas de privacidade dessa Entidade")
		return
	}

	var req updateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	updates := bson.M{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Institution != "" {
		updates["institution"] = req.Institution
	}
	if req.AllowedUsers != nil { // Slice foi informada? Significa que querem trocar as regras de privacidade
		var parsedReqAllowed []bson.ObjectID
		for _, rawHash := range req.AllowedUsers {
			hash, err := bson.ObjectIDFromHex(rawHash)
			if err != nil {
				response.Validations(w, response.ValidationError{Field: "allowed_users", Rule: "objectid", Message: "Um dos IDs de acessibilidade da conta está ilegível ou quebrado"})
				return
			}
			parsedReqAllowed = append(parsedReqAllowed, hash)
		}
		updates["allowed_users"] = parsedReqAllowed
	}

	if len(updates) > 0 {
		if err := h.accRepo.Update(r.Context(), accID, updates); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Houve uma falha ao modificar a Conta Bancária no BD")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete
// @Summary      Deletar Conta Bancária
// @Router       /accounts/{id} [delete]
func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)
	userID, _ := bson.ObjectIDFromHex(claims.UserID)
	
	accIDHex := r.PathValue("id")
	accID, err := bson.ObjectIDFromHex(accIDHex)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "O ID da conta informado via URL é inoperável")
		return
	}

	acc, err := h.accRepo.FindByID(r.Context(), accID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "A conta solicitada não existe.")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Ocorreu um erro interno")
		return
	}

	// BLINDAGEM RLS
	if acc.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Essa conta pertence a outra família.")
		return
	}
	// Apenas o dono
	if acc.CreatedBy != userID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Somente o Criador fundamental da Conta Bancária é detentor do poder de deletá-la da plataforma.")
		return
	}

	if err := h.accRepo.Delete(r.Context(), accID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha de execução")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
