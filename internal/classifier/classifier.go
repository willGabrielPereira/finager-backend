package classifier

import (
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// DefaultTrainingData representa a base estática de dados para evitar o problema de cold-start.
// Mapeia termos característicos e livres de ruídos ambíguos para nomes de tags de sistema.
var DefaultTrainingData = []struct {
	Text    string
	TagName string
}{
	// --- Compras ---
	{"amazon marketplace prime video", "Compras"},
	{"shopee internet", "Compras"},
	{"mercado livre meli", "Compras"},
	{"shein", "Compras"},
	{"magazine luiza magalu", "Compras"},
	{"americanas", "Compras"},
	{"aliexpress cainiao", "Compras"},
	{"casas bahia varejo", "Compras"},
	// --- Alimentação ---
	{"ifood restaurante alimentacao", "Alimentação"},
	{"ifood entrega lanches", "Alimentação"},
	{"mcdonalds burger king habibs bobs", "Alimentação"},
	{"subway sanduiches", "Alimentação"},
	{"padaria panificadora confeitaria", "Alimentação"},
	{"restaurante self service almoco janta", "Alimentação"},
	// --- Mercado ---
	{"supermercado carrefour extra pao de acucar", "Mercado"},
	{"hortifruti sacolao feira", "Mercado"},
	{"assai atacadao pague menos mercadinho", "Mercado"},
	// --- Vestuário ---
	{"renner riachuelo zara", "Vestuário"},
	{"centauro decathlon hering roupas vestuario", "Vestuário"},
	{"calcados sapatos botas tennis", "Vestuário"},
	// --- Pet ---
	{"petz cobasi petshop pet shop", "Pet"},
	{"veterinario racao banho tosa clinica pet", "Pet"},
	// --- Transporte ---
	{"uber trip corrida carona", "Transporte"},
	{"99app 99taxi taxi", "Transporte"},
	{"posto shell combustivel gasolina", "Transporte"},
	{"posto ipiranga petrobras", "Transporte"},
	{"sem parar pedagio", "Transporte"},
	{"bilhete unico metro trem rodoviaria", "Transporte"},
	{"latam gol azul passagens aereas", "Transporte"},
	// --- Streamings ---
	{"netflix assinatura", "Streamings"},
	{"spotify music stream premium", "Streamings"},
	{"youtube premium google", "Streamings"},
	{"hbo max discovery plus", "Streamings"},
	{"disney plus disney", "Streamings"},
	{"amazon prime channels", "Streamings"},
	{"globoplay globo", "Streamings"},
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
	{"boleto cobranca pagamento fatura cartao", "Contas"},
	{"telefone celular vivo tim claro net telecom internet", "Contas"},
	// --- Educação ---
	{"udemy cursos online", "Educação"},
	{"alura curso tecnologia", "Educação"},
	{"coursera faculdade universidade mensalidade", "Educação"},
	{"escola colegio matricula material escolar", "Educação"},
	// --- Salário ---
	{"salario folha de pagamento", "Salário"},
	{"proventos honorarios remuneracao", "Salário"},
	// --- Entretenimento ---
	{"cinemark shopping bilheteria", "Entretenimento"},
	{"cinepolis cinema pipoca", "Entretenimento"},
	{"gnc cinemas ingresso", "Entretenimento"},
	{"steam games jogos", "Entretenimento"},
	{"playstation network psn", "Entretenimento"},
}

// Classifier implementa um classificador probabilístico Naive Bayes.
type Classifier struct {
	TotalDocs       int
	ClassDocs       map[string]int            // Qtd de documentos por classe (Hex do UUID da Tag)
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

func stripAccents(s string) string {
	s = strings.ToLower(s)
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
	return replacer.Replace(s)
}

// CleanMerchantName remove ruídos bancários e prefixos comuns de extratos brasileiros
func CleanMerchantName(name, memo string) string {
	raw := strings.ToUpper(strings.TrimSpace(name))
	if raw == "" {
		raw = strings.ToUpper(strings.TrimSpace(memo))
	}
	raw = strings.ToUpper(stripAccents(raw))

	prefixes := []string{
		"COMPRA CARTAO DEB - ", "COMPRA CARTAO CRED - ", "COMPRA CARTAO - ",
		"COMPRA NO DEBITO - ", "COMPRA NO CREDITO - ", "COMPRA INTERNET - ",
		"COMPRA CARTAO DEB ", "COMPRA CARTAO CRED ", "COMPRA CARTAO ",
		"COMPRA NO DEBITO ", "COMPRA NO CREDITO ", "COMPRA INTERNET ",
		"PAGTO ELETRON COBRANCA ", "PAGTO ELETRON ", "PAGTO COBRANCA ",
		"PAGAMENTO DE TITULO ", "PAGAMENTO TITULO ", "PAGAMENTO ELETRONICO ",
		"TRANSFERENCIA RECEBIDA PELO PIX - ", "TRANSFERENCIA ENVIADA PELO PIX - ",
		"TRANSFERENCIA RECEBIDA PELO PIX ", "TRANSFERENCIA ENVIADA PELO PIX ",
		"PIX TRANSF ", "PIX RECEBIDO ", "PIX ENVIADO ", "PIX ",
		"TED TRANSF ", "TED RECEBIDA ", "TED ENVIADA ", "TED ",
		"DOC ", "ESTORNO ",
	}

	for _, p := range prefixes {
		if strings.HasPrefix(raw, p) {
			raw = strings.TrimPrefix(raw, p)
			break
		}
	}

	raw = strings.TrimPrefix(raw, "- ")
	raw = strings.TrimPrefix(raw, "-")

	return strings.TrimSpace(raw)
}

// Tokenize normaliza acentuação, limpa caracteres especiais e filtra stop-words e ruídos bancários.
func Tokenize(text string) []string {
	text = stripAccents(text)
	var sb strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}

	words := strings.Fields(sb.String())
	stopWords := map[string]struct{}{
		// Preposições e artigos
		"de": {}, "do": {}, "da": {}, "em": {}, "um": {}, "uma": {}, "o": {}, "a": {},
		"os": {}, "as": {}, "com": {}, "para": {}, "por": {}, "e": {}, "ou": {}, "se": {},
		"no": {}, "na": {}, "dos": {}, "das": {}, "ao": {}, "aos": {},
		// Ruídos bancários comuns
		"comprar": {}, "compra": {}, "pagamento": {}, "transacao": {}, "ted": {}, "doc": {},
		"pix": {}, "ref": {}, "valor": {}, "estabelecimento": {}, "debito": {}, "credito": {},
		"cartao": {}, "pagto": {}, "transf": {}, "transferencia": {}, "aut": {}, "agencia": {},
		"banco": {}, "estorno": {}, "tarifa": {}, "iof": {}, "terminal": {},
		// Palavras ambíguas curtas que geram falso positivo
		"dia": {}, "br": {}, "lojas": {}, "loja": {}, "ltda": {}, "me": {}, "sa": {}, "eireli": {},
	}

	var filtered []string
	for _, w := range words {
		if len(w) < 3 { // Descarta letras avulsas e termos curtíssimos
			continue
		}
		if _, isStop := stopWords[w]; !isStop {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// Train adiciona uma transação de treinamento ao modelo para uma tag.
func (c *Classifier) Train(text string, tagID uuid.UUID) {
	class := tagID.String()
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

// Classify analisa o texto e retorna a tag mais provável se houver margem de confiança estatística segura.
func (c *Classifier) Classify(text string) []uuid.UUID {
	words := Tokenize(text)
	if len(words) == 0 || c.TotalDocs == 0 || len(c.ClassDocs) == 0 {
		return nil
	}

	// Certifica que pelo menos uma palavra relevante existe no vocabulário
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
	secondBestProb := -math.MaxFloat64
	var bestClass string
	hasMatch := false

	// Computa log P(C | D) = log P(C) + sum log P(w | C) para cada classe
	for class := range c.ClassDocs {
		prior := float64(c.ClassDocs[class]) / float64(c.TotalDocs)
		logProb := math.Log(prior)

		for _, w := range words {
			count := c.ClassWordCounts[class][w]
			// Laplace smoothing (add-one smoothing)
			wordProb := float64(count+1) / float64(c.ClassTotalWords[class]+len(c.Vocabulary))
			logProb += math.Log(wordProb)
		}

		if logProb > bestProb {
			secondBestProb = bestProb
			bestProb = logProb
			bestClass = class
			hasMatch = true
		} else if logProb > secondBestProb {
			secondBestProb = logProb
		}
	}

	if !hasMatch {
		return nil
	}

	// Limiar de confiança (Confidence Threshold):
	// Se houver mais de uma classe e a melhor não superar a segunda por uma margem estatística mínima segura,
	// abstenha-se de forçar uma tag duvidosa!
	if len(c.ClassDocs) > 1 {
		const minConfidenceMargin = 0.1 // Exige margem segura sobre a 2ª colocada
		if (bestProb - secondBestProb) < minConfidenceMargin {
			return nil // Abstenção inteligente
		}
	}

	id, err := uuid.Parse(bestClass)
	if err != nil {
		return nil
	}

	return []uuid.UUID{id}
}

// ExportState compila e retorna o estado estatístico do classificador.
func (c *Classifier) ExportState(familyID *uuid.UUID) *models.ClassifierState {
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
