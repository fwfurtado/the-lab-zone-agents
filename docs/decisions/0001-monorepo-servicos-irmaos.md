---
tipo: adr
numero: 1
titulo: Monorepo de agentes com serviços irmãos sob services/
status: aceito
relacionado: [the-lab-zone/docs/decisions/0017-triagem-por-alerta-borda-go-sidecar]
---

# ADR-0001 — Monorepo de agentes: serviços irmãos sob `services/`

## Status
Aceito. Estrutura do repositório `the-lab-zone-agents`.

## Contexto
O repositório nasceu como um único projeto Python (o QA bot do Slack), com
`agents/`, `shared/`, `pyproject.toml` e `Dockerfile` na raiz. Quando a Fase B
da triagem (ver ADR-0002) adicionou uma borda em Go, ela foi parar num
subdiretório `services/triage-webhook/`, criando uma assimetria: o projeto
Python ocupava a raiz como se fosse *o* repositório, e o Go era um exilado.

A raiz misturava dois papéis — configuração do monorepo e código de um serviço
específico. A pergunta que forçou a decisão: quando um terceiro serviço
chegar, onde ele vai?

## Decisão
Todos os serviços vivem em `services/<nome>/`, irmãos. A raiz guarda apenas
coordenação (Justfile orquestrador, `mise.toml`, README, workflows de CI).

- `services/core/` — núcleo de agentes em Python (QA bot + servidor de
  triagem). Era a raiz; movido via `git mv` (histórico preservado).
- `services/triage-webhook/` — borda de triagem em Go.

Os imports internos do Python (`from shared...`, `from agents...`) foram
**mantidos intactos**: `shared/` e `agents/` continuam top-level *dentro de*
`services/core/`, com o build rodando a partir dali. Renomear para um namespace
tipo `the_lab_zone.shared` seria over-engineering para um serviço que é
buildado em container, não publicado no PyPI.

Cada serviço expõe o **mesmo contrato** de tarefas via `just`: `test`, `lint`,
`fmt`. O Justfile da raiz orquestra (agregados que rodam em todos + granulares
por serviço). O contrato uniforme é o que permite ao CI ser burro: ele chama
`just lint`/`just test` sem saber a linguagem de cada serviço (ver ADR-0005).

## Consequências
- Adicionar um serviço = criar `services/<novo>/` com seu `Justfile`
  (test/lint/fmt) + uma entrada no filtro de path do CI. Nenhuma lógica de CI
  muda.
- O nome do diretório do serviço e o nome da imagem no registry não precisam
  coincidir — o mapeamento vive no workflow.
- A raiz deixa de ser "o serviço Python" e passa a ser "o monorepo". O README
  raiz, que ainda descrevia só o bot, ficou defasado (dívida registrada).
