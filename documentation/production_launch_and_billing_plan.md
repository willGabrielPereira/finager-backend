# Plano de Lançamento em Produção, Monetização e Cobrança — Finager

Este documento reúne todas as diretrizes técnicas, operacionais, fiscais, jurídicas e de infraestrutura para a publicação do **Finager** em ambiente de produção com suporte a planos e cobranças recorrentes no Brasil.

Salve este arquivo como referência permanente e ticket mestre para o lançamento do produto.

---

## Índice
1. [Visão Geral & Fases de Lançamento](#1-visão-geral--fases-de-lançamento)
2. [Estratégia de Testes de Cobrança Sem Gateway Ativo (Mock Provider)](#2-estratégia-de-testes-de-cobrança-sem-gateway-ativo-mock-provider)
3. [Modelagem de Planos, Quotas e Cupons](#3-modelagem-de-planos-quotas-e-cupons)
4. [Checklist Fiscal e Empresarial no Brasil (CNPJ, Simples, NFS-e)](#4-checklist-fiscal-e-empresarial-no-brasil-cnpj-simples-nfs-e)
5. [Checklist Jurídico e Regulatório (LGPD, CDC e Termos)](#5-checklist-jurídico-e-regulatório-lgpd-cdc-e-termos)
6. [Infraestrutura de Produção, Segurança e Backups](#6-infraestrutura-de-produção-segurança-e-backups)
7. [Guia de Migração para Gateways Reais (Asaas / Stripe)](#7-guia-de-migração-para-gateways-reais-asaas--stripe)
8. [Checklist Final de Go-To-Market](#8-checklist-final-de-go-to-market)

---

## 1. Visão Geral & Fases de Lançamento

Para minimizar riscos operacionais e custos antes de faturar, o lançamento deve ser dividido em 4 fases:

```
[Fase 1: Alpha Fechado] ──> [Fase 2: Beta Privado] ──> [Fase 3: Soft Launch] ──> [Fase 4: B2C Público]
 (Amigos / Lifetime Free)     (Onboarding + Quotas)      (CNPJ + Gateway Test)      (Tráfego & Vendas)
```

| Fase | Descrição | Status de Cobrança | Empresa / CNPJ |
|---|---|---|---|
| **1. Alpha Fechado** (Atual) | Testes funcionais com amigos próximos no backend Go + frontend Vue. | Desativada / Cupons `LIFETIME_FREE`. | Não necessário (Pessoa Física). |
| **2. Beta Privado** | Validação do Onboarding interativo, Guia de OFX e visualização de planos/limites com Mock Provider. | Mock Provider ativo (simulação de upgrade/downgrade). | Não necessário. |
| **3. Soft Launch** | Convite a 50-100 usuários externos reais. Validação de conversão e feedback de uso. | Gateway real em modo Sandbox/Test (ex: Asaas Sandbox). | Abertura do CNPJ SLU em andamento. |
| **4. Lançamento B2C** | Divulgação pública (Instagram, YouTube, indicação) com checkout ativo. | Cobrança real ativa (PIX Recorrente e Cartão). | CNPJ ativo, Simples Nacional, NFS-e automática. |

---

## 2. Estratégia de Testes de Cobrança Sem Gateway Ativo (Mock Provider)

Como você ainda não possui conta em gateway de pagamento (nem CNPJ aberto), a arquitetura do Finager implementa uma **camada de abstração de cobrança** (`BillingProvider`).

### Como funciona a abstração:
```
                 ┌────────────────────────────────┐
                 │    BillingService (Finager)    │
                 └───────────────┬────────────────┘
                                 │
                 ┌───────────────┴────────────────┐
                 │   interface BillingProvider    │
                 └───────┬────────────────┬───────┘
                         │                │
            ┌────────────┴───┐       ┌────┴────────────┐
            │  MockProvider  │       │  AsaasProvider  │
            │ (Ambiente Dev) │       │ (Ambiente Prod) │
            └────────────────┘       └─────────────────┘
```

### Funcionalidades do Mock Provider (Disponíveis Imediatamente):
1. **Endpoint `GET /billing/plan`:** Retorna o plano atual da família, limites e uso de recursos em tempo real (ex: contas criadas 1/2, membros 1/2, dias de histórico).
2. **Endpoint `POST /billing/simulate-upgrade`:** Permite alternar instantaneamente a família para o plano `PRO`, simulando a confirmação de um webhook de pagamento.
3. **Endpoint `POST /billing/simulate-downgrade`:** Simula o cancelamento da assinatura e retorno ao plano `FREE`, permitindo testar se as travas de limite voltam a funcionar.
4. **Endpoint `POST /billing/coupons/apply`:** Permite aplicar cupons promocionais ou códigos como `AMIGO100` (que concede o plano `LIFETIME_FREE` perpétuo).

Isso garante que **todo o frontend Vue pode ser construído, estilizado e testado ponta a ponta hoje**, sem gastar nada e sem depender de aprovações externas.

---

## 3. Modelagem de Planos, Quotas e Cupons

### Regras dos Planos

| Recurso | Plano FREE | Plano PRO (Assinatura) | Plano LIFETIME_FREE (Amigos/Staff) |
|---|---|---|---|
| **Valor** | R$ 0,00 | A definir após benchmarking | R$ 0,00 perpétuo |
| **Membros por Família** | Até 2 membros | Até 8 membros | Até 8 membros |
| **Contas Bancárias** | Até 2 contas | Ilimitadas | Ilimitadas |
| **Histórico de Transações** | Últimos 90 dias | Ilimitado (enquanto ativo) | Ilimitado |
| **Relatórios Básicos** | Liberados | Liberados | Liberados |
| **Relatórios Comparativos** | Liberados (< 90 dias) | Liberados (multi-anual) | Liberados |
| **Classificador Naive Bayes** | Sem limites | Sem limites | Sem limites |

### Política de Proteção e Histórico
* **Soft Lock (90 dias):** No plano Free, transações com mais de 90 dias permanecem no banco, mas a API oculta nas listagens gerais.
* **Expurgo Automático (Prevenção de Banco Inchado):** Contas Free sem nenhum acesso por **12 meses consecutivos** recebem notificação de aviso de 30 dias; após esse período, transações antigas (> 365 dias) são limpas por rotina de manutenção.
* **Grandfathering:** Assinantes garantem o preço de entrada. Reajustes futuros na plataforma aplicam-se apenas a novas assinaturas.

---

## 4. Checklist Fiscal e Empresarial no Brasil (CNPJ, Simples, NFS-e)

Quando decidir iniciar as cobranças públicas reais, siga este roteiro burocrático:

### Passo 1: Abrir Sociedade Limitada Unipessoal (SLU)
* **Por que NÃO MEI?** Desenvolvimento de software / SaaS é atividade intelectual e é expressamente vedada no MEI pela Lei Complementar 123/2006.
* **Por que SLU?** Permite ser sócio único (100% do capital), sem necessidade de sócios terceiros e com blindagem do patrimônio pessoal contra dívidas empresariais.
* **Custo médio de abertura:** R$ 300 a R$ 600 (taxas da Junta Comercial estadual). Muitas contabilidades online (Contabilizei, Agilize) cobram taxa zero de serviço para abrir.

### Passo 2: Enquadramento e CNAE
* **Regime:** **Simples Nacional**.
* **CNAE Principal:** `6203-1/00` — *Desenvolvimento e licenciamento de programas de computador não-customizáveis (SaaS)*.
* **CNAE Secundário recomendado:** `6311-9/00` — *Tratamento de dados, provedores de serviços de aplicação e serviços de hospedagem na internet*.
* **Otimização Tributária (Fator R):**
  * Se a empresa retirar um pró-labore de no mínimo 28% do faturamento, ela é tributada pelo **Anexo III** do Simples Nacional com alíquota inicial de apenas **6%** sobre o faturamento bruto (em vez dos 15,5% do Anexo V).

### Passo 3: Inscrição Municipal e Certificado Digital
* **Inscrição Municipal:** Emitida pela Prefeitura da sua cidade para prestação de serviços.
* **Certificado Digital e-CNPJ (Tipo A1):** Arquivo digital `.pfx` com validade de 1 ano (custa ~R$ 150 a R$ 200). Ele é obrigatório para que a plataforma de cobrança emita as Notas Fiscais de Serviço (NFS-e) automaticamente.

### Passo 4: Emissão de Nota Fiscal Automática
* No **Asaas**, basta fazer o upload do Certificado Digital A1 no painel e preencher a configuração municipal.
* A cada mensalidade paga pelo usuário, o Asaas emite a NFS-e automaticamente na prefeitura e envia o link da nota por e-mail para o assinante.

---

## 5. Checklist Jurídico e Regulatório (LGPD, CDC e Termos)

Antes de receber dados e pagamentos de terceiros:

- [ ] **Direito à Exclusão (Art. 18 LGPD):** Funcionalidade `DELETE /me` operando na API e no frontend com exclusão total de dados e cancelamento de sessões.
- [ ] **Termos de Uso:**
  - [ ] Cláusula de Isenção Financeira: O Finager é uma plataforma de organização e visualização pessoal, sem vínculo ou aconselhamento de investimentos (CVM).
  - [ ] Cláusula de Responsabilidade de Dados Bancários: O sistema lê o que constar no arquivo OFX importado pelo usuário, sem acesso direto ou movimentação da conta bancária.
  - [ ] Regra de Cancelamento e Reembolso: Cancelamento em 1 clique e estorno integral garantido se solicitado em até **7 dias corridos** da primeira cobrança (Art. 49 do Código de Defesa do Consumidor).
- [ ] **Política de Privacidade:**
  - [ ] Finalidade única do processamento do extrato OFX (geração de gráficos familiares).
  - [ ] Garantia de não comercialização de dados pessoais ou financeiros a corretores ou anunciantes.
  - [ ] Indicação de e-mail de contato para solicitações de privacidade.
- [ ] **Logs de Acesso (Marco Civil da Internet):** Guarda de logs de requisição HTTP (IP, data/hora e identificador) por no mínimo 6 meses no servidor.

---

## 6. Infraestrutura de Produção, Segurança e Backups

### 6.1. Requisitos do Servidor
* **VPS recomendada para início:** Hetzner Cloud (CX22 / CPX11), DigitalOcean Droplet (2GB RAM / 1 vCPU) ou AWS Lightsail (~$5 a $10/mês).
* **Stack:** Docker Compose gerenciando:
  * Container 1: `finager-api` (Go binário compilado, leve, consome ~30-50MB RAM).
  * Container 2: `postgres:16-alpine` (Banco de dados com volume persistente).
  * Container 3: `caddy` ou `nginx` com Let's Encrypt automático (Reverse Proxy e SSL/TLS).

### 6.2. Estratégia de Backups do Banco (Crítico para App Financeiro)
A perda de dados em um gerenciador financeiro destrói a confiança do usuário. Crie um script cron diário:
```bash
#!/bin/bash
# Backup diário comprimido do PostgreSQL e envio para S3 / Cloudflare R2
DATE=$(date +%Y%m%d_%H%M%S)
docker exec finager-postgres pg_dump -U postgres finager | gzip > /backups/finager_$DATE.sql.gz
# Manter últimos 30 dias localmente
find /backups -name "finager_*.sql.gz" -mtime +30 -delete
# Sincronizar com bucket remoto seguro
rclone copy /backups/finager_$DATE.sql.gz r2:finager-backups/
```

### 6.3. Checklist de Variáveis de Ambiente em Produção
* `ENVIRONMENT=production`
* `PORT=8080`
* `DATABASE_DSN=postgresql://usuario:senha_ultra_segura@postgres:5432/finager?sslmode=disable`
* `JWT_SECRET=chave_aleatoria_com_64_caracteres_hexadecimal`
* `JWT_EXPIRES_HOURS=24`
* `JWT_REFRESH_EXPIRES_HOURS=720` (30 dias)
* `CORS_ALLOWED_ORIGINS=https://app.finager.com.br`
* `BILLING_PROVIDER=mock` (mudar para `asaas` ao lançar)

---

## 7. Guia de Migração para Gateways Reais (Asaas / Stripe)

Quando o CNPJ estiver emitido e a conta aprovada:

### Configuração com Asaas (Recomendado no Brasil):
1. Crie uma conta no [Asaas](https://www.asaas.com/) com seu CNPJ.
2. Gere a **Chave de API** (Token de Acesso) nas configurações.
3. No Finager, altere a variável de ambiente:
   ```env
   BILLING_PROVIDER=asaas
   ASAAS_API_KEY=$aact_sua_chave_aqui
   ASAAS_ENV=sandbox # ou production
   ASAAS_WEBHOOK_SECRET=segredo_do_webhook
   ```
4. No painel do Asaas, aponte a URL do Webhook para:
   `https://api.finager.com.br/billing/webhook`
5. Eventos mapeados no Webhook:
   * `PAYMENT_RECEIVED` ou `PAYMENT_CONFIRMED`: Ativa ou renova o plano `PRO` da família.
   * `PAYMENT_OVERDUE`: Marca o status como `PAST_DUE` (inadimplente), dando carência de 3 dias antes do bloqueio.
   * `SUBSCRIPTION_CANCELED` ou `SUBSCRIPTION_DELETED`: Converte a família de volta ao plano `FREE`.

---

## 8. Checklist Final de Go-To-Market

- [x] Arquitetura de Planos e Quotas implementada no Backend
- [x] Endpoint de Exclusão de Conta (LGPD `DELETE /me`) funcionando
- [x] Endpoints de Onboarding ativos
- [x] Mock Provider de Cobrança configurado para testes de interface
- [ ] Validação do Onboarding e Guia OFX no frontend Vue com os usuários beta
- [ ] Estudo de mercado de preços concluído e valor final aprovado
- [ ] Páginas de Termos de Uso e Política de Privacidade publicadas no frontend
- [ ] Abertura de SLU no Simples Nacional e emissão do Certificado Digital A1
- [ ] Ativação do gateway real (Asaas) e teste de 1 pagamento real com PIX e Cartão
- [ ] Divulgação inicial e início da aquisição de clientes
