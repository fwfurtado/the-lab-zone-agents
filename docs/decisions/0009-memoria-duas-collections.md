---
tipo: adr
numero: 9
titulo: Memória de triagem em duas collections (gestalt + facetas)
status: aceito
relacionado: [0007-persistencia-triagem-garage, 0008-extracao-conclusoes-adiada]
---

# ADR-0009 — Duas collections: incidentes (gestalt) e facetas

## Status
Aceito. Define a estrutura de índice da memória de triagem (Fase D, Fatia A).
Fonte de verdade continua sendo o `.md` imutável no Garage (ADR-0007); o Qdrant
é índice reconstruível.

## Contexto
A memória entre triagens precisa servir a DUAS perguntas de recuperação com
formas diferentes:

1. **Regressão** — "esse incidente, como um todo, já aconteceu antes?". A query
   é o incidente novo inteiro; o que casa é a assinatura difusa do todo.
2. **Hint de investigação** — "que linha de raciocínio funcionou para esse
   sintoma?", "que causa costuma ser?". A query é dirigida a UMA faceta do
   raciocínio (sintoma, evidência, causa, próximo passo).

Servir as duas com uma collection única força um compromisso ruim: ou o ponto é
o relatório inteiro (bom para regressão, ruído para faceta — a busca por
"evidência" recupera o relatório todo) ou é fatiado por seção (bom para faceta,
destrói a gestalt do todo para regressão).

## Decisão
Duas collections, alimentadas pelo mesmo pipeline, amarradas por `dedup_key`:

- **`triage_incidents`** — 1 ponto por relatório (corpo inteiro). Serve a busca
  por gestalt/regressão. Sem chunking: fatiar por tamanho criava fronteiras
  ruins e destruía a assinatura do todo.
- **`triage_facets`** — 1 ponto por SEÇÃO do relatório. Cada faceta
  (`symptom`/`evidence`/`cause`/`next_step`) vira um vetor nítido, consultável
  isolado, com `metadata.section` no payload para filtrar a faceta desejada.

`Confiança` fica de fora das facetas (2 linhas viram ruído no top-k). Ambas
usam **e5-large** (intfloat/multilingual-e5-large, dim 1024, Cosine) — o MESMO
modelo do indexer e dos MCPs de consumo, obrigatório para query e documento
caírem no mesmo espaço vetorial.

## Consequências
- Re-embedar é grátis (fonte de verdade é o `.md`); o Qdrant pode ser destruído
  e reconstruído pelo job. Collections são estado imperativo criado em runtime
  pelo indexer — FORA do GitOps, o ArgoCD não limpa órfãs (limpeza manual via
  `DELETE /collections/X`).
- O custo é dois índices a manter e dois pontos de escrita por relatório, mas o
  desacoplamento vale: cada collection é ajustável (payload indexes, filtros)
  sem afetar a outra.
- `next_step` de uma triagem passada é SUGESTÃO, não desfecho confirmado — o
  desfecho real amarra no campo `confirmation` (gancho perecível, hoje sempre
  `unverified` até a Fatia B).
