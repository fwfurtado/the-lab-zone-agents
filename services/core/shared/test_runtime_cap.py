"""Testes do cap de resultado de tool (shared.runtime._cap_tool_result).

Este é o piso de segurança do ADR-0016: o limite rígido que impede uma tool de
despejar MBs no contexto do modelo. É a defesa cuja falha custa
UsageLimitExceeded — então vale travá-la contra regressão.
"""

from shared.runtime import _TRUNCATION_NOTE, _cap_tool_result


def test_string_under_limit_passes_untouched():
    result = "log curto"
    assert _cap_tool_result(result, "tool", max_chars=1000) is result


def test_string_at_limit_is_not_truncated():
    # Fronteira: exatamente max_chars deve passar (o corte é > , não >=).
    result = "x" * 100
    assert _cap_tool_result(result, "tool", max_chars=100) == result


def test_string_over_limit_is_truncated_with_note():
    result = "y" * 500
    capped = _cap_tool_result(result, "kubernetes_pods_log", max_chars=100)
    # O corpo é cortado no limite...
    assert capped.startswith("y" * 100)
    # ...e a nota de truncamento é anexada, nomeando a tool e o limite.
    assert "kubernetes_pods_log" in capped
    assert "100" in capped
    # O total é o limite + a nota (não o tamanho original).
    assert len(capped) == 100 + len(_TRUNCATION_NOTE.format(name="kubernetes_pods_log", max=100))


def test_structured_result_under_limit_keeps_structure():
    # Não-string que cabe: devolvido INTACTO (preserva a estrutura, não
    # serializa desnecessariamente).
    result = {"pods": ["a", "b"], "count": 2}
    assert _cap_tool_result(result, "tool", max_chars=1000) is result


def test_structured_result_over_limit_degrades_to_truncated_string():
    # Não-string que estoura: degrada para string truncada — perder estrutura
    # é aceitável; estourar o contexto não.
    result = {"huge": ["item"] * 10_000}
    capped = _cap_tool_result(result, "big_tool", max_chars=200)
    assert isinstance(capped, str)
    assert len(capped) == 200 + len(_TRUNCATION_NOTE.format(name="big_tool", max=200))
    assert "big_tool" in capped


def test_truncation_note_guides_refinement():
    # A nota não é só um aviso: ela instrui o modelo a refinar a chamada.
    # Se esse texto sumir, o modelo perde a dica de como reagir ao corte.
    note = _TRUNCATION_NOTE.format(name="t", max=100)
    assert "truncado" in note
    assert "Refine" in note or "refine" in note
