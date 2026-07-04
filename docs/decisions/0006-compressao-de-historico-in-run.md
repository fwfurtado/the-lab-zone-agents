---
tipo: adr
numero: 6
titulo: Compressão de histórico in-run para tool results antigos
status: aceito
relacionado: [0004-caps-agente-por-processo, the-lab-zone/docs/decisions/0016-headroom-descartado-cap-de-contexto-proprio]
---

# ADR-0006 — Compressão de histórico in-run para tool results antigos

## Status
Aceito. Aplica-se ao `services/core/` e, por consequência, ao runtime
compartilhado entre QA e triagem.

## Contexto
A triagem roda como uma run única do agente Pydantic AI. O crescimento de custo
não vem de múltiplos turnos de conversa; vem do loop síncrono de tool calls
dentro da mesma run. Cada resultado de tool volta a entrar no contexto em toda
request seguinte, o que produz crescimento cumulativo aproximadamente
quadrático e já levou a `UsageLimitExceeded` em produção (ADR-0004).

O cap de entrada de tool result (`MAX_TOOL_RESULT_CHARS`) reduz picos, mas não
resolve o acúmulo estrutural: mesmo um resultado já truncado continua reentrando
dezenas de vezes se a investigação for longa.

O Pydantic AI 2.1.0 oferece um hook in-process (`capabilities=[ProcessHistory]`)
que roda antes de cada model request e enxerga também as mensagens acumuladas
na run corrente. Isso permite envelhecer resultados antigos sem adicionar
serviço externo, latência de rede ou novo ponto de falha.

## Decisão
Adotar a **Opção A**: compressão estrutural, determinística e local dos
`ToolReturnPart` antigos no histórico da run.

- O runtime instala `ProcessHistory(process_history)` no `Agent`.
- O processor é tunável por processo via env:
  `HISTORY_COMPRESS_ENABLED` e `HISTORY_KEEP_RECENT_TOOL_RESULTS`.
- A compressão atua só em `ToolReturnPart` fora da janela dos `N` resultados
  mais recentes e nunca toca a última mensagem da run.
- O pareamento `ToolCallPart` ↔ `ToolReturnPart` é invariável: a estrutura da
  mensagem e o `tool_call_id` são preservados; só o `content` do retorno é
  trocado por um stub curto.
- Resultados multimodais não são comprimidos nesta primeira iteração.
- A compressão só acontece quando o stub fica menor que o conteúdo original;
  resultados antigos já curtos ficam intactos.

## Opções consideradas

### A. Compressão estrutural por stub
Escolhida.

Razões:

- custo zero adicional de LLM;
- latência constante e previsível;
- implementação pequena, testável e idempotente;
- ataca diretamente a causa do crescimento cumulativo.

### B. Sumarização LLM de resultados antigos
Não adotada nesta fase.

Razões:

- recoloca uma chamada LLM no caminho crítico da investigação;
- aumenta latência, custo e superfície de falha;
- é complexidade prematura antes de medir se a compressão estrutural já segura
  o crescimento sem degradar o diagnóstico.

## Consequências
- O orçamento por processo do ADR-0004 continua válido, mas deixa de ser a
  única defesa contra histórico inflado.
- A triagem ganha um mecanismo determinístico para estabilizar o tamanho do
  contexto ao longo da run.
- Observabilidade explícita passa a registrar quantos resultados foram
  comprimidos e quantos caracteres saíram do histórico.

## Invariantes
- Nunca quebrar o par `tool_call_id` entre `ToolCallPart` e `ToolReturnPart`.
- O processor deve ser barato, puro do ponto de vista de transformação e
  idempotente.
- A última mensagem e os `N` tool results mais recentes permanecem intactos.

## Gatilho para revisitar a decisão
Reabrir a opção B apenas se medições e validação end-to-end mostrarem que a
compressão estrutural preserva custo mas passa a degradar a qualidade do
diagnóstico, por exemplo quando relatórios deixam de considerar evidência
coletada cedo na investigação mesmo com `KEEP_RECENT` calibrado.
