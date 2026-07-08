---
tipo: adr
numero: 13
titulo: Conclusões da triagem como artefato próprio no Garage (classificador)
status: aceito
relacionado: [0007-persistencia-triagem-garage, 0008-extracao-conclusoes-adiada, 0009-memoria-duas-collections, 0010-indexer-um-codebase-dois-entrypoints, 0012-consumo-dois-mcps-filtro-nomeado]
---

# ADR-0013 — Conclusões como artefato próprio no Garage

## Status
Aceito. **Emenda o ADR-0008** na parte de ONDE as conclusões são persistidas.
Define a Fatia B.1 (classificador). O loop de `confirmation` (Fatia B.2) herda
este modelo de armazenamento.

## Contexto
O ADR-0008 decidiu (a) NÃO extrair `verdict`/`confidence` na v1 da persistência,
(b) que a extração viria depois via **classificador de segunda passada** (LLM lê
o relatório pronto), e (c) que isso rodaria "no job de embed". O (c) implicava,
na leitura natural, que as conclusões seriam gravadas direto no payload do
Qdrant durante a indexação.

Essa implicação colide com uma invariante que se quer preservar: **o Qdrant deve
ser destruível e reconstruível a qualquer momento**. Se as conclusões vivem só no
Qdrant, destruí-lo perde as classificações, e reconstruir exige rodar o LLM sobre
todo o corpus de novo. O Qdrant deixaria de ser uma projeção derivável e passaria
a ser um armazém de estado precioso.

Há ainda uma restrição mecânica: o indexer faz **GC por `run_id`** — cada re-run
reescreve os pontos com um `run_id` novo e apaga os antigos. Qualquer
enriquecimento gravado no Qdrant *fora* do indexer (por um classificador que
atualiza payload depois) seria apagado na reindexação seguinte.

## Decisão

**As conclusões são um artefato próprio no Garage**, irmão do relatório de
triagem. O Qdrant volta a ser 100% derivável — reconstruir NÃO reclassifica.

### Dois artefatos, mesmo bucket, colados por `dedup_key`

| Artefato | Chave | Natureza |
|---|---|---|
| Relatório | `triage/{ns}/{alert}/{fired_at}__{dedup_key}.md` | **Imutável** (ADR-0007) |
| Conclusão | `conclusions/{ns}/{alert}/{fired_at}__{dedup_key}.md` | **Regenerável** (sobrescrevível) |

Naturezas diferentes de propósito: o relatório é o registro histórico do que o
agente disse; a conclusão é uma **releitura** desse registro, que melhora se o
classificador melhorar. Chave espelhada (mesmo caminho, prefixo distinto); o
`dedup_key` — já a identidade estável do incidente — é a junção.

Ambos têm a mesma anatomia (front-matter YAML + corpo markdown), então o indexer
reusa o parser único do `_common.py` (ADR-0010: o parser não diverge).

### Schema: union discriminado

O classificador emite **um de dois** tipos. A incoerência é INEXPRESSÁVEL — não
existe "diagnosed sem verdict" nem "inconclusive com confidence":

```python
Diagnosed(verdict: str[<=200], confidence: Literal["high","medium","low"], rationale: str)
Inconclusive(reason: str, rationale: str)
```

- `verdict`: texto livre (<=200 chars). NÃO é enum: não há taxonomia estável de
  causas. Vira enum quando o corpus tiver volume e a taxonomia emergir.
- `confidence`: enum em **inglês** — evita acento e variação de grafia no payload
  e no filtro.
- `rationale`: prosa, vai no **corpo** do markdown (não no front-matter).
- `outcome` no payload é **derivado** da variante escolhida, não preenchido pelo
  modelo — portanto não pode divergir do resto.

### Por que `inconclusive` NÃO é um valor de `confidence`

