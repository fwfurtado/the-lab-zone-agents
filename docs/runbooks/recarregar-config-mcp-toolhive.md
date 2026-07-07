# Runbook — Recarregar config dos MCPs de memória (ToolHive)

**TL;DR:** ao mudar o `FILTERABLE_FIELDS` (ou qualquer env via ConfigMap) de um
MCP de memória de triagem, é preciso **reiniciar o StatefulSet do backend** — não
o Deployment que aparece no `kubectl get deploy`. Sem isso, a mudança do ConfigMap
**não** entra no pod que a lê, e passa despercebida (o pod segue com o valor antigo).

```bash
kubectl -n ai rollout restart statefulset/triage-incidents-mcp statefulset/triage-facets-mcp
kubectl -n ai rollout status  statefulset/triage-incidents-mcp --timeout=120s
kubectl -n ai rollout status  statefulset/triage-facets-mcp    --timeout=120s
```

O `rollout status` no fim é importante: ele **espera** os pods novos ficarem
prontos e confirma que subiram. Sem ele, é fácil aplicar e seguir em frente sem
perceber que o restart não terminou (ou falhou).

---

## Por que isto não é óbvio (a topologia do ToolHive)

Um `MCPServer` do ToolHive **não** é um pod só. O operator materializa **dois
workloads** por MCP:

| Workload | O que roda | Aparece como | Lê o `FILTERABLE_FIELDS`? |
|---|---|---|---|
| **Proxy runner** | `ghcr.io/stacklok/toolhive/proxyrunner` | `Deployment` (`triage-facets-mcp`) | **Não** |
| **Backend (mcp-server)** | `ofwfurtado/mcp-server-qdrant` | `StatefulSet` (`triage-facets-mcp-0`) | **Sim** |

O proxy runner recebe, nos seus args, um `--k8s-pod-patch` que **é** a spec do
pod do backend — incluindo o `envFrom` do ConfigMap. O runner cria o StatefulSet
do backend a partir desse patch. Então:

- O `envFrom: [configMapRef: triage-facets-filterable-fields]` vive no
  **StatefulSet backend**, materializado no template dele (confirmável com
  `kubectl -n ai get statefulset triage-facets-mcp -o jsonpath='{.spec.template.spec.containers[*].envFrom}'`).
- Variável de ambiente via `envFrom` é resolvida **no start do container**.
  Mudar o ConfigMap não reinicia o pod nem reinjeta o valor.
- Reiniciar o **Deployment do runner** recria o proxy, mas **não** força o
  StatefulSet backend a reiniciar → a config antiga persiste. É o erro fácil de
  cometer: `kubectl get deploy` mostra `triage-facets-mcp`, você reinicia, e o
  filtro não muda.
- Reiniciar o **StatefulSet backend** faz o pod `-0` subir de novo e reler o
  ConfigMap → a config nova entra. **É este o alvo correto.**

## Caminhos automáticos investigados e descartados

Para não refazer a investigação: as opções de recarga automática foram testadas
no cluster (operator v0.30.0) e **nenhuma serve**, por motivo verificado — não por
falta de tentativa.

| Caminho | Resultado | Por quê |
|---|---|---|
| Anotação `mcpserver.toolhive.stacklok.dev/restarted-at` no MCPServer | **Não existe** | O CRD v0.30.0 não tem campo de restart (`kubectl explain mcpserver.spec --recursive` não lista nada de restart); anotar não recria o pod backend. |
| `spec.resourceOverrides` (labels/annotations) | **Só o proxy** | Cobre `proxyDeployment` e `proxyService` — o runner. Não há override de metadata para o StatefulSet backend. |
| Stakater Reloader | **Não instalado**; e mesmo instalado, sujo | Exigiria anotar o StatefulSet backend, que é **derivado** (criado via pod-patch, não declarado no Git). A anotação sobrevive à reconciliação, mas é estado imperativo fora do GitOps. `podTemplateSpec` põe anotação no POD, não no StatefulSet que o Reloader observa. |
| `MCPRemoteProxy` + Deployment próprio | **Over-engineering** | Funcionaria (backend sob controle total, anotável), mas custa dois workloads por MCP, desvio do padrão `MCPServer` que todos os outros seguem, e perda do `podTemplateSpec` integrado (seed-cache, volumes). Desproporcional para uma config que muda raramente. |

Conclusão: para uma config que muda raramente e sempre por ação consciente (via
PR), o restart explícito deste runbook é o método proporcional. Automatizar
custaria mais complexidade estrutural (ou dívida de GitOps) do que o problema
justifica.

> Se você viu `reloader.stakater.com/auto: "true"` em algum StatefulSet destes:
> é resíduo de um teste, **inócuo** (o Reloader não está instalado). Pode remover
> com `kubectl -n ai annotate statefulset/<nome> reloader.stakater.com/auto-`.

## Quando isto se aplica

Sempre que um `ConfigMap` consumido por `envFrom` de um MCP de memória mudar via
GitOps — hoje, os `triage-incidents-filterable-fields` e
`triage-facets-filterable-fields`. O ArgoCD sincroniza o ConfigMap sozinho; o que
**não** é automático é o restart do backend. Rode o comando do topo após o sync.

> Nota de versão: nesta versão do operator (proxyrunner/operator v0.30.0) o
> backend é um **StatefulSet**. A doc do CRD descreve o backend como `Deployment`
> em outras versões — se após um upgrade do ToolHive o `kubectl get statefulset`
> não listar o backend, verifique se ele passou a ser `Deployment` e ajuste o
> alvo do `rollout restart`.
