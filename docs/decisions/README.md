# ADRs — Decisões de arquitetura (`the-lab-zone-agents`)

Decisões registradas no formato Nygard (contexto → decisão → consequências).
Cada ADR captura *por que* uma escolha foi feita, pra que ela não seja
revertida por esquecimento.

Este repositório é a camada de agentes do the-lab-zone. As decisões de
**infraestrutura** que envolvem os agentes (governança read-only, cap de
contexto, fronteira de deploy da triagem) vivem no repositório `the-lab-zone`,
em `docs/decisions/` — referenciadas aqui quando relevante. Os ADRs deste
repositório cobrem a **estrutura do código e do pipeline**.

| # | Decisão | Resumo |
|---|---|---|
| [0001](0001-monorepo-servicos-irmaos.md) | Monorepo, serviços irmãos sob `services/` | `core` (Python) e `triage-webhook` (Go) lado a lado; raiz só coordena; contrato `just` uniforme |
| [0002](0002-fronteira-go-python-sidecar.md) | Fronteira Go↔Python via sidecar localhost | Materializa o ADR-0017 do `the-lab-zone`; núcleo em `127.0.0.1`, sem Service/CNP |
| [0003](0003-borda-go-stdlib-only.md) | Borda Go stdlib-only | Zero deps; métricas Prometheus in-tree; troca por `client_golang` é mecânica se preciso |
| [0004](0004-caps-agente-por-processo.md) | Orçamento do agente por processo (env) | Mesmo runtime, orçamentos distintos; triagem cruza mais superfícies que QA; calibrar só o que apertou |
| [0005](0005-ci-unificado-contrato-just.md) | CI unificado: `just`, path-filter, semver/serviço | Um workflow; lint/test via `just`; versão independente por serviço; assimetria consciente no lint |
| [0006](0006-compressao-de-historico-in-run.md) | Compressão de histórico in-run para tool results antigos | `ProcessHistory` in-process comprime resultados antigos sem quebrar o pareamento `tool_call`/`tool_return` |
| [0007](0007-persistencia-triagem-garage.md) | Persistência da triagem no Garage | Relatório imutável como fonte de verdade S3; emenda o stdlib-only da borda para o cliente de object store |
| [0008](0008-extracao-conclusoes-adiada.md) | Extração de verdict/confidence adiada | Não-decisão deliberada; classificador de 2ª passada quando extrair (Fatia B) |
| [0009](0009-memoria-duas-collections.md) | Memória em duas collections (gestalt + facetas) | `triage_incidents` (regressão, 1 ponto/relatório) e `triage_facets` (hint, 1 ponto/seção); mesmo e5-large |
| [0010](0010-indexer-um-codebase-dois-entrypoints.md) | Indexer: um code base, dois entrypoints | Núcleo comum + `gestalt`/`facets`; parser num lugar só; mistune para seção, scan para preâmbulo |
| [0011](0011-job-indexer-dag-pvc-efemero.md) | Job do indexer: dag + PVC efêmero | Pods separados (não containerSet, que exige `main` único); PVC efêmero cruza pods; retry por-step |
| [0012](0012-consumo-dois-mcps-filtro-nomeado.md) | Consumo: dois MCPs upstream com filtro nomeado | `FILTERABLE_FIELDS` via ConfigMap/`envFrom`; `name` sem prefixo `metadata.`; becos evitados documentados |
| [0013](0013-conclusoes-artefato-garage.md) | Conclusões como artefato próprio no Garage | Emenda o 0008: `conclusions/…md` irmão do relatório; Qdrant 100% derivável; union discriminado (Diagnosed/Inconclusive) |
| [0014](0014-confirmacao-humana-slack-modal.md) | Confirmação humana como artefato próprio, via botão/modal Slack | Emenda o 0007: `confirmations/…md` terceiro prefixo, isolado do `--reclassify`; botão carrega contexto (sem side-index); `note` obrigatório só ao refutar; filtro `any`, nunca `!=` |

## Decisões relacionadas no `the-lab-zone`

- **0015** — Agentes read-only, mudanças só via PR (invariante de governança).
- **0016** — Headroom descartado; cap de contexto próprio (piso de segurança).
- **0017** — Triagem por alerta: borda Go como sidecar do núcleo Python
  (decisão-mãe do ADR-0002 daqui).

## Não-decisões documentadas

Coisas **deliberadamente não feitas**, com gatilho de retomada:

- **RollingUpdate no triage-webhook** — descartado. O `Recreate` não é
  resquício de conflito de PVC (o serviço não tem PVC); ele materializa a
  invariante "uma janela de dedup in-memory por vez" (ADR-0017). RollingUpdate
  sobreporia dois pods e reintroduziria triagem duplicada. A triagem em
  andamento no deploy já é preservada pela drenagem no shutdown
  (`terminationGracePeriodSeconds` > `TRIAGE_TIMEOUT`), não morta. Gatilho de
  reavaliação: se a dedup migrar para Valkey (o que só acontece se
  `replicas > 1`), a strategy pode ser reconsiderada.
- **Namespace de import `the_lab_zone.*` no core** — descartado. O serviço é
  buildado em container, não publicado no PyPI; o namespace flat (`shared`,
  `agents`) funciona e evita reescrever todos os imports. Gatilho: publicação
  como pacote.
- **`history_processor` para o crescimento quadrático de tokens** — adiado
  (Fase C). In-process, determinístico, distinto do Headroom rejeitado no
  ADR-0016. Gatilho: triagens reais flertando com o teto de tokens.

Registrar a não-decisão e o porquê vale tanto quanto registrar um fix.
