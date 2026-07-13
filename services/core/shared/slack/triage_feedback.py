"""Feedback humano de triagem via botão/modal do Slack (ADR-0014).

Registra handlers no MESMO `AsyncApp`/conexão Socket Mode que o `slack-qa-bot`
já mantém — não abre uma segunda conexão. Duas conexões com o mesmo
app-level token fariam o Slack distribuir eventos aleatoriamente entre elas; a
que não trata `block_actions`/`view_submission` de triagem descartaria parte
em silêncio (ver ADR-0014 para a citação da doc do Slack).

Fluxo: clique no botão (raiz da triagem) → abre modal com o motivo condicional
(opcional ao confirmar, obrigatório ao refutar) → submit escreve
`confirmations/<triage_key>` no Garage → revisa a mensagem raiz trocando os
botões por um registro em texto.

Cada etapa é uma função PURA (parse/build, sem I/O) coberta por teste, mais um
adaptador fino decorado pelo Bolt que só faz I/O e chama a função pura. Mesma
separação do classificador (`build_agent`/`classify_one`) e da observabilidade
— a lógica que merece teste não deve exigir um Socket Mode real para rodar.
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Literal

from slack_bolt.async_app import AsyncApp

from shared.confirmations import NOTE_MAX_CHARS, Confirmation, confirmation_path_for, to_markdown
from shared.garage import GarageClient

logger = logging.getLogger("the_lab_zone.triage_feedback")

Intent = Literal["confirmed", "refuted"]

# action_id (definido no botão que a borda Go monta, services/triage-webhook/
# internal/publish/slack.go) -> intenção. A escolha já está feita no clique;
# o modal não repete a pergunta (ver ADR-0014: um clique comunica um binário,
# reperguntar no modal seria fricção redundante).
ACTION_TO_INTENT: dict[str, Intent] = {
    "triage_confirm": "confirmed",
    "triage_refute": "refuted",
}

_MODAL_CALLBACK_ID = "triage_feedback_submit"
_REASON_BLOCK_ID = "reason_block"
_REASON_ACTION_ID = "reason_input"

_COPY: dict[Intent, dict[str, str]] = {
    "confirmed": {
        "title": "Confirmar diagnóstico",
        "intro": "Você está confirmando que o diagnóstico deste relatório estava correto.",
        "reason_label": "Motivo (opcional)",
        "placeholder": "Ex.: reincidência idêntica às anteriores, mesma causa",
    },
    "refuted": {
        "title": "Refutar diagnóstico",
        "intro": "Você está indicando que o diagnóstico deste relatório estava ERRADO.",
        "reason_label": "O que estava errado?",
        "placeholder": "Ex.: não foi o pod de teste, era outro workload no mesmo node",
    },
}


class InvalidButtonValue(ValueError):
    pass


class InvalidPrivateMetadata(ValueError):
    pass


@dataclass(frozen=True)
class ButtonContext:
    """O que o botão carrega (montado em Go, `feedbackButtonValue`). Chega
    como uma string JSON no `value` do clique."""

    dedup_key: str
    triage_key: str


def parse_button_value(raw: str) -> ButtonContext:
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as e:
        raise InvalidButtonValue(f"value do botão não é JSON: {raw!r}") from e
    dedup_key = data.get("dedup_key")
    triage_key = data.get("triage_key")
    if not dedup_key or not triage_key:
        raise InvalidButtonValue(f"value do botão sem dedup_key/triage_key: {data!r}")
    return ButtonContext(dedup_key=dedup_key, triage_key=triage_key)


@dataclass(frozen=True)
class ModalContext:
    """Viaja inteiro no `private_metadata` do modal (até 3000 chars — folgado
    para estes campos) e volta no `view_submission` sem round-trip adicional à
    API do Slack: nem para reconstruir o texto da raiz, nem para saber onde
    revisá-la depois."""

    dedup_key: str
    triage_key: str
    intent: Intent
    channel: str
    message_ts: str
    header_text: str

    def encode(self) -> str:
        return json.dumps(
            {
                "dedup_key": self.dedup_key,
                "triage_key": self.triage_key,
                "intent": self.intent,
                "channel": self.channel,
                "message_ts": self.message_ts,
                "header_text": self.header_text,
            }
        )


def parse_private_metadata(raw: str) -> ModalContext:
    try:
        data = json.loads(raw)
        return ModalContext(
            dedup_key=data["dedup_key"],
            triage_key=data["triage_key"],
            intent=data["intent"],
            channel=data["channel"],
            message_ts=data["message_ts"],
            header_text=data.get("header_text", ""),
        )
    except (json.JSONDecodeError, KeyError) as e:
        raise InvalidPrivateMetadata(f"private_metadata inválido: {raw!r}") from e


def build_modal(ctx: ModalContext) -> dict:
    """A view do modal. Título e obrigatoriedade do motivo mudam com a
    intenção (já fixada pelo clique, ver ACTION_TO_INTENT) — confirmar
    raramente precisa de explicação; refutar sem motivo é sinal pobre, então
    o campo é `optional: False` e o próprio Slack recusa o submit antes de
    nos notificar (sem round-trip de validação)."""
    copy = _COPY[ctx.intent]
    return {
        "type": "modal",
        "callback_id": _MODAL_CALLBACK_ID,
        "private_metadata": ctx.encode(),
        "title": {"type": "plain_text", "text": copy["title"]},
        "submit": {"type": "plain_text", "text": "Enviar"},
        "close": {"type": "plain_text", "text": "Cancelar"},
        "blocks": [
            {"type": "section", "text": {"type": "mrkdwn", "text": copy["intro"]}},
            {
                "type": "input",
                "block_id": _REASON_BLOCK_ID,
                "optional": ctx.intent != "refuted",
                "label": {"type": "plain_text", "text": copy["reason_label"]},
                "element": {
                    "type": "plain_text_input",
                    "action_id": _REASON_ACTION_ID,
                    "multiline": True,
                    "max_length": NOTE_MAX_CHARS,
                    "placeholder": {"type": "plain_text", "text": copy["placeholder"]},
                },
            },
        ],
    }


def extract_reason(view_state_values: dict) -> str | None:
    """Lê `view["state"]["values"]` do payload de submit. String vazia (campo
    opcional deixado em branco) vira None — não um `note=""` no artefato."""
    raw = (
        view_state_values.get(_REASON_BLOCK_ID, {})
        .get(_REASON_ACTION_ID, {})
        .get("value")
    )
    if raw is None:
        return None
    stripped = raw.strip()
    return stripped or None


def build_confirmation(ctx: ModalContext, user_id: str, reason: str | None, now: datetime) -> Confirmation:
    return Confirmation(
        dedup_key=ctx.dedup_key,
        confirmation=ctx.intent,
        confirmed_by=user_id,
        confirmed_at=now,
        note=reason,
    )


def build_updated_root_blocks(ctx: ModalContext, confirmation: Confirmation) -> list[dict]:
    """Substitui o bloco de botões por um REGISTRO em texto — quem chega
    depois vê o que foi decidido, em vez do convite a agir de novo."""
    icon = "✅" if confirmation.confirmation == "confirmed" else "❌"
    verb = "Confirmado" if confirmation.confirmation == "confirmed" else "Refutado"
    when = confirmation.confirmed_at.astimezone(UTC).strftime("%Y-%m-%d %H:%M UTC")
    line = f"{icon} {verb} por <@{confirmation.confirmed_by}> · {when}"
    if confirmation.note:
        line += f"\n> {confirmation.note}"
    return [
        {"type": "markdown", "text": ctx.header_text},
        {"type": "markdown", "text": line},
    ]


async def open_feedback_modal(ack, body, client, *, intent: Intent) -> None:
    """Adaptador do clique no botão: abre o modal certo para a intenção.

    Função de módulo (não fechada dentro de `register`) para ser testável com
    ack/body/client mockados, sem precisar de um `AsyncApp` real — mesmo
    padrão do `SlackResponder.respond` já usado por `handle_mention`/
    `handle_message` em `shared/slack/app.py`.
    """
    await ack()
    try:
        btn = parse_button_value(body["actions"][0]["value"])
    except (InvalidButtonValue, KeyError, IndexError):
        logger.exception("value do botão de feedback inválido; ignorando clique")
        return

    ctx = ModalContext(
        dedup_key=btn.dedup_key,
        triage_key=btn.triage_key,
        intent=intent,
        channel=body["channel"]["id"],
        message_ts=body["message"]["ts"],
        header_text=body["message"].get("text", ""),
    )
    try:
        await client.views_open(trigger_id=body["trigger_id"], view=build_modal(ctx))
    except Exception:
        logger.exception("falha ao abrir o modal de feedback", extra={"dedup_key": btn.dedup_key})


async def submit_feedback(ack, body, client, view, garage: GarageClient) -> None:
    """Adaptador do submit do modal: valida, escreve o artefato, revisa a
    mensagem raiz. Função de módulo pelo mesmo motivo de `open_feedback_modal`.
    """
    # ack() IMEDIATO: o Slack exige resposta em 3s. A escrita no Garage e o
    # chat_update rodam DEPOIS, fora da janela de ack — mesmo padrão de
    # "confirma rápido, trabalha devagar" que o resto do sistema já segue (o
    # webhook Go não bloqueia a resposta ao Alertmanager na triagem).
    await ack()

    try:
        ctx = parse_private_metadata(view["private_metadata"])
    except InvalidPrivateMetadata:
        logger.exception("private_metadata do modal de feedback inválido")
        return

    reason = extract_reason(view["state"]["values"])
    user_id = body["user"]["id"]
    confirmation = build_confirmation(ctx, user_id, reason, datetime.now(UTC))

    try:
        await garage.put_text(confirmation_path_for(ctx.triage_key), to_markdown(confirmation))
    except Exception:
        logger.exception(
            "falha ao escrever confirmations/ no Garage; feedback perdido",
            extra={"dedup_key": ctx.dedup_key},
        )
        # O modal já fechou (ack() aconteceu) — não há mais como sinalizar
        # erro INLINE no formulário. Uma mensagem efêmera é o próximo melhor
        # lugar: só quem clicou a vê, e ela explica o que fazer.
        try:
            await client.chat_postEphemeral(
                channel=ctx.channel,
                user=user_id,
                text="⚠️ Não consegui salvar seu feedback (falha ao gravar no Garage). Tente de novo.",
            )
        except Exception:
            logger.exception("chat_postEphemeral também falhou; feedback perdido em silêncio")
        return

    try:
        await client.chat_update(
            channel=ctx.channel,
            ts=ctx.message_ts,
            text=ctx.header_text,
            blocks=build_updated_root_blocks(ctx, confirmation),
        )
    except Exception:
        # O artefato já foi escrito com sucesso — isto é só a revisão visual
        # da mensagem. Falhar aqui não perde o feedback.
        logger.exception("confirmation gravada, mas falha ao revisar a mensagem raiz")


def register(app: AsyncApp, garage: GarageClient) -> None:
    """Ponto de entrada: registra os handlers no `app` já vivo (do
    slack-qa-bot) — glue fino sobre `open_feedback_modal`/`submit_feedback`."""

    @app.action("triage_confirm")
    async def handle_confirm(ack, body, client) -> None:
        await open_feedback_modal(ack, body, client, intent="confirmed")

    @app.action("triage_refute")
    async def handle_refute(ack, body, client) -> None:
        await open_feedback_modal(ack, body, client, intent="refuted")

    @app.view(_MODAL_CALLBACK_ID)
    async def handle_submit(ack, body, client, view) -> None:
        await submit_feedback(ack, body, client, view, garage)
