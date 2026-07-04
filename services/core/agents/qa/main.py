import asyncio
import logging

from agents.qa.agent import answer
from shared.config import get_settings
from shared.log import configure_logging
from shared.metrics import start_metrics_server
from shared.slack.app import start_bot

logger = logging.getLogger("the_lab_zone_qa")


def main() -> None:
    settings = get_settings()
    configure_logging(settings.log_level)

    start_metrics_server(port=9090)
    logger.info("starting metrics at :9090/metrics")

    try:
        asyncio.run(start_bot(answer, logger_name="the_lab_zone_qa.bridge"))
    except KeyboardInterrupt:
        logger.info("finishing app...")


if __name__ == "__main__":
    main()
