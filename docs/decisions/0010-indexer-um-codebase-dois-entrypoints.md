---
tipo: adr
numero: 10
titulo: Indexer com um code base e dois entrypoints (gestalt, facets)
status: aceito
relacionado: [0009-memoria-duas-collections]
---

# ADR-0010 — Um code base, dois entrypoints no indexer

## Status
Aceito. Estrutura do `triage-indexer` (imagem em `the-lab-zone-dockerfiles`,
`ghcr.io/fwfurtado/triage-indexer`).

## Contexto
As duas collections (ADR-0009) precisam de dois indexadores. Os dois compartilham
~90% da lógica: parse do front-matter (o YAML restrito que a borda emite), corte
do preâmbulo do modelo, carga do e5-large, upsert idempotente, GC por `run_id`,
guarda anti-silent-failure, criação dos payload indexes. A ÚNICA diferença é como
o corpo vira pontos: gestalt = 1 ponto (corpo inteiro); facets = 1 por seção.

Duas imagens separadas duplicariam esse núcleo — em especial o parser de
front-matter, que já revelou bugs (namespace vazio em alertas de rede; corte de
seção quebrando em título com código inline). Dois parsers divergem com o tempo.

## Decisão
Um projeto Python (`triage_indexer/`) com núcleo comum (`_common.py`) e dois
entrypoints finos como console-scripts:

- `triage-index-gestalt` → `gestalt.py` → collection `triage_incidents`.
- `triage-index-facets` → `facets.py` → collection `triage_facets`.

O ponto de variação é uma função `build_points(report)` injetada no `reconcile`
comum. O parser vive num lugar só. `facets.py` usa **mistune** (parser markdown)
para o corte de SEÇÕES — precisa da estrutura hierárquica (### aninhado dentro de
##) e da robustez contra `## x` dentro de code fence. O corte de PREÂMBULO
(fronteira, não estrutura) fica no núcleo comum como scan de linha, sem mistune.

## Consequências
- Cada entrypoint instrumenta sua própria métrica; o núcleo não sabe de métrica.
- Um Dockerfile, um lugar para consertar o parser, um conjunto de testes do
  núcleo + testes por estratégia.
- Regra geral que fica: **parser de markdown para estrutura (seções); scan para
  fronteira (preâmbulo)**. Ferramenta proporcional ao problema.
