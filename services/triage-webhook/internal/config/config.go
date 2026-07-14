// Package config carrega a configuração do triage-webhook a partir de env.
//
// Regras da casa: nenhum trabalho em init(), parsing estrito (valor inválido é
// erro, não default silencioso), e defaults que funcionam no pod sidecar sem
// nenhuma env obrigatória além do que o Deployment injeta.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config é a configuração completa do serviço. Os defaults descrevem o
// contrato do pod sidecar (ADR-0017): o núcleo Python escuta em localhost.
type Config struct {
	// WebhookAddr é onde o servidor HTTP do webhook escuta (Alertmanager → cá).
	WebhookAddr string
	// MetricsAddr é onde /metrics escuta (vmagent → cá). Porta separada do
	// webhook de propósito: a CNP libera cada origem só para a sua porta.
	MetricsAddr string
	// CoreURL é o endpoint POST /triage do núcleo Python, no mesmo pod.
	CoreURL string
	// CoreHealthURL é o healthcheck do núcleo, usado pelo /readyz da borda.
	CoreHealthURL string
	// Workers é o teto de triagens concorrentes — o teto de custo LLM.
	Workers int
	// QueueSize é a capacidade da fila; cheia → 429 (o Alertmanager reenvia).
	QueueSize int
	// DedupTTL é a janela de deduplicação por grupo de alertas. Deve ser MENOR
	// que o repeat_interval da rota de triagem no Alertmanager, senão o reenvio
	// periódico nunca gera re-triagem (ver ADR-0017).
	DedupTTL time.Duration
	// TriageTimeout é o teto de uma triagem (chamada ao núcleo). O
	// terminationGracePeriodSeconds do pod deve ser MAIOR que isto para o
	// shutdown conseguir drenar a triagem em curso.
	TriageTimeout time.Duration

	// SlackToken é o bot token (xoxb-...) para postar o diagnóstico. Vazio
	// desliga o SlackPublisher — o serviço segue publicando só no log.
	SlackToken string
	// SlackChannel é o canal/ID onde o diagnóstico é postado (ex.: #triage).
	SlackChannel string
	// AlertmanagerURL é a base pública do Alertmanager, usada no link "ver
	// alerta" da mensagem do Slack. Vazio omite o link.
	AlertmanagerURL string

	// Garage* configuram a persistência da triagem no object store
	// S3-compatível (Fase D). GarageEndpoint vazio DESLIGA o GaragePublisher —
	// o serviço segue publicando no log/Slack, sem persistir. Manter a
	// persistência opcional evita acoplar o boot da borda ao object store.
	GarageEndpoint  string
	GarageAccessKey string
	GarageSecretKey string
	GarageBucket    string
	GarageRegion    string
	GarageUseSSL    bool

	// OTel* configuram o tracing (inversão da observabilidade de IA). A borda
	// ENRAÍZA o trace e propaga contexto W3C ao núcleo Python. Endpoint e
	// protocolo do exporter vêm das envs padrão OTEL_EXPORTER_OTLP_*.
	OTelEnabled     bool   // OTEL_ENABLED (default true); false desliga o tracing
	OTelServiceName string // OTEL_SERVICE_NAME (default triage-webhook)
	OTelNamespace   string // OTEL_NAMESPACE (default local para CI/dev)
	OTelEnvironment string // OTEL_ENVIRONMENT (default prod)

	// Pyroscope* configuram continuous profiling. O service.name usado nos
	// traces também é usado como application/profile label para permitir
	// correlação trace -> profile no Grafana.
	PyroscopeEnabled       bool   // PYROSCOPE_ENABLED (default true)
	PyroscopeServerAddress string // PYROSCOPE_SERVER_ADDRESS
}

// Load lê a configuração de env. Erros de parsing abortam o boot — config
// inválida silenciosamente "corrigida" é pior que crash-loop com mensagem.
func Load() (Config, error) {
	cfg := Config{
		WebhookAddr:     getenv("WEBHOOK_ADDR", ":8080"),
		MetricsAddr:     getenv("METRICS_ADDR", ":9090"),
		CoreURL:         getenv("CORE_URL", "http://127.0.0.1:8081/triage"),
		CoreHealthURL:   getenv("CORE_HEALTH_URL", "http://127.0.0.1:8081/healthz"),
		SlackToken:      os.Getenv("SLACK_BOT_TOKEN"),
		SlackChannel:    getenv("SLACK_CHANNEL", "#triage"),
		AlertmanagerURL: os.Getenv("ALERTMANAGER_URL"),
		GarageEndpoint:  os.Getenv("GARAGE_ENDPOINT"),
		GarageAccessKey: os.Getenv("GARAGE_ACCESS_KEY"),
		GarageSecretKey: os.Getenv("GARAGE_SECRET_KEY"),
		GarageBucket:    getenv("GARAGE_BUCKET", "the-lab-zone-triage"),
		GarageRegion:    getenv("GARAGE_REGION", "garage"),
		OTelServiceName: getenv("OTEL_SERVICE_NAME", "triage-webhook"),
		OTelNamespace:   getenv("OTEL_NAMESPACE", "local"),
		OTelEnvironment: getenv("OTEL_ENVIRONMENT", "prod"),
		PyroscopeServerAddress: getenv(
			"PYROSCOPE_SERVER_ADDRESS",
			"http://pyroscope.observability.svc.cluster.local:4040",
		),
	}

	var err error
	if cfg.GarageUseSSL, err = getbool("GARAGE_USE_SSL", false); err != nil {
		return Config{}, err
	}
	if cfg.OTelEnabled, err = getbool("OTEL_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.PyroscopeEnabled, err = getbool("PYROSCOPE_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.Workers, err = getint("WORKERS", 2); err != nil {
		return Config{}, err
	}
	if cfg.QueueSize, err = getint("QUEUE_SIZE", 16); err != nil {
		return Config{}, err
	}
	if cfg.DedupTTL, err = getdur("DEDUP_TTL", 6*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.TriageTimeout, err = getdur("TRIAGE_TIMEOUT", 10*time.Minute); err != nil {
		return Config{}, err
	}

	if cfg.Workers < 1 {
		return Config{}, fmt.Errorf("WORKERS deve ser >= 1, veio %d", cfg.Workers)
	}
	if cfg.QueueSize < 1 {
		return Config{}, fmt.Errorf("QUEUE_SIZE deve ser >= 1, veio %d", cfg.QueueSize)
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s inválido (%q): %w", key, v, err)
	}
	return n, nil
}

func getbool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s inválido (%q): %w", key, v, err)
	}
	return b, nil
}

func getdur(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s inválido (%q): %w", key, v, err)
	}
	return d, nil
}
