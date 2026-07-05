"""metrics.py — métricas Prometheus dos agentes.

Nomes GENÉRICOS: QA e triagem emitem as MESMAS séries; quem distingue os dois
é o label de pod/deployment que o VMPodScrape adiciona no scrape. (Antes eram
slack_qa_bot_*; dashboards/alertas que referenciam aquele nome precisam mudar.)
"""

from prometheus_client import Counter, Histogram, start_http_server

questions_total = Counter(
    "lab_agent_questions_total",
    "Total de perguntas/sintomas recebidos pelo agente.",
)

answer_errors_total = Counter(
    "lab_agent_answer_errors_total",
    "Total de respostas que falharam.",
)

answer_latency = Histogram(
    "lab_agent_answer_latency_seconds",
    "Latência de responder, do recebimento ao texto final.",
    buckets=(0.5, 1, 2, 5, 10, 20, 30, 60, 120),
)

history_compressed_total = Counter(
    "triage_history_compressed_total",
    "Total de resultados antigos de tool comprimidos no histórico da run.",
)

history_chars_saved_total = Counter(
    "triage_history_chars_saved_total",
    "Total de caracteres removidos de resultados antigos de tool no histórico da run.",
)


def start_metrics_server(port: int = 9090) -> None:
    start_http_server(port)


def _sample_value(metric: Counter | Histogram, sample_name: str) -> float:
    """Lê um sample específico via a API pública .collect() do client.

    One-shot (CLI) não expõe /metrics — o registry morre com o processo. Este
    helper lê os valores acumulados em memória para dump ao fim da run, sem
    tocar em internals privados (._value).
    """
    for family in metric.collect():
        for sample in family.samples:
            if sample.name == sample_name:
                return sample.value
    return 0.0


def render_run_stats() -> str:
    """Snapshot legível das métricas da run, para o fim de uma execução CLI.

    Foco em calibração da Fase C (compressão de histórico) e no custo da run.
    Vai para o stderr — o stdout continua sendo só o relatório (pipe-friendly).
    """
    compressed = _sample_value(history_compressed_total, "triage_history_compressed_total")
    chars_saved = _sample_value(history_chars_saved_total, "triage_history_chars_saved_total")
    questions = _sample_value(questions_total, "lab_agent_questions_total")
    errors = _sample_value(answer_errors_total, "lab_agent_answer_errors_total")
    latency_sum = _sample_value(answer_latency, "lab_agent_answer_latency_seconds_sum")
    latency_count = _sample_value(answer_latency, "lab_agent_answer_latency_seconds_count")

    avg_saved = (chars_saved / compressed) if compressed else 0.0
    latency = (latency_sum / latency_count) if latency_count else 0.0

    lines = [
        "── estatísticas da run ──",
        f"  latência:                {latency:.1f}s",
        f"  perguntas / erros:       {int(questions)} / {int(errors)}",
        "  compressão de histórico (Fase C):",
        f"    resultados comprimidos: {int(compressed)}",
        f"    chars economizados:     {int(chars_saved)}",
        f"    média por compressão:   {avg_saved:.0f} chars",
    ]
    if compressed == 0:
        lines.append("    (nenhuma compressão — histórico não excedeu KEEP_RECENT, ou feature off)")
    return "\n".join(lines)
