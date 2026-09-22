# Classificador de Transações e Tagueamento Automático — Finager

Este documento descreve o motor de classificação e tagueamento inteligente de transações do Finager, composto por duas camadas complementares e mecanismos de proteção contra contaminação de dados.

---

## 1. Arquitetura em Duas Camadas

Ao importar extratos OFX (`POST /transactions/import`) ou disparar o auto-tagueamento em lote (`POST /transactions/ai-auto-tag`), o sistema executa o seguinte fluxo:

```
                  ┌──────────────────────────────┐
                  │      Transação Recebida      │
                  │       (Nome / Memo OFX)      │
                  └──────────────┬───────────────┘
                                 │
                    [Camada 1: Merchant Memory]
                                 │
                    Houve match exato de padrão?
                     /                         \
                   SIM                         NÃO
                   /                             \
        ┌───────────────────────┐   [Camada 2: Naive Bayes]
        │ Aplica Tag (100% conf)│                 │
        └───────────────────────┘     Confiança >= Limiar?
                                       /               \
                                     SIM               NÃO
                                     /                   \
                          ┌─────────────────────┐   ┌───────────────┐
                          │ Aplica Tag sugerida │   │ Não tagueia   │
                          └─────────────────────┘   └───────────────┘
```

### Camada 1: Memória Determinística de Estabelecimentos (`Merchant Memory`)
- **Tabela:** `merchant_mappings` (escopada por `family_id`).
- **Funcionamento:** Quando o usuário edita ou associa manualmente uma tag a uma transação com padrão repetitivo, ou cria uma regra via `POST /merchant-rules`, esse padrão é gravado.
- **Confiança:** 100% determinística. Prevalece sempre sobre o modelo probabilístico.

### Camada 2: Classificador Probabilístico (Naive Bayes)
- **Pacote:** `internal/classifier`.
- **Funcionamento:** Tokeniza o texto sanitizado (removendo pontuações, stop words e números irrelevantes) e calcula a probabilidade *a posteriori* de cada tag disponível.
- **Persistência de Estado:** O vocabulário e as frequências de cada família são persistidos serializados em JSONB na tabela `classifier_states`.
- **Cold Start:** Famílias novas iniciam com um modelo pré-treinado baseado no `classifier.DefaultTrainingData`, cobrindo tags universais de sistema (Alimentação, Mercado, Transporte, Compras, etc.).

---

## 2. Princípio Anti-Poisoning (Segurança de Aprendizado)

Para evitar que erros de classificação ou tags com nomes incorretos de uma família afetem outros usuários:

1. **Isolamento Total:** Cada família possui seu próprio registro em `classifier_states`. Os pesos calculados para a Família A jamais alteram o modelo da Família B.
2. **Treinamento Global Protegido:** Se o classificador global do sistema for retreinado via `classifier.TrainForFamily(ctx, nil, ...)`, ele utiliza **estritamente** transações associadas a tags de sistema (`is_system = true`), impedindo que ruídos de tags particulares corrompam o modelo padrão.

---

## 3. Endpoints da IA de Classificação

- `POST /transactions/{id}/suggest-tags`: Retorna tags sugeridas com score de probabilidade para uma transação específica.
- `POST /transactions/{id}/apply-similar`: Aplica a mesma tag a transações pendentes que compartilham descrição similar.
- `POST /transactions/ai-auto-tag`: Executa a varredura e tagueamento automático em lote para transações sem tag da família.
- `GET /merchant-rules`: Lista as regras de memória de estabelecimentos da família.
- `POST /merchant-rules`: Cria uma regra determinística manual (`pattern` -> `tag_id`).
- `DELETE /merchant-rules/{id}`: Remove uma regra determinística existente.
