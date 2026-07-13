"""Testes do feedback de triagem via Slack (ADR-0014).

Funções puras (parse/build) testadas diretamente, sem Bolt. Os adaptadores
(`open_feedback_modal`, `submit_feedback`) testados com ack/body/client/garage
mockados — são funções de módulo (não fechadas em `register`) justamente para
isso.
"""

from datetime import UTC, datetime
from unittest.mock import AsyncMock

import pytest

from shared.confirmations import Confirmation
from shared.slack.triage_feedback import (
    ButtonContext,
    InvalidButtonValue,
    InvalidPrivateMetadata,
    ModalContext,
    build_confirmation,
    build_modal,
    build_updated_root_blocks,
    extract_reason,
    open_feedback_modal,
    parse_button_value,
    parse_private_metadata,
    submit_feedback,
)

# --------------------------------------------------------------------------
# parse_button_value: o que o botão (montado em Go) carrega
# --------------------------------------------------------------------------


def test_parse_button_value_ok():
    ctx = parse_button_value(
        '{"dedup_key":"e80a8f6d3b656e53efc1dc660452abd8","triage_key":"data/X/20260710__e80a8f6d.md"}'
    )
    assert ctx == ButtonContext(
        dedup_key="e80a8f6d3b656e53efc1dc660452abd8",
        triage_key="data/X/20260710__e80a8f6d.md",
    )


def test_parse_button_value_json_invalido():
    with pytest.raises(InvalidButtonValue):
        parse_button_value("isso não é json")


def test_parse_button_value_sem_dedup_key():
    with pytest.raises(InvalidButtonValue):
        parse_button_value('{"triage_key":"x"}')


def test_parse_button_value_sem_triage_key():
    with pytest.raises(InvalidButtonValue):
        parse_button_value('{"dedup_key":"x"}')


# --------------------------------------------------------------------------
# ModalContext: encode/decode via private_metadata (round-trip)
# --------------------------------------------------------------------------


def _ctx(**overrides) -> ModalContext:
    base = dict(
        dedup_key="e80a8f6d3b656e53efc1dc660452abd8",
        triage_key="data/X/20260710__e80a8f6d.md",
        intent="confirmed",
        channel="C0123",
        message_ts="1234567890.000100",
        header_text="🔍 *Triagem automática* — CiliumPolicyDrop",
    )
    base.update(overrides)
    return ModalContext(**base)


def test_private_metadata_round_trip():
    ctx = _ctx(intent="refuted")
    back = parse_private_metadata(ctx.encode())
    assert back == ctx


def test_parse_private_metadata_invalido():
    with pytest.raises(InvalidPrivateMetadata):
        parse_private_metadata("não é json")


def test_parse_private_metadata_incompleto():
    with pytest.raises(InvalidPrivateMetadata):
        parse_private_metadata('{"dedup_key":"x"}')  # faltam campos


# --------------------------------------------------------------------------
# build_modal: título e obrigatoriedade do motivo mudam com a intenção
# --------------------------------------------------------------------------


def test_modal_de_confirmacao_tem_motivo_opcional():
    view = build_modal(_ctx(intent="confirmed"))
    assert view["title"]["text"] == "Confirmar diagnóstico"
    reason_block = next(b for b in view["blocks"] if b.get("block_id") == "reason_block")
    assert reason_block["optional"] is True


def test_modal_de_refutacao_exige_motivo():
    view = build_modal(_ctx(intent="refuted"))
    assert view["title"]["text"] == "Refutar diagnóstico"
    reason_block = next(b for b in view["blocks"] if b.get("block_id") == "reason_block")
    assert reason_block["optional"] is False


def test_modal_carrega_o_contexto_no_private_metadata():
    ctx = _ctx()
    view = build_modal(ctx)
    assert parse_private_metadata(view["private_metadata"]) == ctx


def test_modal_callback_id_e_o_que_o_handler_de_submit_escuta():
    view = build_modal(_ctx())
    assert view["callback_id"] == "triage_feedback_submit"


# --------------------------------------------------------------------------
# extract_reason: string vazia vira None, não note=""
# --------------------------------------------------------------------------


def test_extract_reason_presente():
    values = {"reason_block": {"reason_input": {"type": "plain_text_input", "value": "  motivo aqui  "}}}
    assert extract_reason(values) == "motivo aqui"


def test_extract_reason_vazio_vira_none():
    values = {"reason_block": {"reason_input": {"type": "plain_text_input", "value": ""}}}
    assert extract_reason(values) is None


def test_extract_reason_so_espacos_vira_none():
    values = {"reason_block": {"reason_input": {"type": "plain_text_input", "value": "   "}}}
    assert extract_reason(values) is None


def test_extract_reason_ausente():
    assert extract_reason({}) is None


# --------------------------------------------------------------------------
# build_confirmation / build_updated_root_blocks
# --------------------------------------------------------------------------


def test_build_confirmation():
    ctx = _ctx(intent="refuted")
    now = datetime(2026, 7, 11, 14, 32, tzinfo=UTC)
    c = build_confirmation(ctx, "U0123ABC", "não foi isso", now)
    assert c.dedup_key == ctx.dedup_key
    assert c.confirmation == "refuted"
    assert c.confirmed_by == "U0123ABC"
    assert c.note == "não foi isso"
    assert c.via == "slack_modal"


