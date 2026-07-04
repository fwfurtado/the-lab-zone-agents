// Package server monta os dois servidores HTTP da borda:
//
//   - webhook (WEBHOOK_ADDR): POST /webhook (Alertmanager), /healthz, /readyz
//   - métricas (METRICS_ADDR): /metrics (vmagent)
//
// Portas separadas de propósito: a CiliumNetworkPolicy libera cada origem
// apenas para a sua porta (Alertmanager → webhook; vmagent → métricas).
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/alertmanager"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/core"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/dedup"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/metrics"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/pipeline"
)

// Webhook monta o servidor que recebe o Alertmanager.
func Webhook(addr string, pool *pipeline.Pool, cache *dedup.Cache, coreClient *core.Client, m *metrics.Set, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		m.WebhooksReceived.Inc()

		payload, err := alertmanager.Parse(r.Body)
		if err != nil {
			m.WebhooksInvalid.Inc()
			log.Warn("payload inválido", "err", err)
			respond(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if payload.Version != "4" {
			log.Warn("versão de payload inesperada; seguindo mesmo assim", "version", payload.Version)
		}

		// send_resolved: false na rota já evita isto; defesa em profundidade.
		firing := payload.Firing()
		if len(firing) == 0 {
			respond(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "sem alertas firing"})
			return
		}

		key := payload.DedupKey()
		if cache.Seen(key) {
			m.DedupHits.Inc()
			log.Info("webhook deduplicado", "dedup_key", key, "group_key", payload.GroupKey)
			respond(w, http.StatusAccepted, map[string]string{"status": "duplicate", "dedup_key": key})
			return
		}

		job := pipeline.Job{
			DedupKey: key,
			GroupKey: payload.GroupKey,
			Summary:  payload.Summary(),
			Context:  payload.RenderContext(),
			Received: time.Now(),
		}
		if !pool.TryEnqueue(job) {
			// Fila cheia: 429 e o Alertmanager reenvia. IMPORTANTE: a chave já
			// entrou na janela de dedup no Seen acima — precisamos removê-la?
			// Não: Seen registrou, e o reenvio bateria em "duplicate" sem nunca
			// ter triado. Ver Forget abaixo.
			cache.Forget(key)
			log.Warn("fila cheia; webhook rejeitado", "dedup_key", key, "group_key", payload.GroupKey)
			respond(w, http.StatusTooManyRequests, map[string]string{"status": "queue_full"})
			return
		}

		log.Info("triagem enfileirada", "dedup_key", key, "group_key", payload.GroupKey, "alerts", len(firing))
		respond(w, http.StatusAccepted, map[string]string{"status": "accepted", "dedup_key": key})
	})

	// Liveness: o processo responde. Sem dependências — reiniciar a borda não
	// conserta o núcleo.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness: a borda só está pronta se o núcleo estiver de pé — receber
	// webhook sem conseguir triar seria aceitar trabalho para falhar.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := coreClient.Healthy(r.Context()); err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"status": "core indisponível", "error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
}

// Metrics monta o servidor de métricas.
func Metrics(addr string, reg *metrics.Registry) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg.Handler())
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Conexão já era; nada útil a fazer além de não entrar em pânico.
		_ = err
	}
}

// Shutdown encerra um servidor com o prazo dado (helper para o main).
func Shutdown(srv *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return srv.Shutdown(ctx)
}