Tentação natural: adicionar `inconclusive` ao enum de `confidence`. Rejeitado —
mistura duas dimensões. `verdict` responde "qual é a causa"; `confidence`
responde "quão seguro". Quando o relatório não conclui, **não existe causa** sobre
a qual ter confiança; a pergunta não se aplica.

Consequências de não separar:
- `verdict` perderia tipo semântico (carregaria "não há dados suficientes", que
  não é um veredito) e poluiria a busca por causa com não-causas.
- `confidence=low` (há causa, evidência fraca — pista ÚTIL) ficaria
  indistinguível de `confidence=inconclusive` (não há causa — ausência de pista).

O union discriminado resolve: são estados estruturalmente diferentes.

### Spec do prompt (herdada do ADR-0008)

A confiança extraída é a do **diagnóstico primário**, NÃO o mínimo global entre
afirmações de camadas diferentes. Ressalvas sobre evidência auxiliar ("a métrica
não tem label de pod", "o PSI não veio") não rebaixam a confiança da conclusão.
Temperatura 0 + saída constrangida ao schema mitigam o não-determinismo.

### Fluxo: o classificador nunca toca o Qdrant

```
downloader → classifier (lê triage/, escreve conclusions/) → indexer {gestalt,facets}
```

O indexer permanece o **único escritor** do Qdrant, lendo os dois prefixos e
juntando por `dedup_key`. Sem race, sem update posterior de payload, sem conflito
com o GC por `run_id`.

**Degradação suave:** relatório sem conclusão irmã (recém-triado) é indexado com
`verdict`/`confidence` ausentes — o filtro por confiança não o alcança até a
conclusão existir. Não bloqueia, não falha.

### CLI: `--reclassify` com filtro subordinado

Terceiro entrypoint do `triage-indexer` (ADR-0010). Mecânica inspirada em
`docker --build-arg K=V`:

```bash
classify                                  # incremental: só triagem sem conclusão irmã
classify --reclassify                     # tudo, sobrescreve
classify --reclassify dedup-key=a102e854  # só esse
classify --reclassify namespace=data      # futuro: mesma assinatura, nova chave
```

- O filtro é **subordinado** ao `--reclassify`: não existe filtrar sem
  reprocessar. Estado inválido inexpressável, sem validação em runtime.
- Exatamente **zero ou um** filtro; dois ou mais → erro claro.
- Regenerar é **sobrescrita idempotente** (PUT), nunca `rm` no bucket — que também
  contém as triagens imutáveis. A flag existe para nunca precisar deletar.

## Consequências

- Reconstruir o Qdrant do zero é barato e não chama LLM: relê os dois `.md`.
- Regenerar conclusões (classificador v2) é `classify --reclassify`, sem tocar no
  artefato imutável.
- `confidence` e `outcome` viram FilterableFields com condition **POSITIVA**
  (`== high`, `== diagnosed`), NUNCA `!=` — ADR-0012 registra o trace real em que
  o LLM caiu na inversão semântica e zerou as buscas.
- `reason` (do Inconclusive) fica só no `.md` por ora: registro, não filtro. Sobe
  ao payload quando houver volume e uma pergunta de meta-análise real. Dado sem
  leitor não sobe (mesma disciplina do ADR-0008).
- `rationale` NÃO entra no embedding — é artefato de auditoria e calibração do
  classificador (o eval harness previsto no ADR-0008), não conteúdo buscável.
- O `triage-indexer` ganha dependência de cliente LLM, que os dois indexers não
  usam. Aceito em nome do parser único (ADR-0010); mitigável com extra opcional
  no `pyproject` se a imagem incomodar.

## O que fica para a Fatia B.2

O loop de `confirmation` (o que muda `unverified` → `confirmed`/`refuted`).
Herda: o modelo de verdade durável fora do Qdrant, e a disciplina de condition
positiva. Fica em aberto **qual sinal** confirma/refuta — feedback humano
explícito, recorrência do alerta, ou correlação com o PR de correção. É a parte
cara, adiada de propósito.
