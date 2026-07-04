# Justfile do monorepo the-lab-zone-agents.
#
# Estrutura: cada serviço vive em services/<nome>/ com seu próprio Justfile
# expondo o MESMO contrato — test / lint / fmt. Este arquivo orquestra: os
# alvos agregados rodam em todos os serviços; os granulares (-core, -webhook)
# miram um só. O CI chama os alvos por-serviço via matrix.

# Serviços do monorepo. Adicionar um serviço = uma entrada aqui + um
# services/<nome>/Justfile com test/lint/fmt.
services := "core triage-webhook"

default:
    @just --list

# ---- Agregados: rodam o contrato em TODOS os serviços ----

test:
    #!/usr/bin/env bash
    set -euo pipefail
    for svc in {{services}}; do
        echo "== test: $svc =="
        just -f services/$svc/Justfile test
    done

lint:
    #!/usr/bin/env bash
    set -euo pipefail
    for svc in {{services}}; do
        echo "== lint: $svc =="
        just -f services/$svc/Justfile lint
    done

fmt:
    #!/usr/bin/env bash
    set -euo pipefail
    for svc in {{services}}; do
        echo "== fmt: $svc =="
        just -f services/$svc/Justfile fmt
    done

# ---- Granulares: miram um serviço (o CI usa estes na matrix) ----

test-core:
    just -f services/core/Justfile test
lint-core:
    just -f services/core/Justfile lint
fmt-core:
    just -f services/core/Justfile fmt

test-webhook:
    just -f services/triage-webhook/Justfile test
lint-webhook:
    just -f services/triage-webhook/Justfile lint
fmt-webhook:
    just -f services/triage-webhook/Justfile fmt

# ---- Dev do QA bot (long-running, Slack) ----
# Preservado da versão anterior; path ajustado pro services/core/.
qa-bot-dev env_file="services/core/.env.tpl":
    #!/usr/bin/env bash
    set -euo pipefail
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
