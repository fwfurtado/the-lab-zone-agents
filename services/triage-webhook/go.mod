module github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook

go 1.26

// Fase D: persistência da triagem no Garage (object store S3-compatível).
// EMENDA o ADR-0003 (borda Go stdlib-only). A versão abaixo é um piso razoável;
// rode `go get github.com/minio/minio-go/v7@latest` na sua máquina para fixar a
// versão exata e gerar o go.sum (o sandbox de geração deste patch não alcança o
// proxy de módulos Go).
require github.com/minio/minio-go/v7 v7.0.70

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/stretchr/testify v1.7.0 // indirect
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
)
