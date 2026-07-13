---
tipo: adr
numero: 14
titulo: Confirmação humana como artefato próprio, capturada por botão/modal no Slack
status: aceito
relacionado: [0007-persistencia-triagem-garage, 0012-consumo-dois-mcps-filtro-nomeado, 0013-conclusoes-artefato-garage]
---

# ADR-0014 — Confirmação humana (Fatia B.2)

## Status
Aceito. **Emenda o ADR-0007** na parte de onde `confirmation` é escrito. Define
a Fatia B.2: o loop que move `confirmation` de `unverified` para
`confirmed`/`refuted`.

## Contexto

O ADR-0007 previu o campo `confirmation` desde o primeiro relatório
("gancho da qualidade de memória", decisão 4 da Fase D), mas ele **nasce e
morre `unverified`**: `buildDocument` grava `confirmation: unverified` direto
no front-matter do `.md` em `triage/…` — o artefato que o próprio ADR-0007
declara imutável ("nasce nunca muda"). Não existe, nem pode existir sem violar
essa invariante, um mecanismo que volte e mude esse campo no lugar.

É o mesmo problema que o ADR-0013 resolveu para `verdict`/`confidence`: campo
que precisa mudar não pode morar no artefato que nunca muda. A diferença para
`confirmation` é que o dado de origem não é uma releitura barata por LLM — é
**feedback humano**, caro de obter e caro de perder. Uma sobrescrita acidental
(por exemplo, se `confirmation` morasse no mesmo `.md` que o `--reclassify`
regenera) apagaria um dado que ninguém consegue reproduzir automaticamente.

Duas decisões de mecanismo foram tomadas antes deste ADR, e ficam registradas
aqui com o raciocínio:

**Reação de emoji foi descartada.** Um canal de incidentes acumula cultura de
reação (✅ para "li", não necessariamente "confirmo") — o sinal é barato e por
isso ambíguo. Um botão com rótulo explícito remove a ambiguidade de intenção.

**O listener roda no MESMO processo do `slack-qa-bot`, não em um novo.** O bot
"Labot" já mantém uma conexão Socket Mode viva neste workspace
(`services/core/shared/slack/app.py`). A documentação do Slack é categórica:
quando múltiplas conexões usam o mesmo app-level token, cada payload pode ser
entregue a **qualquer uma** delas, sem padrão garantido. Abrir uma segunda
conexão faria os eventos de interação do feedback de triagem serem
distribuídos aleatoriamente entre os dois processos — o `slack-qa-bot`, que não
os trata, os descartaria em silêncio parte das vezes. Um Slack App dedicado
eliminaria isso, mas exige provisionamento manual novo (app, escopo, convite ao
canal); reusar a conexão existente tem custo zero de setup, ao preço de
misturar duas responsabilidades no mesmo deployment. Aceito pela simplicidade
operacional; se um dia a mistura incomodar, migrar para app dedicado é
mecânico — o handler é isolado (ver "Consequências").

## Decisão

### Terceiro artefato: `confirmations/`, mirror exato de `triage/`

```
triage/{ns}/{alert}/{fired_at}__{dedup_key}.md         (imutável, ADR-0007)
conclusions/{ns}/{alert}/{fired_at}__{dedup_key}.md    (regenerável, ADR-0013)
confirmations/{ns}/{alert}/{fired_at}__{dedup_key}.md  (regravável só por humano, este ADR)
```

Mesmo bucket, prefixo irmão, mesma chave de junção (`dedup_key`, via o mesmo
sufixo de path que `objectKey()` já calcula em Go). **Isolado por construção**
do ciclo do classificador: `--reclassify` varre `triage/` e escreve
`conclusions/`; nunca lê nem toca `confirmations/`. Não é convenção, é
consequência do glob.

