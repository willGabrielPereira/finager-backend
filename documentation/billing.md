# Arquitetura de Cobrança, Planos e Quotas — Finager

Este documento descreve o funcionamento do módulo de faturamento (`internal/billing`), planos, quotas de uso e como o provedor de pagamentos está desacoplado da lógica de negócio.

---

## 1. Planos e Quotas da Família

As quotas são definidas no modelo `models.PlanLimits` (`internal/models/plan.go`) e aplicadas por família (`family_id`):

| Recurso | Plano `FREE` | Plano `PRO` / `LIFETIME_FREE` |
| :--- | :--- | :--- |
| **Contas bancárias (`max_accounts`)** | Até 2 contas | Ilimitado (`-1`) |
| **Membros na família (`max_members`)** | Até 2 membros | Até 8 membros |
| **Histórico de lançamentos (`max_history_days`)** | 90 dias | Ilimitado (`0`) |
| **Acesso a recursos Pro (`is_pro`)** | `false` | `true` |

> Se o status de assinatura da família não for `ACTIVE` (ex: `PAST_DUE` ou `CANCELED`), o sistema automaticamente faz fallback para os limites do plano `FREE`.

---

## 2. Camada de Abstração do Provedor (`BillingProvider`)

Para permitir desenvolvimento e testes completos sem gateway real (ou credenciais de produção), o sistema utiliza a interface:

```go
type Provider interface {
    CreateCustomer(ctx context.Context, email, name string) (string, error)
    CreateSubscription(ctx context.Context, customerID, planID string) (*SubscriptionResult, error)
    CancelSubscription(ctx context.Context, subscriptionID string) error
}
```

- **Ambiente Atual (Mock):** A aplicação inicializa com `billing.NewMockProvider()`, que simula operações em memória e no banco local.
- **Transição para Gateways Reais (Asaas / Stripe):** Crie uma nova implementação da interface em `internal/billing/` (ex: `asaas_provider.go`) e injete-a na criação do serviço em `cmd/api/routes.go`. Os handlers de negócio não sofrem nenhuma alteração.

---

## 3. Endpoints de Billing e Simulação

Todos os endpoints requerem autenticação (`Bearer <token>`):

- `GET /billing/plan`: Retorna o plano atual da família, limites contratuais e o consumo em tempo real (`accounts_count`, `members_count`).
- `POST /billing/coupons/apply`: Aplica um cupom promocional para atualizar o plano da família (ex: cupons de teste ou `LIFETIME_FREE`).
- `POST /billing/simulate-upgrade`: Endpoint de desenvolvimento para alternar a família para o plano `PRO`.
- `POST /billing/simulate-downgrade`: Endpoint de desenvolvimento para retornar a família ao plano `FREE`.

---

## 4. Onde as Quotas São Validadas no Código

As regras de faturamento são chamadas diretamente nos handlers de domínio através do `billing.Service`:

1. **Criação de Contas:** Em `handlers.AccountHandler.Create` (`internal/handlers/account.go`), antes de persistir uma nova conta, o serviço valida se `accCount < limits.MaxAccounts`.
2. **Convite de Membros:** Em `handlers.FamilyHandler.CreateInvite` (`internal/handlers/family_handler.go`), o serviço valida se o total de membros ativos + convites pendentes não excede `limits.MaxMembers`.
