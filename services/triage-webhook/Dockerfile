# Borda Go do agente de triagem (Fase B, ADR-0017).
# Contexto de build: services/triage-webhook (auto-contido; go.mod próprio).

FROM golang:1.23-alpine AS build

WORKDIR /src

# Camada de deps separada por disciplina; o módulo é stdlib-only hoje.
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/triage-webhook ./cmd/triage-webhook

# static-debian12 (não scratch): traz CA bundle e tzdata — a Fase B2 (publisher
# Slack) vai precisar de TLS e não queremos trocar de base no meio.
FROM gcr.io/distroless/static-debian12

LABEL org.opencontainers.image.title="the-lab-zone-triage-webhook" \
      org.opencontainers.image.authors="Mimi <fwfurtado@gmail.com>" \
      org.opencontainers.image.vendor="The Lab Zone" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/triage-webhook /usr/local/bin/triage-webhook

# Mesmo UID não-root do resto da frota.
USER 10001:10001

ENTRYPOINT ["/usr/local/bin/triage-webhook"]
