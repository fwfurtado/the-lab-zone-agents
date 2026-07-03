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
}

// Load lê a configuração de env. Erros de parsing abortam o boot — config
// inválida silenciosamente "corrigida" é pior que crash-loop com mensagem.
func Load() (Config, error) {
	cfg := Config{
		WebhookAddr:   getenv("WEBHOOK_ADDR", ":8080"),
		MetricsAddr:   getenv("METRICS_ADDR", ":9090"),
		CoreURL:       getenv("CORE_URL", "http://127.0.0.1:8081/triage"),
		CoreHealthURL: getenv("CORE_HEALTH_URL", "http://127.0.0.1:8081/healthz"),
	}

	var err error
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
