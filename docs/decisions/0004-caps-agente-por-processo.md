---
tipo: adr
numero: 4
titulo: Orçamento do agente é tunável por processo via env
status: aceito
relacionado: [the-lab-zone/docs/decisions/0016-headroom-descartado-cap-de-contexto-proprio]
---

# ADR-0004 — Orçamento do agente tunável por processo (env)

## Status
Aceito. Aplica-se ao `services/core/` (runtime compartilhado por QA e triagem).

## Contexto
O núcleo (`shared/runtime.py`) aplica limites de segurança sobre o agente: o
cap de resultado de tool (`MAX_TOOL_RESULT_CHARS`, o piso do ADR-0016 do
`the-lab-zone`) e os `UsageLimits` do Pydantic AI (total de tokens, número de
requests ao modelo, número de chamadas de tool). Esses limites foram
calibrados para o QA bot do Slack: perguntas relativamente rasas.

A triagem de incidente é um perfil de carga diferente — investigação funda que
cruza muitas superfícies (métricas, logs, estado k8s, CNPs). Durante a
validação em produção, uma triagem estourou sucessivamente: primeiro o
`total_tokens` (o limite é cumulativo da run, e o histórico reentra a cada
request — crescimento ~quadrático); depois o `tool_calls_limit` (42 chamadas
cruzando CNPs de dois namespaces + endpoints + hubble + pods — trabalho
legítimo, não loop).

## Decisão
Todos os caps são **env, tunáveis por processo**, e não hardcoded:
`AGENT_TOTAL_TOKENS_LIMIT`, `AGENT_REQUEST_LIMIT`, `AGENT_TOOL_CALLS_LIMIT`,
`AGENT_MAX_CONCURRENCY`, `MAX_TOOL_RESULT_CHARS`. O mesmo código de runtime
serve QA e triagem; cada Deployment injeta o orçamento adequado ao seu perfil.

A triagem roda com orçamento maior (1.5M tokens, 120 tool calls, 60 requests,
30k chars por tool result) e concorrência limitada de tools para não abrir
rajadas contra o vMCP. O QA bot mantém os defaults conservadores. A disciplina
de calibração: **subir apenas o limite que apertou**, com dado empírico, e não
antecipar limites que ainda não dispararam.

## Consequências
- Ganhar robustez na triagem não afrouxa o QA bot — orçamento é por Deployment.
- `AGENT_MAX_CONCURRENCY` não reduz o teto total de investigação; só serializa
  parte das chamadas quando o modelo pede muitas tools em paralelo, evitando
  sobrecarga transitória no vMCP.
- O cap de tool result (`MAX_TOOL_RESULT_CHARS`) ataca o termo quadrático: um
  resultado gordo reentra em toda request seguinte, então cortá-lo cedo reduz
  o crescimento cumulativo, não só o pico.
- O crescimento quadrático continua estrutural. A solução de fundo (envelhecer
  tool results antigos no histórico via `history_processor` do Pydantic AI —
  in-process, determinística, sem serviço no caminho, distinta do Headroom que
  o ADR-0016 rejeitou) fica registrada como trabalho futuro (Fase C), a ser
  atacada quando triagens reais flertarem com o teto de tokens.
