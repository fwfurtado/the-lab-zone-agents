---
tipo: adr
numero: 12
titulo: Consumo da memória via dois MCPs upstream com filtro nomeado (FILTERABLE_FIELDS)
status: aceito
relacionado: [0009-memoria-duas-collections, the-lab-zone/docs/decisions/0015-agentes-read-only]
---

# ADR-0012 — Consumo: dois MCPs upstream com filtro nomeado

## Status
Aceito. Fecha a Fatia A da Fase D. Duas instâncias do `mcp-server-qdrant`
(upstream oficial, imagem `ofwfurtado/mcp-server-qdrant:0.2.2`) expõem a memória
ao agente de triagem via vMCP. Registra as decisões e os becos evitados.

## Contexto
As duas collections (ADR-0009) precisam ser consultáveis pelo agente com filtro
de payload (o filtro híbrido é metade do valor: buscar por vetor E restringir por
`namespace`/`alertnames`/`section`/`confirmation`). O upstream tem restrições que
moldaram o desenho:

1. **Uma collection por instância** → duas instâncias
   (`triage-incidents-mcp`, `triage-facets-mcp`), não uma.
2. **Filtro não vem por padrão** — é habilitado por `FILTERABLE_FIELDS`.

## Decisão
Filtro **nomeado** via `FILTERABLE_FIELDS` (não arbitrary filter, não código
custom, não achatamento de payload). Cada instância declara seus campos
filtráveis; as `description` viram doc da tool que guia o agente:

- `triage-incidents-mcp` → `namespace`(==), `alertnames`(any).
- `triage-facets-mcp` → `section`(==), `namespace`(==).

(`confirmation` fez parte da versão inicial mas foi removido — ver Consequências.)

Ambas read-only (`QDRANT_READ_ONLY`), e5-large, grupo `lab-readonly`, agregadas
no vMCP de triagem com `excludeAll: false` (o `qdrant-mcp` do QA segue excluído).

## Descobertas que decidiram o caminho (para não repetir a investigação)

- **`FILTERABLE_FIELDS` funciona por env var** apesar de o campo não ter
  `validation_alias`: o pydantic-settings casa o nome do campo em maiúsculas e
  desserializa o JSON complexo para `list[FilterableField]`. Falha barulhenta
  (ValidationError no boot) se o JSON for inválido.
- **O JSON precisa ser UMA LINHA** — o ToolHive valida os valores de env do
  RunConfig e rejeita quebra de linha (`invalid characters`). Bloco YAML `|`
  quebra; JSON minificado entre aspas simples resolve.
- **Via ConfigMap + `envFrom`** (não `spec.env`): move o dado de config para um
  objeto dedicado E escapa da validação de env do ToolHive (o valor vai ao
  container pelo mecanismo nativo do Kubernetes). O ToolHive propaga `envFrom`
  do `podTemplateSpec`. O JSON ainda é uma linha (env com `\n` é problemática
  para o pydantic parsear de qualquer forma).
- **O `name` do FilterableField NÃO leva o prefixo `metadata.`** — o upstream
  o usa como nome de parâmetro da tool (não pode ter ponto: `inspect.Parameter`)
  E re-prefixa `metadata.` sozinho ao montar o `FieldCondition`
  (`common/filters.py`). Passar `metadata.section` quebrava as duas pontas. O
  payload aninhado sob `metadata` fica como está — nada de achatar nem re-indexar.

## Becos evitados (rejeitados com motivo)
- **Arbitrary filter** (`QDRANT_ALLOW_ARBITRARY_FILTER`): o agente montaria o
  filtro Qdrant cru. Perde o filtro guiado (as descriptions), que é o valor.
- **Achatar o payload** (campos filtráveis no topo, não sob `metadata`):
  desnecessário — o upstream QUER o aninhamento sob `metadata` (ele re-prefixa).
- **Imagem custom** (subclasse de `QdrantMCPServer`): código a manter; o filtro
  nomeado por env resolve sem código.

## Consequências
- O filtro `confirmation` (condition `!=`) FOI REMOVIDO dos campos filtráveis
  (2026-07). Motivo: numa triagem real (drop-test), o agente passou
  `confirmation: "unverified"` nas 4 buscas e ZEROU todas — o `!=` exclui o valor
  passado, e como todo relatório é `unverified`, isso excluía tudo. O agente
  racionalizou "quero os não-verificados" e passou o valor literal, caindo na
  inversão semântica (confirmado no trace do Langfuse). O system prompt instruía
  `refuted`, mas a instrução contraintuitiva não segurou o modelo. Como o campo
  era **dormente** (nenhum relatório é confirmed/refuted até a Fatia B), removê-lo
  não perdeu recuperação e eliminou a armadilha na raiz (o agente não pode passar
  um campo fora do schema da tool), não só no prompt.
- LIÇÃO para a Fatia B: quando `confirmation` ganhar valores variados e voltar a
  ser filtrável, usar condition `any` (valor positivo: "me dê os confirmed"), NUNCA
  `!=`. A semântica "o valor passado é o excluído" confunde humano E LLM — não é
  risco teórico, aconteceu em produção.
- Payload indexes (criados pelo indexer em `_ensure_collection`) são o que
  HABILITA o filtro performático no Qdrant — a contraparte de escrita deste ADR.
