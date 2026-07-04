FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY pyproject.toml README.md ./
COPY shared ./shared
COPY agents ./agents

RUN pip install .

# cria um usuário não-root e passa a rodar como ele
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin appuser
USER 10001

# Dois entrypoints numa imagem:
#   qa-bot  -> bot Slack (long-running; Deployment com command: ["qa-bot"])
#   triage  -> CLI de triagem (one-shot; recebe contexto por arg/stdin)
# Os prompts viajam como package-data; SYSTEM_PROMPT_PATH pode sobrescrever.
CMD ["qa-bot"]
