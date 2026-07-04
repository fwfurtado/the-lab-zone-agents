---
tipo: adr
numero: 5
titulo: CI unificado — contrato just, detecção por path, semver por serviço
status: aceito
relacionado: [0001-monorepo-servicos-irmaos, the-lab-zone-dockerfiles]
---

# ADR-0005 — CI unificado: contrato `just`, detecção por path, semver por serviço

## Status
Aceito. Workflow `.github/workflows/ci.yml`.

## Contexto
Após a reorganização em monorepo (ADR-0001), os dois workflows separados
(um por serviço, ambos `workflow_dispatch`-only, um deles sem etapa de teste)
eram assimétricos e frágeis. Um deles não disparava em push — o que causou um
incidente real: uma imagem Python mergeada nunca rebuildou sozinha, e o pod
subiu com código velho (crashloop). O padrão desejado já existia no repositório
irmão `the-lab-zone-dockerfiles`: versionamento independente por serviço.

## Decisão
Um único workflow com:

- **Detecção por path** (`dorny/paths-filter`) → matrix dinâmica: só builda o
  serviço cujos arquivos em `services/<nome>/**` mudaram. (Preferido à
  aritmética `HEAD~N` do repositório irmão, que é frágil em force-push/merge.)
- **Contrato `just`**: cada serviço passa por `just lint` e `just test` antes
  do build. O setup difere por linguagem (uv para o core; Go + golangci-lint
  para a borda), mas a execução é uniforme. Adicionar um serviço não muda a
  lógica do CI — só acrescenta um filtro de path e um bloco de setup.
- **Versionamento independente**: `tag-prefix: "<serviço>-v"` → cada serviço
  tem sua própria série de tags git e seu semver, dirigido por conventional
  commits (`feat(core):` sobe só o core).
- `lint` **falha** o pipeline (gate de qualidade); `fmt` é uso local
  (efeito colateral, fora do CI).

### Assimetria consciente no invocador do lint
Go tem uma action oficial de primeira classe (`golangci-lint-action`), que É o
runner (instala + roda + annotations inline no PR). Python não tem equivalente.
Então o **invocador** difere — a action roda o lint Go no CI; `just lint` roda
ruff+mypy no core — mas a **config é compartilhada**: a action e o `just lint`
local do Go leem o mesmo `.golangci.yml`. Simetria no que importa (regras,
resultado), assimetria só no mecanismo.

## Consequências
- Fix de infra de CI (que toca só o workflow, não `services/**`) NÃO dispara o
  build dos serviços — pelo próprio filtro de path. Valida-se via
  `workflow_dispatch` com `force_service` (embutido para isso). Regra: mudou
  código do serviço → automático; mudou o CI → dispara manual.
- Acoplamento golangci-lint ↔ toolchain Go: o linter só analisa código cuja
  versão de Go (go.mod) seja `<=` à versão com que ELE foi compilado. Subir o
  `go` directive obriga subir o golangci-lint junto (no `ci.yml` e no
  `mise.toml`). Imposto recorrente, documentado no `mise.toml`.
- Pinar versão explícita do linter (não `latest`) mantém builds reproduzíveis,
  ao custo desse ritual de manutenção acoplada. Trade-off consciente.
