package migrations

// register.go — mecanismo de auto-descoberta de migrations via init().
//
// Como funciona em Go:
//   Diferente de linguagens interpretadas (PHP, Python), Go não pode carregar
//   arquivos .go em runtime — tudo é compilado estáticamente. A solução
//   idiomática é o padrão "init() registration": cada migration chama
//   Register() no seu init(), que é executado automaticamente quando
//   o pacote é importado, sem nenhum arquivo de lista manual.
//
// Para criar uma nova migration, basta:
//   1. Criar o arquivo  internal/migrations/NNN_descricao.go
//   2. Implementar a interface Migration
//   3. Adicionar ao final do arquivo:
//
//        func init() { Register(&MNNNDescricao{}) }
//
//   Pronto. Nenhum arquivo de registro precisa ser editado.

import "sort"

var registered []Migration

// Register adiciona uma Migration ao registry global.
// Não é thread-safe — projetado exclusivamente para uso em init().
func Register(m Migration) {
	registered = append(registered, m)
}

// All retorna todas as migrations registradas, ordenadas por ID crescente.
// A ordenação garante a ordem correta independente da ordem de execução dos init().
func All() []Migration {
	out := make([]Migration, len(registered))
	copy(out, registered)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}
