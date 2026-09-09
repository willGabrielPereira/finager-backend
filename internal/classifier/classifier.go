package classifier

import (
	"math"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// DefaultTrainingData representa a base estática de dados para evitar o problema de cold-start.
// Mapeia termos comuns de faturas brasileiras para nomes legíveis de tags de sistema.
var DefaultTrainingData = []struct {
	Text    string
	TagName string
}{
	// --- Compras ---
	{"amazon marketplace prime video s.a.", "Compras"},
	{"shopee br compras internet", "Compras"},
	{"mercado livre meli", "Compras"},
	{"shein shein br", "Compras"},
	{"magazine luiza magalu", "Compras"},
	{"americanas com", "Compras"},
	{"aliexpress cainiao", "Compras"},
	{"casas bahia varejo", "Compras"},
	// --- Alimentação ---
	{"ifood *restaurante alimentacao", "Alimentação"},
	{"ifood *entrega lanches", "Alimentação"},
	{"mcdonalds burger king habibs bobs", "Alimentação"},
	{"subway sanduiches", "Alimentação"},
	{"padaria panificadora confeitaria", "Alimentação"},
	{"restaurante self service almoço janta", "Alimentação"},
	// --- Mercado ---
	{"supermercado carrefour extra dia pão de açúcar", "Mercado"},
	{"hortifruti sacolao feira sacolão hortifrúti", "Mercado"},
	{"assai atacadao pague menos mercado mercadinho", "Mercado"},
	// --- Vestuário ---
	{"renner c&a cea riachuelo zara lojas", "Vestuário"},
	{"centauro decathlon hering roupas vestuario", "Vestuário"},
	{"calcados sapatos botas tennis", "Vestuário"},
	// --- Pet ---
	{"petz cobasi petshop pet shop", "Pet"},
	{"veterinario racao ração banho tosa clinica pet", "Pet"},
	// --- Transporte ---
	{"uber *trip corrida carona", "Transporte"},
	{"99app 99taxi taxi", "Transporte"},
	{"posto shell combustivel gasolina", "Transporte"},
	{"posto ipiranga petrobras br", "Transporte"},
	{"sem parar pedagio", "Transporte"},
	{"bilhete unico metro trem rodoviaria", "Transporte"},
	{"latam gol azul passagens aereas", "Transporte"},
	// --- Streamings ---
	{"netflix.com netflix assinatura", "Streamings"},
	{"spotify music stream premium", "Streamings"},
	{"youtube premium google", "Streamings"},
	{"hbo max discovery plus", "Streamings"},
	{"disney plus disney+", "Streamings"},
	{"amazon prime channels", "Streamings"},
	{"globoplay globo com", "Streamings"},
	// --- Saúde ---
	{"droga raia farmacia medicamentos", "Saúde"},
	{"drogasil farmacia", "Saúde"},
	{"pague menos ultrafarma", "Saúde"},
	{"drogaria sao paulo", "Saúde"},
	{"consulta medica unimed amil", "Saúde"},
	{"laboratorio exame clinica", "Saúde"},
	{"odontoprev dentista obturacao", "Saúde"},
	// --- Moradia ---
	{"enel energia eletrica conta luz", "Moradia"},
	{"sabesp comgas saneamento agua", "Moradia"},
	{"condominio aluguel imobiliaria", "Moradia"},
	{"material de construcao leroy merlin", "Moradia"},
	// --- Contas ---
	{"boleto cobranca pagamento fatura cartao de credito", "Contas"},
	{"telefone celular vivo tim claro net telecom internet", "Contas"},
	// --- Educação ---
	{"udemy cursos online", "Educação"},
	{"alura curso tecnologia", "Educação"},
	{"coursera faculdade universidade mensalidade", "Educação"},
	{"escola colegio matricula material escolar", "Educação"},
	// --- Salário ---
	{"ted recebido salario folha de pagamento", "Salário"},
	{"pix recebido proventos honorarios", "Salário"},
	{"pagamento salario mensal", "Salário"},
	// --- Entretenimento ---
	{"cinemark shopping bilheteria", "Entretenimento"},
	{"cinepolis cinema pipoca", "Entretenimento"},
	{"gnc cinemas ingresso com", "Entretenimento"},
	{"steam games jogos", "Entretenimento"},
	{"playstation network psn", "Entretenimento"},
	{"ingressocruz show concerto", "Entretenimento"},
}

// Classifier implementa um classificador probabilístico Naive Bayes.
type Classifier struct {
	TotalDocs       int
	ClassDocs       map[string]int            // Qtd de documentos por classe (Hex do ObjectID da Tag)
	ClassWordCounts map[string]map[string]int // Frequência de cada palavra por classe
	ClassTotalWords map[string]int            // Total de palavras por classe
	Vocabulary      map[string]struct{}       // Vocabulário único geral
}

// New cria um classificador limpo.
func New() *Classifier {
	return &Classifier{
		ClassDocs:       make(map[string]int),
		ClassWordCounts: make(map[string]map[string]int),
		ClassTotalWords: make(map[string]int),
		Vocabulary:      make(map[string]struct{}),
	}
}

// Tokenize limpa o texto, remove pontuação e filtra stop-words em português.
func Tokenize(text string) []string {
	text = strings.ToLower(text)
	var sb strings.Builder
	for _, r := range text {
		// Mantém apenas letras, números e espaços
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}

	words := strings.Fields(sb.String())
	stopWords := map[string]struct{}{
		"de": {}, "do": {}, "da": {}, "em": {}, "um": {}, "uma": {}, "o": {}, "a": {},
		"os": {}, "as": {}, "com": {}, "para": {}, "por": {}, "e": {}, "ou": {}, "se": {},
		"no": {}, "na": {}, "dos": {}, "das": {}, "comprar": {}, "compra": {},
		"pagamento": {}, "transação": {}, "ted": {}, "doc": {}, "pix": {}, "ref": {},
		"valor": {}, "estabelecimento": {},
	}

	var filtered []string
	for _, w := range words {
		if len(w) < 2 { // Descarta letras avulsas
			continue
		}
		if _, isStop := stopWords[w]; !isStop {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// Train adiciona uma transação de treinamento ao modelo para uma tag.
func (c *Classifier) Train(text string, tagID bson.ObjectID) {
	class := tagID.Hex()
	words := Tokenize(text)
	if len(words) == 0 {
		return
	}

	c.TotalDocs++
	c.ClassDocs[class]++

	if _, exists := c.ClassWordCounts[class]; !exists {
		c.ClassWordCounts[class] = make(map[string]int)
	}

	for _, w := range words {
		c.ClassWordCounts[class][w]++
		c.ClassTotalWords[class]++
		c.Vocabulary[w] = struct{}{}
	}
}

// Classify analisa o texto e retorna a tag mais provável (se houver correspondência estatística confiável).
func (c *Classifier) Classify(text string) []bson.ObjectID {
	words := Tokenize(text)
	if len(words) == 0 || c.TotalDocs == 0 || len(c.ClassDocs) == 0 {
		return nil
	}

	// Certifica que pelo menos uma das palavras existe no vocabulário geral.
	// Se for tudo palavras novas, o classificador não tem dados para sugerir.
	hasKnownWord := false
	for _, w := range words {
		if _, exists := c.Vocabulary[w]; exists {
			hasKnownWord = true
			break
		}
	}
	if !hasKnownWord {
		return nil
	}


	bestProb := -math.MaxFloat64
	var bestClass string
	hasMatch := false

	// Computa log P(C | D) = log P(C) + sum log P(w | C) para cada classe
	for class := range c.ClassDocs {
		// Prior probability P(C)
		prior := float64(c.ClassDocs[class]) / float64(c.TotalDocs)
		logProb := math.Log(prior)

		// Likelihood sum log P(w | C)
		for _, w := range words {
			count := c.ClassWordCounts[class][w]
			// Laplace smoothing (add-one smoothing)
			wordProb := float64(count+1) / float64(c.ClassTotalWords[class]+len(c.Vocabulary))
			logProb += math.Log(wordProb)
		}

		if logProb > bestProb {
			bestProb = logProb
			bestClass = class
			hasMatch = true
		}
	}

	if !hasMatch {
		return nil
	}

	id, err := bson.ObjectIDFromHex(bestClass)
	if err != nil {
		return nil
	}

	return []bson.ObjectID{id}
}

// ExportState compila e retorna o estado estatístico do classificador.
func (c *Classifier) ExportState(familyID bson.ObjectID) *models.ClassifierState {
	vocab := make([]string, 0, len(c.Vocabulary))
	for w := range c.Vocabulary {
		vocab = append(vocab, w)
	}

	return &models.ClassifierState{
		FamilyID:        familyID,
		TotalDocs:       c.TotalDocs,
		ClassDocs:       c.ClassDocs,
		ClassWordCounts: c.ClassWordCounts,
		ClassTotalWords: c.ClassTotalWords,
		Vocabulary:      vocab,
	}
}

// RestoreState reconstrói e inicializa um Classifier a partir do estado persistido.
func RestoreState(state *models.ClassifierState) *Classifier {
	vocabSet := make(map[string]struct{})
	for _, w := range state.Vocabulary {
		vocabSet[w] = struct{}{}
	}

	classDocs := state.ClassDocs
	if classDocs == nil {
		classDocs = make(map[string]int)
	}

	classWordCounts := state.ClassWordCounts
	if classWordCounts == nil {
		classWordCounts = make(map[string]map[string]int)
	}

	classTotalWords := state.ClassTotalWords
	if classTotalWords == nil {
		classTotalWords = make(map[string]int)
	}

	return &Classifier{
		TotalDocs:       state.TotalDocs,
		ClassDocs:       classDocs,
		ClassWordCounts: classWordCounts,
		ClassTotalWords: classTotalWords,
		Vocabulary:      vocabSet,
	}
}
