package classifier_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/classifier"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "IFOOD *RESTAURANTE COM DUAS PALAVRAS",
			expected: []string{"ifood", "restaurante", "duas", "palavras"},
		},
		{
			input:    "Posto de Gasolina com PIX",
			expected: []string{"posto", "gasolina"}, // 'de', 'com', 'pix' são stop-words
		},
		{
			input:    "A, B, C! UBER* TRIP",
			expected: []string{"uber", "trip"}, // remove pontuação e letras avulsas
		},
	}

	for _, tt := range tests {
		got := classifier.Tokenize(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("Tokenize(%q) len = %d, want %d (got: %v, want: %v)", tt.input, len(got), len(tt.expected), got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("Tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestClassifier(t *testing.T) {
	c := classifier.New()

	idAlimentacao := uuid.New()
	idTransporte := uuid.New()
	idSaude := uuid.New()

	// Treinamento básico
	c.Train("ifood restaurante lanche hamburgueria", idAlimentacao)
	c.Train("mcdonalds big mac batata", idAlimentacao)
	c.Train("uber trip corrida uberx 99taxi", idTransporte)
	c.Train("posto shell combustivel gasolina alcool", idTransporte)
	c.Train("droga raia farmacia remedio aspirina", idSaude)

	tests := []struct {
		input    string
		expected uuid.UUID
		hasMatch bool
	}{
		{"IFOOD ENTREGA DE LANCHE", idAlimentacao, true},
		{"UBER CORRIDA PARA TRABALHO", idTransporte, true},
		{"COMPRA NA FARMACIA DROGASIL", idSaude, true},
		{"QUALQUER COISA ALEATORIA", uuid.Nil, false}, // Não deve coincidir de forma confiável
	}

	for _, tt := range tests {
		got := c.Classify(tt.input)
		if !tt.hasMatch {
			if len(got) > 0 {
				t.Errorf("Classify(%q) expected no match, got %v", tt.input, got[0].String())
			}
			continue
		}

		if len(got) == 0 {
			t.Errorf("Classify(%q) expected match, got none", tt.input)
			continue
		}

		if got[0] != tt.expected {
			t.Errorf("Classify(%q) = %v, want %v", tt.input, got[0].String(), tt.expected.String())
		}
	}
}

func TestClassifier_Empty(t *testing.T) {
	c := classifier.New()
	got := c.Classify("qualquer coisa")
	if len(got) != 0 {
		t.Errorf("Empty classifier should return nil, got %v", got)
	}
}
