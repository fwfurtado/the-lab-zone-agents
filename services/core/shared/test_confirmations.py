"""Testes do artefato de confirmação (ADR-0014).

`to_markdown` foi validado manualmente contra o parser REAL do indexer
(`triage_indexer._common.parse_document`, no repo the-lab-zone-dockerfiles) —
os seis campos do front-matter (incluindo um `note` adversarial com aspas,
dois-pontos, barra invertida e quebra de linha) sobrevivem intactos. Esses
testes fixam o contrato aqui, sem depender do outro repo em runtime.
"""

from datetime import UTC, datetime, timezone

import pytest
from pydantic import ValidationError

from shared.confirmations import (
    NOTE_MAX_CHARS,
    Confirmation,
    confirmation_path_for,
    to_markdown,
)


def _confirmation(**overrides) -> Confirmation:
    base = dict(
        dedup_key="e80a8f6d3b656e53efc1dc660452abd8",
        confirmation="confirmed",
        confirmed_by="U0123ABCDE",
        confirmed_at=datetime(2026, 7, 11, 14, 32, 0, tzinfo=UTC),
    )
    base.update(overrides)
    return Confirmation(**base)


# --------------------------------------------------------------------------
# O schema é a rede de segurança: dedup_key/confirmed_by NUNCA precisam de
# escaping YAML porque o padrão os rejeita antes de chegar ao serializador.
# --------------------------------------------------------------------------


def test_dedup_key_aceita_hex():
    assert _confirmation(dedup_key="a" * 32).dedup_key == "a" * 32


def test_dedup_key_rejeita_nao_hex():
    with pytest.raises(ValidationError):
        _confirmation(dedup_key="não-é-hex")


def test_dedup_key_rejeita_caracteres_que_quebrariam_yaml():
    for bad in ('has"quote', "has:colon", "has\nnewline", "has\\backslash"):
        with pytest.raises(ValidationError):
            _confirmation(dedup_key=bad)


def test_confirmed_by_aceita_formato_slack():
    assert _confirmation(confirmed_by="U0123ABC").confirmed_by == "U0123ABC"
    assert _confirmation(confirmed_by="W9ZZZZZZZ").confirmed_by == "W9ZZZZZZZ"


def test_confirmed_by_rejeita_formato_invalido():
    with pytest.raises(ValidationError):
        _confirmation(confirmed_by="not-a-slack-id")


def test_confirmation_so_aceita_o_enum():
    with pytest.raises(ValidationError):
        _confirmation(confirmation="maybe")


def test_note_respeita_o_teto_do_modal():
    _confirmation(note="x" * NOTE_MAX_CHARS)  # no limite: ok
    with pytest.raises(ValidationError):
        _confirmation(note="x" * (NOTE_MAX_CHARS + 1))


def test_note_e_opcional():
    assert _confirmation().note is None


def test_via_tem_default_slack_modal():
    """O gancho de extensão: sinais futuros (recorrência, PR) usarão outro via,
    mas o default de hoje é o único caminho que existe."""
    assert _confirmation().via == "slack_modal"


# --------------------------------------------------------------------------
# Serialização: front-matter sem aspas (validado contra o parser real —
# ver docstring do módulo), note no corpo.
# --------------------------------------------------------------------------


def test_to_markdown_front_matter_sem_aspas():
    md = to_markdown(_confirmation())
    assert "dedup_key: e80a8f6d3b656e53efc1dc660452abd8" in md  # sem "..."
    assert 'dedup_key: "' not in md


def test_to_markdown_inclui_todos_os_campos():
    md = to_markdown(_confirmation(note="motivo qualquer"))
    for line in (
        "schema: 1",
        "dedup_key: e80a8f6d3b656e53efc1dc660452abd8",
        "confirmation: confirmed",
        "confirmed_by: U0123ABCDE",
        "confirmed_at: 2026-07-11T14:32:00Z",
        "via: slack_modal",
    ):
        assert line in md, f"front-matter sem {line!r}\n{md}"
    assert "## Motivo" in md
    assert "motivo qualquer" in md


def test_to_markdown_sem_note_corpo_fica_vazio_mas_com_cabecalho():
    md = to_markdown(_confirmation(note=None))
    assert "## Motivo" in md
    # nada de "None" vazando pro corpo
    assert "None" not in md


def test_to_markdown_normaliza_para_utc():
    """confirmed_at pode chegar em qualquer timezone; o artefato é sempre UTC
    (mesma disciplina do timestamp compacto do objectKey em Go)."""
    from datetime import timedelta

    tz_menos3 = timezone(timedelta(hours=-3))
    c = _confirmation(confirmed_at=datetime(2026, 7, 11, 11, 32, 0, tzinfo=tz_menos3))
    md = to_markdown(c)
    assert "confirmed_at: 2026-07-11T14:32:00Z" in md  # 11:32 -03:00 == 14:32Z


# --------------------------------------------------------------------------
# confirmation_path_for: mesmo sufixo que triage_key carrega no botão.
# --------------------------------------------------------------------------


def test_confirmation_path_espelha_o_sufixo():
    suffix = "data/CiliumPolicyDrop/20260710T191200Z__e80a8f6d3b656e53efc1dc660452abd8.md"
    assert confirmation_path_for(suffix) == f"confirmations/{suffix}"


def test_confirmation_path_rejeita_vazio():
    with pytest.raises(ValueError):
        confirmation_path_for("")


def test_confirmation_path_rejeita_barra_inicial():
    with pytest.raises(ValueError):
        confirmation_path_for("/data/x.md")
