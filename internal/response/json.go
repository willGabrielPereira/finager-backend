package response

import (
	"encoding/json"
	"net/http"
)

// ValidationError mapeia falhas de validação de forma abstrata (Inspired by Vine.js).
// A chave `rule` (ex: "required", "uuid", "min_length") permite ao frontend 
// usar i18n (internacionalização) para traduzir ou engatilhar tratamentos específicos sem depender de parse de texto.
type ValidationError struct {
	Message string `json:"message"`
	Rule    string `json:"rule"`
	Field   string `json:"field"`
}

// ValidationResponses encapsula as validações 
type ValidationResponse struct {
	Errors []ValidationError `json:"errors"`
}

// JSON emite retornos puros para Respostas OK
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// Validations emite Status 422 (Unprocessable) para payloads, entregando o Array de Errors purista
func Validations(w http.ResponseWriter, errors ...ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(ValidationResponse{
		Errors: errors,
	})
}

// Error envia falhas globais não ligadas a campos/formulários (ex: Falha de Rede, Token expirado, etc)
func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code, 
			"message": message,
		},
	})
}
