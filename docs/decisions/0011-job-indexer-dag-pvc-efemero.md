---
tipo: adr
numero: 11
titulo: Job do indexer como dag + PVC efêmero (não containerSet)
status: aceito
relacionado: [0010-indexer-um-codebase-dois-entrypoints]
---

# ADR-0011 — Job do indexer: dag + PVC efêmero

## Status
Aceito. Estrutura do WorkflowTemplate `triage-indexer` (Argo, no `the-lab-zone`,
`apps/ai/jobs/`). Registra por que NÃO é um containerSet.

## Contexto
O job noturno baixa os relatórios do Garage uma vez e alimenta os dois
indexadores (ADR-0010). O desenho é um fan-out: `downloader` (rclone) → `gestalt`
e `facets` em paralelo. Duas formas no Argo:

- **containerSet** (os três no mesmo pod, `emptyDir` compartilhado sem PVC): o
  primeiro desenho. Falhou na validação — `containerSet` com `outputs.parameters`
  exige um único container `main`, incompatível com DOIS produtores de output
  (gestalt e facets cada um escreve sua contagem).
- **dag com templates separados** (cada indexer um pod próprio): cada template
  tem seu `main` e seus outputs, satisfazendo a regra do Argo e devolvendo a
  métrica de contagem por modo.

## Decisão
`dag`: `downloader` → fan-out `gestalt`/`facets`, cada um um template/pod próprio.
O workspace compartilhado entre pods é um **`volumeClaimTemplate`** — PVC
EFÊMERO, criado por run e deletado no fim. Não é estado durável a gerenciar; é
scratch que cruza pods (o que o `emptyDir` do containerSet não faz).

StorageClass `openebs-hostpath-ssd` (LocalPV, RWO): node-local, então os três
pods CO-LOCAM no mesmo nó. Aceitável — o headroom de RAM é por-nó, e o job roda
de madrugada.

## Consequências
- Observabilidade por modo: `gestalt` e `facets` são pods/nós distintos no Argo
  UI; falha isolada e visível. Métrica `triage_index_documents{mode}` por
  template.
- O grafo mostra nós de retry (`(0)`) porque cada template tem `retryStrategy` —
  não é bug, é o retry por-step (mais robusto para um job que fala com rede e com
  um Qdrant que reinicia). Filtrar por "Pod" no Argo UI dá o grafo enxuto; manter
  o retry por-step vale mais que o grafo bonito.
- O PVC efêmero reverte o receio inicial (PVC = peça durável a gerenciar): sendo
  criado/destruído com o run, o custo não existe.
