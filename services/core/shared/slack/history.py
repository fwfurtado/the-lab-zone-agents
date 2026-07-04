import logging
from collections.abc import Sequence
from typing import Any

from pydantic_ai.messages import (
    ModelMessage,
    ModelRequest,
    ModelResponse,
    TextPart,
    UserPromptPart,
)

from shared.slack.message_parser import SlackMessageParser


class SlackHistoryBuilder:
    def __init__(self, history_limit: int = 20) -> None:
        self._history_limit = history_limit

    async def build(
        self,
        logger: logging.Logger,
        client: Any,
        channel: str,
        thread_ts: str,
        bot_user_id: str,
        message_parser: SlackMessageParser,
    ) -> Sequence[ModelMessage]:
        try:
            response = await client.conversations_replies(
                channel=channel,
                ts=thread_ts,
                limit=self._history_limit,
            )
        except Exception:
            logger.warning(
                "Falha ao buscar histórico do thread; seguindo sem contexto.",
                exc_info=True,
            )
            return []

        messages = response.get("messages", [])
        history: list[ModelMessage] = []

        for message in messages[:-1]:
            model_message = self._to_model_message(
                message=message,
                bot_user_id=bot_user_id,
                message_parser=message_parser,
            )
            if model_message is not None:
                history.append(model_message)

        return history

    def _to_model_message(
        self,
        message: dict[str, Any],
        bot_user_id: str,
        message_parser: SlackMessageParser,
    ) -> ModelMessage | None:
        if message.get("subtype"):
            return None

        content = message_parser.strip_mention(message.get("text", ""))
        if not content:
            return None

        if message.get("user") == bot_user_id:
            return ModelResponse(parts=[TextPart(content=content)])

        return ModelRequest(parts=[UserPromptPart(content=content)])
