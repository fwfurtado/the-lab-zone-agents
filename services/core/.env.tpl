SLACK_BOT_TOKEN="op://the-lab-zone/Labot-dev/bot-token"
SLACK_APP_TOKEN="op://the-lab-zone/Labot-dev/app-token"
LITELLM_BASE_URL=https://litellm.lab.the-lab.zone/v1
LITELLM_KEY="op://the-lab-zone/Labot-dev/litellm-token"
MODEL_NAME=minimax-m3-paid
# port-forward para o svc vmcp-triage
TOOLHIVE_VMCP_URL=http://localhost:4483
LOG_LEVEL=INFO
AGENT_REQUEST_LIMIT=120
AGENT_TOOL_CALLS_LIMIT=120
AGENT_TOTAL_TOKENS_LIMIT=1500000
AGENT_MAX_CONCURRENCY=4
MAX_TOOL_RESULT_CHARS=30000
HISTORY_COMPRESS_ENABLED=true
HISTORY_KEEP_RECENT_TOOL_RESULTS=16
OTEL_ENABLED=true
OTEL_SERVICE_NAME="triage-agent"
# port-forward para o POD do otel-collector. Estou usando DaemonSet e por isso não tenho o svc
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
