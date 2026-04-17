package validator

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

var validate *validator.Validate

func init() {
	validate = validator.New()

	// Intercepta as structs para extrair o nome fiel usado nas Tags `json` no frontend
	// Assim "FamilyName" em Go vira fielmente "family_name" no response abstato.
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return fld.Name
		}
		return name
	})
}

// Struct varre um DTO providenciado pelas rotas e devolve null se OK,
// ou uma array robusta moldada ao estilo VineJS caso as regras (Tags) tenham falhado.
func Struct(data interface{}) []response.ValidationError {
	err := validate.Struct(data)
	if err == nil {
		return nil
	}

	var errors []response.ValidationError

	if validationErrs, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrs {
			// Construindo um fallback customizado limpo, ou enviando a regra bruta pra Client-Side traduzir
			msg := "Falha de validação na regra: " + e.Tag()
			
			// Podemos incluir um mini dicionário para as coisas cruciais pra já devolver traduzido se não bater no front
			switch e.Tag() {
			case "required":
				msg = "Este campo é de preenchimento obrigatório"
			case "min":
				msg = "O valor inserido é insuficiente ou mais curto que o mínimo aceitável (" + e.Param() + ")"
			case "max":
				msg = "O valor inserido excede o limite máximo permitido (" + e.Param() + ")"
			case "hexcolor":
				msg = "Cor tem que estar no padrão hexadecimal (Ex: #FF00AA)"
			}

			errors = append(errors, response.ValidationError{
				Field:   e.Field(),
				Rule:    e.Tag(),
				Message: msg,
			})
		}
		return errors
	}

	// Caso não seja um erro mapeável normal em regras de campo (Exemplo: Struct corrompida)
	return []response.ValidationError{
		{Field: "body", Rule: "malformed", Message: err.Error()},
	}
}
