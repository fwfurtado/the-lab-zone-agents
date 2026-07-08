---
tipo: adr
numero: 8
titulo: Extração de verdict/confidence adiada para o job de embed (classificador)
status: aceito (emendado pelo ADR-0013)
relacionado: [0007-persistencia-triagem-garage, 0006-compressao-de-historico-in-run, 0013-conclusoes-artefato-garage]
---

# ADR-0008 — Extração de conclusões (verdict/confidence) adiada

> **EMENDA (ADR-0013):** a decisão de *adiar* a extração e de usar um
> *classificador de segunda passada* continua válida. O que mudou foi **onde as
> conclusões são persistidas**: não no payload do Qdrant durante a indexação
> ("no job de embed", como a leitura natural deste ADR sugeria), mas num
> **artefato próprio no Garage** (`conclusions/…`), irmão do relatório. Motivo:
> conclusões no Qdrant quebram a invariante de que o Qdrant é destruível e
> reconstruível sem reclassificar. Ver ADR-0013.

## Status
Aceito como **não-decisão deliberada**. Registra por que a v1 da persistência
NÃO extrai verdict/confidence, e o que decidirá o método quando extrair.

## Contexto
O front-matter da persistência (ADR-0007) idealmente carregaria as conclusões
do modelo — o veredito (causa primária) e a confiança (que alimentaria um filtro
de recuperação, "só me lembra triagens de confiança alta"). Diferente dos fatos
de alerta, essas conclusões são SEMÂNTICAS: vivem no corpo do relatório em prosa
markdown, sem estrutura estável entre triagens.

Três métodos de extração foram considerados:

1. **Regex/parser determinístico no núcleo Python**, co-locado com o prompt.
   Foi prototipado e validado contra dois relatórios reais — e cada relatório
   revelou bugs estruturais novos (confiança estratificada por afirmação,
   marcador de confiança inline vs. em seção dedicada, qualificador parentético
   no lugar do veredito). O parser fica robusto, mas é um alvo móvel: a saída do
   agente é semântica, não estruturada, e regex impõe forma sobre algo sem forma
   estável.

2. **`output_type` do Pydantic AI** (saída estruturada na run principal).
   Elimina o parsing, mas REESTRUTURA a geração do corpo do relatório — a peça
   calibrada do sistema (A/B da Fase C, tuning do system prompt) que roda em
   produção. Mistura "mudar como o relatório é gerado" com "adicionar
   persistência".

3. **Classificador de segunda passada** (LLM lê o relatório pronto e extrai as
   duas infos no formato desejado). Não toca na geração calibrada (só lê o
   resultado), e resolve a fragilidade da regex (entende a semântica sem
   antecipar cada forma de markdown).

## Decisão
A v1 da persistência **não extrai** verdict/confidence. O front-matter carrega
só fatos de alerta + `confirmation`. As conclusões entram depois, no **job de
embed** (async, best-effort, fora do caminho crítico da triagem), provavelmente
via **classificador de segunda passada** (método 3).

Racional do adiamento:
- Sem consumidor imediato: nada LÊ verdict/confidence até o Tier 0 da
  recuperação existir (que é construído junto do job de embed). Persistir agora
  seria dado sem leitor.
- Regenerável: verdict/confidence se derivam do corpo íntegro, que a v1 já
  persiste. Adiar não perde nada — ao contrário de `confirmation`, que é
  perecível e por isso nasce já (ADR-0007).
- O classificador pertence ao caminho async do embed, não ao `server` inline —
  é lá que ele não adiciona latência à triagem, respeitando o ADR-0016.

Racional de preferir o classificador quando a hora chegar: no homelab a
inferência é local (custo é compute, não dinheiro) e roda fora do caminho
crítico, então as objeções clássicas a uma 2ª chamada LLM (latência, custo,
ponto de falha) se aplicam fraco. O trade-off que sobra é não-determinismo (temp
0 + saída constrangida ao enum mitiga) e o custo de DX de trocar testes puros
por eval harness.

Nota: qualquer método herda a mesma decisão de design já resolvida — a confiança
que importa é a do diagnóstico PRIMÁRIO, não o mínimo global entre afirmações de
camadas diferentes. Isso vira a spec do prompt do classificador.

## Gatilho de reabertura
- Se, ao construir o job de embed, o classificador se mostrar caro ou instável o
  suficiente para não valer a 2ª chamada, reconsiderar `output_type` (método 2)
  como refactor próprio da run de triagem.
- Se um filtro simples por confiança no Tier 0 se provar crude demais (porque a
  confiança real é estratificada), reconsiderar persistir a confiança como
  estrutura (por afirmação) em vez de escalar único.
