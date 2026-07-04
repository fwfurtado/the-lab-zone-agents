"""CLI de triagem: recebe o contexto de um alerta/sintoma e imprime o diagnóstico.

Uso:
    triage "KubePodCrashLooping em open-webui-0 (ns ai). Investiga."
    echo "<contexto>" | triage
    triage < alerta.json

O relatório vai para o stdout; logs vão para o stderr. Assim a saída é
pipe-friendly e já está pronta para virar o corpo de um webhook na Opção 2 —
o núcleo (agents.triage.agent.answer) é o mesmo que uma API HTTP embrulharia.
"""

import argparse
import asyncio
import logging
import sys

from agents.triage.agent import answer
from shared.config import get_settings
from shared.log import configure_logging

logger = logging.getLogger("the_lab_zone_triage.cli")


def _read_context(positional: str | None) -> str:
    if positional:
        return positional.strip()
    if sys.stdin.isatty():
        return ""  # sem arg e sem pipe: nada a ler
    return sys.stdin.read().strip()


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="triage",
        description=(
            "Triagem de incidentes do the-lab-zone. Recebe o contexto de um "
            "sintoma/alerta (argumento ou stdin) e retorna o diagnóstico triado."
        ),
    )
    parser.add_argument(
        "context",
        nargs="?",
        help="Contexto do alerta/sintoma. Se omitido, lê do stdin.",
    )
    args = parser.parse_args()

    settings = get_settings()
    configure_logging(settings.log_level)  # logs -> stderr; relatório -> stdout

    context = _read_context(args.context)
    if not context:
        parser.error("nenhum contexto: passe como argumento ou via stdin (pipe).")

    try:
        report = asyncio.run(answer(context))
    except KeyboardInterrupt:
        sys.exit(130)
    except Exception:
        logger.exception("falha ao executar a triagem")
        sys.exit(1)

    print(report)


if __name__ == "__main__":
    main()
