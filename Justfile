image := "the-lab-zone-slack-agents"
tag := "latest"

default:
    @just --list

build:
    docker build -t {{image}}:{{tag}} .

build-tag tag_name:
    docker build -t {{image}}:{{tag_name}} .


# Justfile
qa-bot-dev env_file=".env.tpl":
    #!/usr/bin/env bash
    set -euo pipefail
    # Guarda: o env de dev PRECISA apontar pro app Slack de dev (/Labot-dev/).
    # Sem isso, um .env de prod abriria 2a conexão Socket Mode no app real → fan-out.
    if ! grep -q '/Labot-dev/' {{env_file}}; then
      echo "ERRO: {{env_file}} não referencia '/Labot-dev/' — recusando p/ não conectar no app de prod." >&2
      exit 1
    fi
    sleep 2
    op run --env-file {{env_file}} -- \
      docker run --rm --network host \
        -e TOOLHIVE_VMCP_URL -e LITELLM_BASE_URL -e LITELLM_KEY \
        -e SLACK_BOT_TOKEN -e SLACK_APP_TOKEN -e MODEL_NAME -e LOG_LEVEL \
        ofwfurtado/the-lab-zone-slack-bot:dev
