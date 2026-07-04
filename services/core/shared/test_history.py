from pydantic_ai.messages import (
    ModelMessage,
    ModelRequest,
    ModelResponse,
    TextPart,
    ToolCallPart,
    ToolReturnPart,
)

from shared.config import get_settings
from shared.history import _COMPRESSED_NOTE, compress_history, process_history


def _build_history() -> list[ModelMessage]:
    return [
        ModelResponse(
            parts=[
                ToolCallPart(tool_name="metrics_query", args={"q": "first"}, tool_call_id="call-1")
            ]
        ),
        ModelRequest(
            parts=[
                ToolReturnPart(tool_name="metrics_query", content="a" * 400, tool_call_id="call-1")
            ]
        ),
        ModelResponse(
            parts=[
                ToolCallPart(tool_name="logs_query", args={"q": "second"}, tool_call_id="call-2")
            ]
        ),
        ModelRequest(
            parts=[ToolReturnPart(tool_name="logs_query", content="b" * 320, tool_call_id="call-2")]
        ),
        ModelResponse(
            parts=[ToolCallPart(tool_name="pods_query", args={"q": "third"}, tool_call_id="call-3")]
        ),
        ModelRequest(
            parts=[ToolReturnPart(tool_name="pods_query", content="c" * 280, tool_call_id="call-3")]
        ),
        ModelResponse(parts=[TextPart(content="relatorio final")]),
    ]


def _tool_return_content(message: ModelMessage) -> str:
    assert isinstance(message, ModelRequest)
    part = message.parts[0]
    assert isinstance(part, ToolReturnPart)
    assert isinstance(part.content, str)
    return part.content


def _is_tool_pairing_valid(messages: list[ModelMessage]) -> bool:
    tool_calls: dict[str, str] = {}
    tool_returns: dict[str, str] = {}

    for message in messages:
        if isinstance(message, ModelResponse):
            for part in message.parts:
                if isinstance(part, ToolCallPart):
                    tool_calls[part.tool_call_id] = part.tool_name
        elif isinstance(message, ModelRequest):
            for request_part in message.parts:
                if isinstance(request_part, ToolReturnPart):
                    tool_returns[request_part.tool_call_id] = request_part.tool_name

    return tool_calls == tool_returns


def test_process_history_preserves_tool_pairing():
    history = _build_history()
    processed, _, _ = compress_history(history, enabled=True, keep_recent_tool_results=2)

    assert _is_tool_pairing_valid(processed)


def test_recent_results_and_last_message_stay_intact():
    history = _build_history()

    processed, compressed_total, _ = compress_history(
        history,
        enabled=True,
        keep_recent_tool_results=2,
    )

    assert compressed_total == 1
    assert processed[3] is history[3]
    assert processed[5] is history[5]
    assert processed[-1] is history[-1]


def test_old_result_is_replaced_by_smaller_stub():
    history = _build_history()
    original = _tool_return_content(history[1])

    processed, compressed_total, chars_saved = compress_history(
        history,
        enabled=True,
        keep_recent_tool_results=2,
    )
    compressed = _tool_return_content(processed[1])

    assert compressed_total == 1
    assert chars_saved == len(original) - len(compressed)
    assert compressed.startswith("[resultado da tool `metrics_query` comprimido:")
    assert len(compressed) < len(original)


def test_history_compression_is_idempotent():
    history = _build_history()

    first_pass, _, _ = compress_history(history, enabled=True, keep_recent_tool_results=2)
    second_pass, _, _ = compress_history(first_pass, enabled=True, keep_recent_tool_results=2)

    assert second_pass == first_pass


async def test_history_processing_is_noop_when_disabled_or_under_limit(monkeypatch):
    history = _build_history()

    processed_disabled, compressed_total_disabled, chars_saved_disabled = compress_history(
        history,
        enabled=False,
        keep_recent_tool_results=2,
    )
    processed_under_limit, compressed_total_under_limit, chars_saved_under_limit = compress_history(
        history,
        enabled=True,
        keep_recent_tool_results=3,
    )

    assert processed_disabled is history
    assert compressed_total_disabled == 0
    assert chars_saved_disabled == 0
    assert processed_under_limit is history
    assert compressed_total_under_limit == 0
    assert chars_saved_under_limit == 0

    get_settings.cache_clear()
    monkeypatch.setenv("LITELLM_KEY", "test-key")
    monkeypatch.setenv("TOOLHIVE_VMCP_URL", "http://vmcp.test")
    monkeypatch.setenv("HISTORY_COMPRESS_ENABLED", "false")
    assert await process_history(history) == history
    get_settings.cache_clear()


def test_stub_template_mentions_compression():
    note = _COMPRESSED_NOTE.format(name="tool", omitted=123)
    assert "comprimido" in note
    assert "123" in note