def test_updated_blocks_confirmado_sem_nota():
    c = Confirmation(
        dedup_key="e80a8f6d3b656e53efc1dc660452abd8",
        confirmation="confirmed",
        confirmed_by="U0123ABC",
        confirmed_at=datetime(2026, 7, 11, 14, 32, tzinfo=UTC),
    )
    blocks = build_updated_root_blocks(_ctx(), c)
    assert blocks[0]["text"] == _ctx().header_text
    assert "✅" in blocks[1]["text"]
    assert "<@U0123ABC>" in blocks[1]["text"]


def test_updated_blocks_refutado_com_nota():
    c = Confirmation(
        dedup_key="e80a8f6d3b656e53efc1dc660452abd8",
        confirmation="refuted",
        confirmed_by="U0123ABC",
        confirmed_at=datetime(2026, 7, 11, 14, 32, tzinfo=UTC),
        note="não foi o pod de teste",
    )
    blocks = build_updated_root_blocks(_ctx(intent="refuted"), c)
    assert "❌" in blocks[1]["text"]
    assert "não foi o pod de teste" in blocks[1]["text"]


# --------------------------------------------------------------------------
# open_feedback_modal: adaptador do clique, com ack/body/client mockados
# --------------------------------------------------------------------------


def _block_actions_body(value: str = '{"dedup_key":"e80a8f6d3b656e53efc1dc660452abd8","triage_key":"data/X/20260710__e80a8f6d.md"}'):
    return {
        "actions": [{"value": value}],
        "channel": {"id": "C0123"},
        "message": {"ts": "1234.5678", "text": "🔍 *Triagem automática* — X"},
        "trigger_id": "trig123",
    }


@pytest.mark.asyncio
async def test_open_feedback_modal_abre_com_intent_correto():
    ack = AsyncMock()
    client = AsyncMock()
    await open_feedback_modal(ack, _block_actions_body(), client, intent="confirmed")

    ack.assert_awaited_once()
    client.views_open.assert_awaited_once()
    _, kwargs = client.views_open.call_args
    assert kwargs["trigger_id"] == "trig123"
    assert kwargs["view"]["title"]["text"] == "Confirmar diagnóstico"


@pytest.mark.asyncio
async def test_open_feedback_modal_value_invalido_nao_quebra():
    ack = AsyncMock()
    client = AsyncMock()
    await open_feedback_modal(ack, _block_actions_body(value="lixo"), client, intent="confirmed")

    ack.assert_awaited_once()  # sempre confirma o clique ao Slack
    client.views_open.assert_not_awaited()  # mas não abre modal com contexto inválido


# --------------------------------------------------------------------------
# submit_feedback: adaptador do submit, com garage mockado
# --------------------------------------------------------------------------


def _view_submission(ctx: ModalContext, reason: str | None = None):
    return {
        "private_metadata": ctx.encode(),
        "state": {"values": {"reason_block": {"reason_input": {"value": reason}}}},
    }, {"user": {"id": "U0123ABC"}}


@pytest.mark.asyncio
async def test_submit_feedback_escreve_no_garage_e_atualiza_a_mensagem():
    ctx = _ctx(intent="confirmed")
    view, body = _view_submission(ctx, reason=None)
    ack = AsyncMock()
    client = AsyncMock()
    garage = AsyncMock()

    await submit_feedback(ack, body, client, view, garage)

    ack.assert_awaited_once()
    garage.put_text.assert_awaited_once()
    args, _ = garage.put_text.call_args
    key, md = args
    assert key == f"confirmations/{ctx.triage_key}"
    assert "confirmation: confirmed" in md

    client.chat_update.assert_awaited_once()
    _, kwargs = client.chat_update.call_args
    assert kwargs["channel"] == ctx.channel
    assert kwargs["ts"] == ctx.message_ts


@pytest.mark.asyncio
async def test_submit_feedback_falha_no_garage_posta_efemera_e_nao_atualiza_mensagem():
    ctx = _ctx()
    view, body = _view_submission(ctx)
    ack = AsyncMock()
    client = AsyncMock()
    garage = AsyncMock()
    garage.put_text.side_effect = RuntimeError("s3 fora do ar")

    await submit_feedback(ack, body, client, view, garage)

    client.chat_postEphemeral.assert_awaited_once()
    client.chat_update.assert_not_awaited()  # não revisa a mensagem se não gravou


@pytest.mark.asyncio
async def test_submit_feedback_falha_no_chat_update_nao_reescreve_garage():
    """A gravação já aconteceu; falhar só a revisão visual não deve re-tentar
    escrever (não há nada de novo a escrever) nem propagar exceção."""
    ctx = _ctx()
    view, body = _view_submission(ctx)
    ack = AsyncMock()
    client = AsyncMock()
    client.chat_update.side_effect = RuntimeError("rede caiu")
    garage = AsyncMock()

    await submit_feedback(ack, body, client, view, garage)  # não deve levantar

    garage.put_text.assert_awaited_once()


@pytest.mark.asyncio
async def test_submit_feedback_private_metadata_invalido_nao_quebra():
    ack = AsyncMock()
    client = AsyncMock()
    garage = AsyncMock()
    view = {"private_metadata": "lixo", "state": {"values": {}}}
    body = {"user": {"id": "U0123ABC"}}

    await submit_feedback(ack, body, client, view, garage)

    ack.assert_awaited_once()
    garage.put_text.assert_not_awaited()
