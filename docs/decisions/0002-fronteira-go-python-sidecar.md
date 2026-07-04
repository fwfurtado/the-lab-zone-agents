---
tipo: adr
numero: 2
titulo: Fronteira Go↔Python da triagem via sidecar HTTP em localhost
status: aceito
relacionado: [the-lab-zone/docs/decisions/0017-triagem-por-alerta-borda-go-sidecar, 0001-monorepo-servicos-irmaos]
---

# ADR-0002 — Fronteira Go↔Python: sidecar HTTP em localhost

## Status
Aceito. A decisão-mãe é o **ADR-0017 do repositório `the-lab-zone`**
(infraestrutura); este ADR registra a materialização dela no código deste
repositório.

## Contexto
A Fase B do agente de triagem liga o Alertmanager ao núcleo Python: alerta →
webhook → triagem automática. A borda (dedup, fila, backpressure, retry) é um
domínio onde Go é a ferramenta certa (binário estático, concorrência barata,
custo de memória irrisório); o núcleo é o mesmo agente Pydantic AI que serve o
QA bot, e guarda o `Assistant` singleton + a sessão MCP num processo longevo.

## Decisão
Sidecar: um pod com dois containers.

- `services/triage-webhook/` (Go) — recebe o Alertmanager em `:8080`, métricas
  em `:9090`. Deduplica (janela TTL in-memory), enfileira com backpressure,
  aciona o núcleo.
- `services/core/` (Python) — o console script `triage-serve` sobe um servidor
  aiohttp em `127.0.0.1:8081` (`POST /triage`), métricas em `:9091`.

O núcleo escuta em **loopback**: não tem Service nem CNP no cluster; só a borda
o alcança. Contrato síncrono `POST /triage {"context"} → 200 {"report"}`.

A escolha completa (por que sidecar e não dois Deployments ou subprocess, o
contrato de drenagem no shutdown, o versionamento atômico) está detalhada no
ADR-0017 do `the-lab-zone` e não é repetida aqui.

## Consequências
- O `services/core/` ganhou um terceiro entrypoint (`triage-serve`) além de
  `qa-bot` e `triage` (CLI). Mesma imagem, três formas de rodar.
- `aiohttp` já era dependência direta do monorepo — o servidor HTTP tem delta
  zero de dependências.
- Se surgir um segundo consumidor do núcleo, a migração para dois Deployments
  com Service é mecânica (mudar o bind, criar Service + CNP); o contrato HTTP
  não muda.
