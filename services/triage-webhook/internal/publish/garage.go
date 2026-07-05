package publish

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// GaragePublisher persiste o relatório de triagem num object store
// S3-compatível (Garage) como fonte de verdade imutável da memória de triagens
// (Fase D). É o caminho de ESCRITA; o job de embed (Qdrant) lerá daqui depois.
//
// Best-effort por contrato do MultiPublisher: um PUT que falha é logado e
// devolvido como erro, mas NÃO derruba os outros publishers nem a triagem — a
// memória degrada em silêncio, a triagem sempre publica no Slack/log. Este é o
// ponto do ADR-0016 respeitado: object PUT fora do caminho crítico, sem
// framework, degradando bem.
//
// ADR-0003 (borda Go stdlib-only) é EMENDADO por esta dependência: o cliente S3
// minio-go substitui a autenticação SigV4 artesanal. A troca — acoplar a uma
// lib S3 estável em vez de manter código de assinatura HMAC à mão — está
// registrada no ADR da Fase D.
type GaragePublisher struct {
	client *minio.Client
	bucket string
	log    *slog.Logger
}

// GarageConfig são os parâmetros de conexão ao object store.
type GarageConfig struct {
	Endpoint  string // host:port do Garage (sem esquema)
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string // Garage aceita qualquer região; default "garage"
	UseSSL    bool
}

// NewGarage cria o publisher. Erro só na configuração do cliente (credenciais/
// endpoint malformados) — a existência do bucket e a conectividade são
// verificadas no primeiro Publish, não no boot, para não acoplar o start da
// borda à disponibilidade do object store.
func NewGarage(cfg GarageConfig, log *slog.Logger) (*GaragePublisher, error) {
	region := cfg.Region
	if region == "" {
		region = "garage"
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("configurando cliente Garage: %w", err)
	}
	return &GaragePublisher{client: client, bucket: cfg.Bucket, log: log}, nil
}

// Publish grava o documento (front-matter + corpo) no bucket, sob a chave
// derivada do incidente. PUT idempotente: mesmo incidente (mesmo dedupKey +
// firedAt) → mesma chave → sobrescreve, sem poluir o corpus com duplicatas.
func (p *GaragePublisher) Publish(ctx context.Context, r Report) error {
	key := objectKey(r)
	doc := buildDocument(r)
	body := bytes.NewReader([]byte(doc))

	_, err := p.client.PutObject(ctx, p.bucket, key, body, int64(len(doc)), minio.PutObjectOptions{
		ContentType: "text/markdown; charset=utf-8",
		// Metadados de objeto espelham os campos de correlação para inspeção
		// via cabeçalho, sem precisar abrir o corpo.
		UserMetadata: map[string]string{
			"dedup-key": r.DedupKey,
			"namespace": r.Namespace,
		},
	})
	if err != nil {
		p.log.Error("persistência no Garage falhou",
			"dedup_key", r.DedupKey, "bucket", p.bucket, "key", key, "err", err)
		return fmt.Errorf("persistindo triagem no Garage (%s): %w", key, err)
	}

	p.log.Info("triagem persistida no Garage",
		"dedup_key", r.DedupKey, "bucket", p.bucket, "key", key, "bytes", len(doc))
	return nil
}
