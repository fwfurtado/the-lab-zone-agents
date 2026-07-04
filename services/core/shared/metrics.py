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
