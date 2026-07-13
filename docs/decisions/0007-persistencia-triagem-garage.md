---
tipo: adr
numero: 7
titulo: Persistência da triagem no Garage (emenda o stdlib-only da borda)
status: aceito (emendado pelo ADR-0014)
relacionado: [0003-borda-go-stdlib-only, 0002-fronteira-go-python-sidecar, 0014-confirmacao-humana-slack-modal, the-lab-zone/docs/decisions/0016-headroom-descartado-cap-de-contexto-proprio]
---

# ADR-0007 — Persistência da triagem no Garage

> **EMENDA (ADR-0014):** este ADR previu `confirmation: unverified` gravado no
> front-matter do relatório desde o primeiro dia ("gancho da qualidade de
> memória", decisão 4 da Fase D) — mas o relatório é IMUTÁVEL, e o campo não
> tinha, nem podia ter, mecanismo de escrita ali sem violar essa invariante. O
> ADR-0014 move `confirmation` para um artefato próprio (`confirmations/…`,
> regravável só por feedback humano explícito) e `buildDocument` para de
> escrever o campo no relatório. O relatório de triagem permanece imutável e
> carrega só fatos de alerta.

## Status
Aceito. Aplica-se a `services/triage-webhook/`. **Emenda o ADR-0003** (borda Go
stdlib-only) no ponto específico do cliente de object store.

## Contexto
A Fase D dá memória entre triagens ao agente. O primeiro passo é a ESCRITA:
persistir cada relatório de triagem como fonte de verdade imutável, de onde um
job posterior lê para embedar no Qdrant. A persistência foi deliberadamente
adiada desde a Fase B2 (o publisher só logava e mandava pro Slack).

O relatório é imutável (nasce, nunca muda) — object store S3-compatível é o
formato ideal. O Garage já está provisionado no data platform. A borda Go já
tem o `MultiPublisher` (fan-out best-effort) e, com a propagação dos fatos de
alerta `Job`→`Report`, tem tudo que o front-matter precisa VERBATIM do payload
(alertnames, namespace, fired_at, dedup_key). O núcleo Python NÃO tem esses
fatos estruturados (recebe/devolve string), então a montagem do documento e a
escrita moram na borda Go, não no núcleo.

Escrever em S3 exige autenticação SigV4. Duas saídas: implementá-la à mão sobre
`net/http` (~80 linhas de HMAC-SHA256, mantendo o stdlib-only do ADR-0003) ou
adicionar um cliente S3.

## Decisão
Adicionar `github.com/minio/minio-go/v7` como dependência da borda e implementar
o `GaragePublisher` como mais um `Publisher` no `MultiPublisher` existente.

Isto EMENDA o ADR-0003: a borda deixa de ser estritamente stdlib-only. A emenda
é pontual — a única dependência de terceiros é o cliente de object store, e a
justificativa é manutenção: código de assinatura SigV4 artesanal é uma
superfície de bug criptográfico que não se quer manter num homelab, enquanto
minio-go é uma lib S3 estável e de escopo pequeno (feita para MinIO/Garage/Ceph,
árvore de deps enxuta comparada ao aws-sdk-go-v2). Preferiu-se acoplar a algo
estável a manter autenticação à mão.

O front-matter da v1 carrega SÓ fatos de alerta (verbatim, autoritativos) mais
`confirmation: unverified`. NÃO carrega verdict/confidence: essas conclusões do
modelo entram no job de embed (extração via classificador — ver ADR-0008). O
`confirmation` nasce em todo relatório mesmo sem mecanismo de escrita: é o
gancho da qualidade de memória (decisão 4 da Fase D), e o conhecimento
"esse diagnóstico estava certo?" decai rápido — o campo tem que existir desde o
primeiro relatório.

Chave do objeto: `triage/{namespace}/{alertname}/{fired_at}__{dedup_key}.md`. O
`dedup_key` (já derivado como groupKey + fingerprints + startsAt) é a identidade
estável do incidente: reenvio idêntico → mesma chave → PUT idempotente
sobrescreve (não polui o corpus); incidente novo (inclusive mesmo alertname
re-disparando depois de resolver) → dedup_key diferente → objeto novo.

## Consequências
- A persistência é OPCIONAL (GARAGE_ENDPOINT vazio a desliga) e best-effort: um
  PUT que falha é logado e devolvido como erro pelo `MultiPublisher`, mas não
  derruba os outros destinos nem a triagem. A memória degrada em silêncio; a
  triagem sempre publica no Slack/log. Coerente com o ADR-0016 (Headroom
  descartado): nada de serviço externo no caminho crítico — o PUT é fora dele.
- O `go.mod` deixa de ser vazio de `require`. O churn de supply chain que o
  ADR-0003 evitava reaparece, limitado a minio-go e sua árvore. Aceito pela
  troca de manutenção acima.
- A lógica com regra (montagem de front-matter + chave) fica isolada em
  `garage_document.go` (stdlib puro, testável) do I/O em `garage.go`. A troca
  do cliente S3, se um dia necessária, é localizada.
- O corpo íntegro do relatório é a fonte de verdade regenerável: o embedding (e,
  depois, verdict/confidence via classificador) se derivam dele, então mudar de
  modelo de embedding ou de extração não perde nada.

## Gatilho de reabertura
Reabrir a emenda ao ADR-0003 se a dependência minio-go trouxer custo
desproporcional (CVEs recorrentes, árvore de deps inflando) — nesse caso, a
autenticação SigV4 artesanal volta à mesa como alternativa de escopo fechado.
