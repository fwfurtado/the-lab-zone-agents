import asyncio
import logging

from async_lru import alru_cache
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from slack_bolt.async_app import AsyncApp

from shared.config import get_settings
from shared.slack.history import SlackHistoryBuilder
from shared.slack.message_parser import SlackMessageParser
from shared.slack.responder import SlackResponder
from shared.types import AnswerFn


async def start_bot(
    answer_fn: AnswerFn,
    *,
    logger_name: str = "the_lab_zone.bridge",
) -> None:
    """Sobe a ponte Slack (Socket Mode) para um agente.

    Ponte I/O pura e agnóstica ao agente: recebe o `answer_fn` (contrato
    shared.types.AnswerFn) e não conhece a implementação concreta. Só o modo
    bot exige os tokens do Slack — por isso são opcionais no config e validados
    aqui, na fronteira que de fato os usa.
    """
    settings = get_settings()
    logger = logging.getLogger(logger_name)

    bot_token = settings.slack_bot_token
    app_token = settings.slack_app_token
    if bot_token is None or app_token is None:
        raise RuntimeError("SLACK_BOT_TOKEN e SLACK_APP_TOKEN são obrigatórios para o modo bot.")

    app = AsyncApp(
        token=bot_token.get_secret_value(),
        # Socket Mode nao usa verificacao de assinatura HTTP (nao ha signing_secret).
        # Sem isso, o bolt instancia o middleware de request verification e estoura
        # com "signing_secret must not be empty".
        signing_secret="",
        request_verification_enabled=False,
    )

    message_parser = SlackMessageParser()
    history_builder = SlackHistoryBuilder()
    responder = SlackResponder(
        logger=logger,
        history_builder=history_builder,
        message_parser=message_parser,
        answer_fn=answer_fn,
    )

    @alru_cache(maxsize=1)
    async def _get_bot_user_id() -> str:
        """Busca o user id do bot uma vez e memoiza pela vida do processo.

        O user id não muda com rotação de token, então cache permanente é seguro.
        Evita um auth_test() de rede a cada mensagem recebida.
        """
        auth = await app.client.auth_test()
        user_id = auth["user_id"]
        if not isinstance(user_id, str):
            raise RuntimeError(f"auth_test retornou user_id inesperado: {user_id!r}")
        return user_id

    async def _respond(event, client, say) -> None:
        bot_user_id = await _get_bot_user_id()
        await responder.respond(event, client, say, bot_user_id=bot_user_id)

    @app.event("app_mention")
    async def handle_mention(event, client, say):
        await _respond(event, client, say)

    @app.event("message")
    async def handle_message(event, client, say):
        if event.get("channel_type") != "im":
            return
        if event.get("bot_id") or event.get("subtype"):
            return
        await _respond(event, client, say)

    handler = AsyncSocketModeHandler(app, app_token.get_secret_value())

    logger.info("starting the-lab-zone bot socket...")

    try:
        await handler.start_async()
    except asyncio.CancelledError:
        logger.info("finishing the-lab-zone bot socket...")
    finally:
        await handler.close_async()
