from __future__ import annotations

from dataclasses import replace

from pydantic_ai.messages import ModelMessage, ModelRequest, ToolReturnPart

from shared.config import get_settings
from shared.metrics import history_chars_saved_total, history_compressed_total

_COMPRESSED_NOTE = (
    "[resultado da tool `{name}` comprimido: {omitted} chars omitidos do "
    "histórico antigo; preserve o resultado recente e o raciocínio já feito.]"
)

type HistoryProcessingResult = tuple[list[ModelMessage], int, int]


def _tool_result_stub(tool_name: str, original_chars: int) -> str:
    stub = _COMPRESSED_NOTE.format(name=tool_name, omitted=original_chars)
    while len(stub) >= original_chars and original_chars > 0:
        original_chars -= 1
        stub = _COMPRESSED_NOTE.format(name=tool_name, omitted=original_chars)
    return stub


def _is_compressed_stub(tool_name: str, content: str) -> bool:
    prefix = f"[resultado da tool `{tool_name}` comprimido:"
    return content.startswith(prefix) and content.endswith("raciocínio já feito.]")


def _compress_tool_return(part: ToolReturnPart) -> tuple[ToolReturnPart, int] | None:
    # Contrato do pydantic-ai (verificado na 2.1.0): ToolReturnPart expõe
    # `.files` (lista, vazia quando não-multimodal) e `.model_response_str()`
    # (o conteúdo textual serializado). Ambos são API interna-ish; se um
    # upgrade de versão mudá-los, os testes deste módulo quebram — que é o
    # sinal desejado (foi assim que descobrimos ProcessHistory vs
    # history_processors). Não substituir por acesso direto a `.content` sem
    # revalidar: content pode ser não-string (estruturado/multimodal).
    if part.files:
        return None

    original = part.model_response_str()
    if not original or _is_compressed_stub(part.tool_name, original):
        return None

    stub = _tool_result_stub(part.tool_name, len(original))
    if stub == original or len(stub) >= len(original):
        return None

    compressed = replace(part, content=stub)
    return compressed, len(original) - len(stub)


def compress_history(
    messages: list[ModelMessage],
    *,
    enabled: bool,
    keep_recent_tool_results: int,
) -> HistoryProcessingResult:
    if not enabled or keep_recent_tool_results < 0 or len(messages) < 2:
        return messages, 0, 0

    candidates: list[tuple[int, int]] = []
    for message_index, message in enumerate(messages[:-1]):
        if not isinstance(message, ModelRequest):
            continue
        for part_index, part in enumerate(message.parts):
            if isinstance(part, ToolReturnPart):
                candidates.append((message_index, part_index))

    if len(candidates) <= keep_recent_tool_results:
        return messages, 0, 0

    # "N mais recentes" = os N últimos em candidates, que segue ordem de
    # (message_index, part_index). Para returns SEQUENCIAIS isso é recência
    # temporal. Para returns PARALELOS (vários no mesmo ModelRequest) a ordem
    # entre eles é a de disparo, não temporal — mas todos têm a mesma idade
    # (mesmo request), então qual deles cai na janela é indiferente para o
    # objetivo (limitar o volume reentrante). O pareamento é preservado em
    # qualquer caso, pois só o content é trocado.
    protected = set(candidates[-keep_recent_tool_results:]) if keep_recent_tool_results else set()
    updated_messages: list[ModelMessage] | None = None
    compressed_total = 0
    chars_saved_total = 0

    for message_index, part_index in candidates:
        if (message_index, part_index) in protected:
            continue

        source_messages = updated_messages if updated_messages is not None else messages
        message = source_messages[message_index]
        if not isinstance(message, ModelRequest):
            continue

        part = message.parts[part_index]
        if not isinstance(part, ToolReturnPart):
            continue

        compressed = _compress_tool_return(part)
        if compressed is None:
            continue

        compressed_part, chars_saved = compressed
        if updated_messages is None:
            updated_messages = list(messages)

        current_message = updated_messages[message_index]
        assert isinstance(current_message, ModelRequest)
        updated_parts = list(current_message.parts)
        updated_parts[part_index] = compressed_part
        updated_messages[message_index] = replace(current_message, parts=updated_parts)

        compressed_total += 1
        chars_saved_total += chars_saved

    return (
        (updated_messages if updated_messages is not None else messages),
        compressed_total,
        chars_saved_total,
    )


async def process_history(messages: list[ModelMessage]) -> list[ModelMessage]:
    settings = get_settings()
    processed, compressed_total, chars_saved = compress_history(
        messages,
        enabled=settings.history_compress_enabled,
        keep_recent_tool_results=settings.history_keep_recent_tool_results,
    )

    if compressed_total:
        history_compressed_total.inc(compressed_total)
    if chars_saved:
        history_chars_saved_total.inc(chars_saved)

    return processed
