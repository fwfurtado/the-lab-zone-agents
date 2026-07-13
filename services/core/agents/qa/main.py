import asyncio
import logging

from agents.qa.agent import answer
from shared.config import get_settings
from shared.garage import GarageClient
from shared.log import configure_logging
from shared.metrics import start_metrics_server
from shared.observability import configure_observability
from shared.slack.app import start_bot
from shared.slack.triage_feedback import register as register_triage_feedback

logger = logging.getLogger("the_lab_zone_qa")


def _build_extra_registrars(settings) -> list:
    """Feedback de triagem (ADR-0014) reusa a MESMA conexão Socket Mode do QA
    bot — ver docstring de shared.slack.app.start_bot para o porquê. Exige
    credenciais do Garage; sem elas, o registrador simplesmente não entra na
    lista (o QA bot segue funcionando sem a capacidade de feedback, em vez de
    falhar o boot inteiro por uma dependência de uma responsabilidade que não
    é a dele por natureza)."""
    if settings.garage_endpoint is None or settings.garage_access_key is None or settings.garage_secret_key is None:
        logger.warning(
            "GARAGE_* ausente: feedback de triagem via Slack desabilitado neste processo"
        )
        return []

    garage = GarageClient(
        endpoint=settings.garage_endpoint,
        access_key=settings.garage_access_key.get_secret_value(),
        secret_key=settings.garage_secret_key.get_secret_value(),
        bucket=settings.garage_bucket,
        region=settings.garage_region,
        use_ssl=settings.garage_use_ssl,
    )
    return [lambda app: register_triage_feedback(app, garage)]


def main() -> None:
    settings = get_settings()
    configure_logging(settings.log_level)
    configure_observability()  # TracerProvider + instrument_all (no-op se OTEL_ENABLED=false)

    start_metrics_server(port=9090)
    logger.info("starting metrics at :9090/metrics")

    try:
        asyncio.run(
            start_bot(
                answer,
                logger_name="the_lab_zone_qa.bridge",
                extra_registrars=_build_extra_registrars(settings),
            )
        )
    except KeyboardInterrupt:
        logger.info("finishing app...")


if __name__ == "__main__":
    main()
