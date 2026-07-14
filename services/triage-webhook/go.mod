module github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook

go 1.26

// Fase D: persistência da triagem no Garage (object store S3-compatível).
// EMENDA o ADR-0003 (borda Go stdlib-only). A versão abaixo é um piso razoável;
// rode `go get github.com/minio/minio-go/v7@latest` na sua máquina para fixar a
// versão exata e gerar o go.sum (o sandbox de geração deste patch não alcança o
// proxy de módulos Go).
require github.com/minio/minio-go/v7 v7.0.70

// Inversão da observabilidade: OTel na borda (a borda enraíza o trace e propaga
// contexto W3C ao núcleo) e Pyroscope para profiling contínuo. EMENDA o ADR-0003
// (stdlib-only -> deps justificadas).
// As versões abaixo são um PISO; rode `go mod tidy` (ou `go get <mods>@latest`)
// na sua máquina para fixar as versões exatas e gerar/atualizar o go.sum — o
// sandbox que gera este patch não alcança o proxy de módulos Go.
require (
	github.com/grafana/pyroscope-go v1.4.1
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grafana/pyroscope-go/godeltaprof v0.1.11 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.2 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/rs/xid v1.5.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.1 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250825161204-c5933d9347a5 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250825161204-c5933d9347a5 // indirect
	google.golang.org/grpc v1.75.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
)
