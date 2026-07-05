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
from shared.metrics import answer_errors_total, answer_latency, questions_total

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
    parser.add_argument(
        "--stats",
        action="store_true",
        help=(
            "Ao fim da run, imprime no stderr as estatísticas da execução "
            "(latência, compressão de histórico da Fase C). Útil para calibrar "
            "HISTORY_KEEP_RECENT_TOOL_RESULTS. Não afeta o stdout (o relatório)."
        ),
    )
    args = parser.parse_args()

    settings = get_settings()
    configure_logging(settings.log_level)  # logs -> stderr; relatório -> stdout

    context = _read_context(args.context)
    if not context:
        parser.error("nenhum contexto: passe como argumento ou via stdin (pipe).")

    # Instrumentação no transporte, como Slack (responder.py) e HTTP
    # (server.py): questions/latency/errors são incrementadas aqui porque o
    # answer() central não instrumenta — cada transporte o faz, e a CLI ficava
    # de fora, deixando a latência zerada no --stats. Idealmente a
    # instrumentação viveria no ponto único (answer()), mas migrar os dois
    # transportes em produção não se paga agora; segue-se o padrão vigente.
    questions_total.inc()
    try:
        with answer_latency.time():
            report = asyncio.run(answer(context))
    except KeyboardInterrupt:
        sys.exit(130)
    except Exception:
        answer_errors_total.inc()
        logger.exception("falha ao executar a triagem")
        sys.exit(1)

    print(report)

    if args.stats:
        # stderr: não polui o stdout (relatório) que pode estar sendo pipeado.
        from shared.metrics import render_run_stats

        print(render_run_stats(), file=sys.stderr)


if __name__ == "__main__":
    main()
