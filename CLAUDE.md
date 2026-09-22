# Claude Code Instructions — Finager API

Este projeto adota o padrão `AGENTS.md` como fonte canônica de instruções, invariantes arquiteturais e regras de contexto.

Siga rigorosamente as diretrizes em:
- [AGENTS.md](./AGENTS.md)

## Guia Rápido de Comandos

```bash
make docs        # Obrigatório após alterar handlers/Swagger
make run         # Inicia API local
make test        # Executa suíte de testes (requer Docker)
make migrate     # Roda migrations
```

## Navegação Eficiente de Contexto

Para manter o consumo de tokens baixo e maximizar o prompt caching:
- Consulte a seção **Contexto Especializado Sob Demanda** em `AGENTS.md` para saber qual arquivo de `documentation/` carregar quando necessário.
- Use buscas pontuais via grep antes de inspecionar arquivos.
- Verifique `cmd/api/routes.go` para mapear rotas existentes.
