# core — núcleo de agentes (Python)

Núcleo read-only de agentes do the-lab-zone: o QA bot do Slack e o servidor de
triagem, ambos sobre o vMCP/ToolHive via LiteLLM, construídos com Pydantic AI.

Este é um dos serviços do monorepo `the-lab-zone-agents`. A borda Go da triagem
vive em `../triage-webhook/` e conversa com o servidor de triagem daqui via
HTTP local (sidecar — ver ADR-0017 no `the-lab-zone`).

## Entrypoints (console scripts)

- `qa-bot` — bot Slack (long-running; Socket Mode).
- `triage` — CLI de triagem one-shot (contexto por arg/stdin).
- `triage-serve` — servidor HTTP de triagem (consumido pela borda Go).

## Desenvolvimento

As tarefas seguem o contrato comum do monorepo, via `just` (na raiz ou aqui):

```
just test   # pytest
just lint   # ruff check + mypy (falha o pipeline em qualquer problema)
just fmt    # ruff format (corrige no lugar)
```
