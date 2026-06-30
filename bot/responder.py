import logging
from typing import Any

from bot.history import SlackHistoryBuilder
from bot.message_parser import SlackMessageParser
from bot.slack_message_updater import SlackMessageUpdater
from bot.types import AnswerFn
from metrics import answer_errors_total, answer_latency, questions_total


class SlackResponder:
    def __init__(
        self,
        *,
        logger: logging.Logger,
        history_builder: SlackHistoryBuilder,
        message_parser: SlackMessageParser,
        answer_fn: AnswerFn,
    ) -> None:
        self._logger = logger
        self._history_builder = history_builder
        self._message_parser = message_parser
        self._answer = answer_fn

    async def respond(
        self,
        event: dict[str, Any],
        client: Any,
        say: Any,
        *,
        bot_user_id: str,
    ) -> None:
        channel = event["channel"]
        question = self._message_parser.extract_question(event)
        thread_ts = event.get("thread_ts", event["ts"])

        if not question:
            await say(
                text="Manda a pergunta junto da menção que eu respondo. :)",
                thread_ts=thread_ts,
            )
            return

        questions_total.inc()

        placeholder = await say(text="_pensando_...", thread_ts=thread_ts)
        message_updater = SlackMessageUpdater(
            self._logger,
            client,
            channel,
            placeholder["ts"],
            thread_ts,
        )

        try:
            history = await self._history_builder.build(
                logger=self._logger,
                client=client,
                channel=channel,
                thread_ts=thread_ts,
                bot_user_id=bot_user_id,
                message_parser=self._message_parser,
            )
            with answer_latency.time():
                reply = await self._answer(
                    question, history, message_updater.push
                )

            await message_updater.finalize(reply)
        except Exception:
            answer_errors_total.inc()
            self._logger.exception("Erro ao responder pergunta no thread %s", thread_ts)
            await message_updater.finalize(
                ":warning: Deu erro ao responder. Da uma olhada nos logs do bot "
                "(`kubectl -n ai logs deploy/slack-qa-bot`)."
            )
