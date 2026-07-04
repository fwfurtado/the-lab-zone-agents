---
tipo: adr
numero: 3
titulo: Borda Go da triagem é stdlib-only
status: aceito
relacionado: [0002-fronteira-go-python-sidecar]
---

# ADR-0003 — Borda Go stdlib-only (métricas Prometheus in-tree)

## Status
Aceito. Aplica-se a `services/triage-webhook/`.

## Contexto
A borda de triagem é um serviço pequeno e bem delimitado: parse do payload do
Alertmanager, deduplicação, fila com worker pool, cliente HTTP do núcleo,
publicadores (log, Slack). Nenhuma dessas responsabilidades exige biblioteca
externa — o `net/http`, `encoding/json`, `sync` e `context` da stdlib cobrem
tudo. A única tentação seria `prometheus/client_golang` para as métricas.

## Decisão
`services/triage-webhook` é **stdlib-only** (o `go.mod` não tem `require` de
terceiros). As métricas Prometheus são implementadas in-tree
(`internal/metrics`): o formato de exposição texto (counter/gauge/histogram
cumulativo com bucket `le=+Inf`, `_sum`, `_count`) é pequeno e estável, e as
séries do serviço não têm labels dinâmicos.

## Consequências
- Binário estático mínimo, zero churn de dependências, superfície de supply
  chain irrisória. Coerente com a natureza do serviço (uma borda, não uma
  aplicação).
- Se o serviço um dia precisar de labels dinâmicos ou exemplars, a troca por
  `client_golang` é mecânica e localizada: `internal/metrics` é o único ponto
  de contato. O resto do código fala com a interface `*metrics.Set`, não com o
  formato de exposição.
- O worker pool usa `sync.WaitGroup` em vez de `errgroup`; a orquestração de
  shutdown é feita à mão com `context` e canais. Mais verboso que com
  bibliotecas, mas explícito e sem dependência.
