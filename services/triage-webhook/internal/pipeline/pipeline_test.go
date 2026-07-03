package pipeline

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/metrics"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/publish"
)

type fakeCore struct {
	calls   atomic.Int32
	block   chan struct{} // se não-nil, Triage bloqueia até fechar
	release chan struct{} // sinaliza que Triage começou
}

func (f *fakeCore) Triage(ctx context.Context, _ string) (string, error) {
	f.calls.Add(1)
	if f.release != nil {
		f.release <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "diagnóstico", nil
}

type countPublisher struct{ n atomic.Int32 }

func (c *countPublisher) Publish(context.Context, publish.Report) error {
	c.n.Add(1)
	return nil
}

func newTestPool(workers, queue int, core Triager, pub publish.Publisher) *Pool {
	m := metrics.NewSet(metrics.NewRegistry())
	return New(workers, queue, time.Minute, core, pub, m, slog.New(slog.NewTextHandler(discard{}, nil)))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestEnqueueBackpressure(t *testing.T) {
	core := &fakeCore{}
	pool := newTestPool(1, 2, core, &countPublisher{})
	// Sem Run: nada consome; a fila (cap 2) enche e a terceira é rejeitada.
	if !pool.TryEnqueue(Job{DedupKey: "a"}) || !pool.TryEnqueue(Job{DedupKey: "b"}) {
		t.Fatal("as duas primeiras deveriam enfileirar")
	}
	if pool.TryEnqueue(Job{DedupKey: "c"}) {
		t.Fatal("fila cheia deveria rejeitar")
	}
}

func TestRunProcessesAndPublishes(t *testing.T) {
	core := &fakeCore{}
	pub := &countPublisher{}
	pool := newTestPool(2, 4, core, pub)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pool.Run(ctx) }()

	for _, k := range []string{"a", "b", "c"} {
		if !pool.TryEnqueue(Job{DedupKey: k, Received: time.Now()}) {
			t.Fatalf("enqueue de %q falhou", k)
		}
	}

	deadline := time.After(5 * time.Second)
	for pub.n.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("esperava 3 publicações; veio %d", pub.n.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

func TestShutdownDrainsInFlightAndDropsQueued(t *testing.T) {
	core := &fakeCore{block: make(chan struct{}), release: make(chan struct{}, 1)}
	pub := &countPublisher{}
	pool := newTestPool(1, 4, core, pub)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pool.Run(ctx) }()

	if !pool.TryEnqueue(Job{DedupKey: "em-curso"}) {
		t.Fatal("enqueue falhou")
	}
	<-core.release // worker pegou o job e está bloqueado no núcleo

	// Enche a fila com jobs que serão descartados no shutdown.
	pool.TryEnqueue(Job{DedupKey: "fila-1"})
	pool.TryEnqueue(Job{DedupKey: "fila-2"})

	cancel()          // shutdown: worker deve TERMINAR o job em curso...
	close(core.block) // ...que o fake agora deixa concluir.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run não drenou")
	}

	if got := core.calls.Load(); got != 1 {
		t.Fatalf("só o job em curso deveria ter rodado; rodaram %d", got)
	}
	if got := pub.n.Load(); got != 1 {
		t.Fatalf("o job em curso deveria ter sido publicado; publicados %d", got)
	}
}