Sobrescrita é permitida (PUT), mas por um motivo diferente do `conclusions/`:
lá, sobrescrever é o LLM regenerando uma releitura. Aqui, sobrescrever é a
**mesma pessoa corrigindo a própria reação** ("cliquei errado, era o outro
botão"). A escrita nunca é automática — só acontece por um clique explícito.

### Ausência de artefato = `unverified`, sem tri-state artificial

Como em `conclusions/`, degradação suave: um incidente sem `confirmations/*.md`
correspondente é `unverified` por convenção. Não existe um valor
`confirmation: unverified` gravado em lugar nenhum — evita reintroduzir o
próprio problema que motivou este ADR.

### Mecânica no Slack: dois botões na mensagem raiz, cada um abre um modal

A mensagem raiz que `SlackPublisher.Publish` já posta (curta: resumo + link pro
Alertmanager) ganha um bloco `actions` com dois botões:

```json
{
  "type": "actions",
  "block_id": "triage_feedback",
  "elements": [
    {
      "type": "button",
      "action_id": "triage_confirm",
      "text": {"type": "plain_text", "text": "✅ Confirmar diagnóstico"},
      "style": "primary",
      "value": "{\"dedup_key\":\"<dedup_key>\",\"triage_key\":\"<ns>/<alert>/<fired_at>__<dedup_key>.md\"}"
    },
    {
      "type": "button",
      "action_id": "triage_refute",
      "text": {"type": "plain_text", "text": "❌ Refutar diagnóstico"},
      "style": "danger",
      "value": "{\"dedup_key\":\"<dedup_key>\",\"triage_key\":\"<ns>/<alert>/<fired_at>__<dedup_key>.md\"}"
    }
  ]
}
```

`triage_key` é o sufixo que `objectKey()` já produz (só sem o prefixo
`triage/`), reusado tal qual — o mesmo valor vira o path em
`confirmations/<triage_key>`, sem reconstruir a chave em outro lugar (mesma
disciplina do `conclusion_path_for` no ADR-0013: derivar do que existe, não
recalcular).

**Correlação sem índice reverso.** Diferente da reação (que só carrega
`channel`+`ts`, exigindo um side-index tipo Valkey para saber a qual incidente
pertence), o clique no botão chega com o `value` que o próprio botão carrega —
o contexto vem embutido, não precisa ser procurado. Isso elimina uma peça de
infraestrutura inteira que o design por reação exigiria.

### O modal: intenção já fixada pelo botão, motivo condicional

Clicar em qualquer um dos dois botões abre um modal — não uma escolha
adicional de confirmar/refutar dentro dele, porque essa escolha já foi feita no
clique. O título e a obrigatoriedade do campo de motivo mudam conforme o botão:

```json
{
  "type": "modal",
  "callback_id": "triage_feedback_submit",
  "private_metadata": "{\"intent\":\"confirmed\",\"dedup_key\":\"...\",\"triage_key\":\"...\",\"channel\":\"...\",\"message_ts\":\"...\",\"header_text\":\"...\"}",
  "title": {"type": "plain_text", "text": "Confirmar diagnóstico"},
  "submit": {"type": "plain_text", "text": "Enviar"},
  "close": {"type": "plain_text", "text": "Cancelar"},
  "blocks": [
    {
      "type": "section",
      "text": {"type": "mrkdwn", "text": "Você está confirmando que o diagnóstico deste relatório estava correto."}
    },
    {
      "type": "input",
      "block_id": "reason_block",
      "optional": true,
      "label": {"type": "plain_text", "text": "Motivo (opcional)"},
      "element": {
        "type": "plain_text_input",
        "action_id": "reason_input",
        "multiline": true,
        "max_length": 500,
        "placeholder": {"type": "plain_text", "text": "Ex.: reincidência idêntica às anteriores, mesma causa"}
      }
    }
  ]
}
```

Ao refutar, o mesmo modal com três diferenças: título "Refutar diagnóstico",
texto de abertura ajustado, e `"optional": false` no `reason_block` — o motivo
passa a ser exigido pela própria UI do Slack (sem round-trip: o cliente recusa
o submit antes de nos notificar). É aqui que o formulário paga a escolha sobre
reação: "confirmado" raramente precisa de explicação (foi isso mesmo);
"refutado" sem motivo é sinal pobre (diz *que* errou, não *o quê*) — e é
exatamente aí que a explicação vale mais e a pessoa já está motivada a
escrevê-la.

`private_metadata` (até 3000 chars, folgado) carrega tudo que o
`view_submission` vai precisar sem round-trip adicional à API do Slack:
`intent`, `dedup_key`, `triage_key`, `channel`+`message_ts` (para o
`chat.update` depois) e `header_text` (o texto atual da mensagem raiz, lido do
próprio payload do clique — não precisa ser recalculado nem persistido em
lugar algum).

### Ao submeter: escreve o artefato e revisa a mensagem original

1. Extrai `intent`/contexto do `private_metadata`, o texto do motivo de
   `view.state.values.reason_block.reason_input.value` (pode ser vazio quando
   opcional).
2. Escreve (sobrescreve) `confirmations/<triage_key>` — PUT idempotente, mesmo
   humano corrigindo a própria escolha é o único caminho de reescrita.
3. `chat.update` na mensagem raiz: troca o bloco `actions` por uma linha de
   registro — `"✅ Confirmado por <@U0123> · 2026-07-11 14:32"` (mais o motivo,
   se houver) — para que o canal carregue o resultado, não só o convite a agir.
   Isso também evita clique duplo por pessoas diferentes gerando confusão: quem
   chega depois já vê o que foi decidido.

### Schema do artefato

```python
class Confirmation(BaseModel):
    dedup_key: str
    confirmation: Literal["confirmed", "refuted"]
    confirmed_by: str          # Slack user id (U0123ABC) — não resolvido a nome
                                # no artefato; resolver na leitura evita ficar
                                # desatualizado se a pessoa renomear a conta.
    confirmed_at: datetime     # UTC
    note: str | None = None    # limitado a 500 chars pela própria UI do modal
                                # (o Slack recusa o submit acima disso — sem
                                # round-trip de validação, sem retry: diferente
                                # do verdict do classificador, aqui não há LLM
                                # tentando de novo, então o teto é só client-side).
    via: Literal["slack_modal"] = "slack_modal"
```

`via` é o gancho de extensão para os sinais que o design original da B.2 já
previa e adiou (recorrência do alerta, correlação com PR): quando um desses
existir, escreve o mesmo artefato com `via` diferente. Quem lê (indexer,
agente) não precisa saber a origem — só o valor de `confirmation`.

`note` fica só no `.md` (auditoria, igual ao `rationale` das conclusões) — não
sobe ao payload do Qdrant nem entra no embedding. Sobe quando houver uma
pergunta real de recuperação sobre ele; dado sem leitor não sobe (ADR-0008/13).

### Emenda ao ADR-0007: para de gravar `confirmation` no relatório imutável

`buildDocument` (`garage_document.go`) para de escrever
`confirmation: unverified` no front-matter de `triage/…`. O campo era, na
prática, morto — nunca lido de volta de lá para nada além do indexer, que
agora passa a ler de `confirmations/` (com o mesmo default `unverified` na
ausência). Mantê-lo no relatório imutável é enganoso: sugere um campo que muda
num artefato que por definição não muda.

### `FILTERABLE_FIELDS`: `any`, não `==`, e nunca `!=`

Diferente de `outcome`/`confidence` (que usam `==`), `confirmation` deve expor
condition **`any`**. Razão: a pergunta mais útil do agente não é "me dê só
`confirmed`" (amostra pequena demais no início) — é "não me dê o que já foi
**refutado**", preservando `confirmed` e `unverified` como precedente válido.
Isso se expressa com segurança como `confirmation: any([confirmed,
unverified])` — positivo, lista explícita do que aceitar — nunca como
`confirmation != refuted`, que é exatamente a inversão que zerou a memória em
produção e motivou a lição do ADR-0012.

Só expor esse filtro **depois** de confirmar no Qdrant que os pontos carregam
o campo — a mesma disciplina de "dado antes de filtro" da Fatia B.1.

## Consequências

- `confirmations/` some do glob do classificador por construção — não há como
  um `--reclassify` apagar feedback humano sem alguém explicitamente apontar o
  código para o prefixo errado.
- O botão elimina a necessidade de um índice reverso (Valkey ou equivalente)
  para correlacionar a interação ao incidente — o contexto viaja no próprio
  clique.
- O handler (`block_actions` + `view_submission`) fica isolado num módulo
  próprio dentro do processo do `slack-qa-bot`, não espalhado pela lógica de
  resposta a perguntas — se um dia a mistura de responsabilidades incomodar, a
  migração para um Slack App dedicado é mover esse módulo, não reescrevê-lo.
- `SlackPublisher.Publish` (Go) ganha a responsabilidade de montar o bloco
  `actions` com o `triage_key` — mudança contida, mesmo padrão de blocos que já
  monta hoje.
- O campo `confirmation` no relatório imutável de `triage/` deixa de ser
  escrito; leitores antigos que dependiam dele (nenhum encontrado além do
  indexer) precisariam migrar para ler `confirmations/`.
- Falta decidir, fora deste ADR: se `note` de uma refutação deve um dia virar
  faceta indexável (uma "correção" pesquisável, no espírito do
  `section: cause`) — adiado até haver volume que justifique.
