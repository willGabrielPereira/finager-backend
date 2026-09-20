# Diretrizes de Interface e Feedback Visual (Finager)

## Regra Obrigatória: Proibido Uso de Diálogos Nativos do Navegador

> [!CRITICAL]
> **NUNCA utilize `alert()`, `confirm()` ou `prompt()` nativos do navegador em nenhuma parte do frontend.**
> O navegador bloqueia o thread da interface, quebra a experiência do usuário e não respeita o design Dark OLED do Finager.

### Padrão Oficial do Projeto: `sweetalert2` via `@/utils/feedback`

Todo e qualquer feedback de ação, confirmação destrutiva ou notificação deve utilizar exclusivamente os métodos utilitários padronizados em `src/utils/feedback.ts`:

#### 1. Notificações Rápidas (Toasts)
Não bloqueiam o fluxo do usuário e somem automaticamente em 4 segundos:
```ts
import { toast } from '@/utils/feedback'

// Sucesso
toast.success('Lançamento salvo com sucesso.')

// Erro
toast.error('Erro ao processar', 'Verifique os dados informados.')

// Alerta / Aviso
toast.warning('Ação restrita', 'Esta categoria é fixa do sistema.')

// Informação
toast.info('Sincronizando extratos...')
```

#### 2. Confirmação de Ações (Modais Destrutivos ou Críticos)
Modais com backdrop blur, foco acessível e botões customizados:
```ts
import { showAlert } from '@/utils/feedback'

// Confirmação destrutiva (ex: exclusão de conta, contas bancárias, lançamentos, regras)
const confirmed = await showAlert.confirm({
  title: 'Excluir conta permanentemente?',
  text: 'Esta ação não poderá ser desfeita e excluirá seus dados conforme a LGPD.',
  confirmText: 'Sim, excluir',
  cancelText: 'Cancelar',
  isDestructive: true,
})
if (!confirmed) return

// Confirmação simples (não destrutiva, ex: logout, finalizar tour)
const confirmed = await showAlert.confirm({
  title: 'Deseja realmente sair?',
  confirmText: 'Sair da conta',
  cancelText: 'Permanecer',
  isDestructive: false,
})
if (!confirmed) return
```

#### 3. Alertas Informativos com Bloqueio de Ação
Para erros críticos ou avisos formais que exigem reconhecimento do usuário:
```ts
import { showAlert } from '@/utils/feedback'

await showAlert.error('Limite do Plano Atingido', 'Faça upgrade para adicionar mais contas.')
await showAlert.success('Tudo pronto!', 'Seus lançamentos foram conciliados.')
```
