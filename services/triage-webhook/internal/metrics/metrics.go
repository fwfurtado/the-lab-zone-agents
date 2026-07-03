// Package metrics implementa as métricas do serviço no formato de exposição
// texto do Prometheus, sem dependências externas.
//
// Por que in-tree e não client_golang: o serviço é stdlib-only de propósito —
// binário estático mínimo, zero churn de dependências. Para meia dúzia de
// séries sem labels dinâmicos, o formato de exposição é pequeno e estável
// (counter/gauge/histogram cumulativo + le=+Inf). Se o serviço crescer para
// labels dinâmicos ou exemplars, trocar por client_golang é mecânico: este
// pacote é o único ponto de contato.
package metrics

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

// Counter é um contador monotônico.
type Counter struct {
	name, help string
	v          atomic.Uint64
}

// Inc incrementa o contador em 1.
func (c *Counter) Inc() { c.v.Add(1) }

// Add incrementa o contador em n.
func (c *Counter) Add(n uint64) { c.v.Add(n) }

// Gauge é um valor instantâneo (inteiro — profundidade de fila, workers ocupados).
type Gauge struct {
	name, help string
	v          atomic.Int64
}

// Set define o valor do gauge.
func (g *Gauge) Set(n int64) { g.v.Store(n) }

// Inc incrementa o gauge em 1.
func (g *Gauge) Inc() { g.v.Add(1) }

// Dec decrementa o gauge em 1.
func (g *Gauge) Dec() { g.v.Add(-1) }

// Histogram é um histograma cumulativo de buckets fixos.
type Histogram struct {
	name, help string
	bounds     []float64 // limites superiores, ordenados
	counts     []atomic.Uint64
	sumBits    atomic.Uint64 // float64 via math.Float64bits (CAS)
	count      atomic.Uint64
}

// Observe registra uma observação (em segundos, por convenção _seconds).
func (h *Histogram) Observe(v float64) {
	for i, b := range h.bounds {
		if v <= b {
			h.counts[i].Add(1)
		}
	}
	h.count.Add(1)
	for {
		old := h.sumBits.Load()
		next := math.Float64bits(math.Float64frombits(old) + v)
		if h.sumBits.CompareAndSwap(old, next) {
			return
		}
	}
}

// Registry agrega as métricas e sabe se serializar no formato Prometheus.
type Registry struct {
	counters   []*Counter
	gauges     []*Gauge
	histograms []*Histogram
}

// NewRegistry cria um registry vazio.
func NewRegistry() *Registry { return &Registry{} }

// NewCounter registra e devolve um novo Counter.
func (r *Registry) NewCounter(name, help string) *Counter {
	c := &Counter{name: name, help: help}
	r.counters = append(r.counters, c)
	return c
}

// NewGauge registra e devolve um novo Gauge.
func (r *Registry) NewGauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	r.gauges = append(r.gauges, g)
	return g
}

// NewHistogram registra e devolve um novo Histogram com os buckets dados.
func (r *Registry) NewHistogram(name, help string, bounds []float64) *Histogram {
	sorted := append([]float64(nil), bounds...)
	sort.Float64s(sorted)
	h := &Histogram{
		name:   name,
		help:   help,
		bounds: sorted,
		counts: make([]atomic.Uint64, len(sorted)),
	}
	r.histograms = append(r.histograms, h)
	return h
}

// WriteTo serializa todas as métricas no formato de exposição texto.
func (r *Registry) WriteTo(w *strings.Builder) {
	for _, c := range r.counters {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", c.name, c.help, c.name, c.name, c.v.Load())
	}
	for _, g := range r.gauges {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", g.name, g.help, g.name, g.name, g.v.Load())
	}
	for _, h := range r.histograms {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
		for i, b := range h.bounds {
			fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", h.name, formatBound(b), h.counts[i].Load())
		}
		total := h.count.Load()
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, total)
		fmt.Fprintf(w, "%s_sum %s\n", h.name, strconv.FormatFloat(math.Float64frombits(h.sumBits.Load()), 'g', -1, 64))
		fmt.Fprintf(w, "%s_count %d\n", h.name, total)
	}
}

// Handler devolve o http.Handler de /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var sb strings.Builder
		r.WriteTo(&sb)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(sb.String()))
	})
}

func formatBound(b float64) string {
	return strconv.FormatFloat(b, 'g', -1, 64)
}

// Set é o conjunto de métricas do triage-webhook. Nomes com prefixo
// triage_webhook_ para não colidir com as lab_agent_* do núcleo Python.
type Set struct {
	WebhooksReceived  *Counter
	WebhooksInvalid   *Counter
	DedupHits         *Counter
	QueueRejected     *Counter
	JobsEnqueued      *Counter
	JobsDroppedOnStop *Counter
	RunsTotal         *Counter
	RunErrors         *Counter
	QueueDepth        *Gauge
	WorkersBusy       *Gauge
	RunDuration       *Histogram
}

// NewSet registra o conjunto padrão de métricas no registry dado.
func NewSet(r *Registry) *Set {
	return &Set{
		WebhooksReceived:  r.NewCounter("triage_webhook_received_total", "Webhooks recebidos do Alertmanager."),
		WebhooksInvalid:   r.NewCounter("triage_webhook_invalid_total", "Webhooks rejeitados por payload inválido."),
		DedupHits:         r.NewCounter("triage_webhook_dedup_hits_total", "Webhooks descartados por deduplicação (janela TTL)."),
		QueueRejected:     r.NewCounter("triage_webhook_queue_rejected_total", "Webhooks rejeitados com 429 por fila cheia."),
		JobsEnqueued:      r.NewCounter("triage_webhook_jobs_enqueued_total", "Triagens enfileiradas."),
		JobsDroppedOnStop: r.NewCounter("triage_webhook_jobs_dropped_on_shutdown_total", "Triagens descartadas da fila no shutdown."),
		RunsTotal:         r.NewCounter("triage_webhook_runs_total", "Triagens executadas (com ou sem sucesso)."),
		RunErrors:         r.NewCounter("triage_webhook_run_errors_total", "Triagens que falharam (erro do núcleo ou timeout)."),
		QueueDepth:        r.NewGauge("triage_webhook_queue_depth", "Triagens aguardando na fila."),
		WorkersBusy:       r.NewGauge("triage_webhook_workers_busy", "Workers executando triagem neste instante."),
		RunDuration:       r.NewHistogram("triage_webhook_run_duration_seconds", "Duração da triagem, do dequeue ao relatório.", []float64{5, 15, 30, 60, 120, 300, 600}),
	}
}
