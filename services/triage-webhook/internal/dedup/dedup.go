// Package dedup implementa a janela de deduplicação de webhooks.
//
// In-memory de propósito (ADR-0017): com 1 réplica e strategy Recreate, um
// mapa com TTL é o estado honesto. Perder a janela num restart custa, no pior
// caso, UMA triagem duplicada quando o Alertmanager reenviar — não vale uma
// dependência de rede (Valkey) nem a CNP que viria junto. A regra registrada:
// dedup migra para Valkey no dia em que replicas > 1, e não antes.
package dedup

import (
	"context"
	"sync"
	"time"
)

// Cache é a janela de dedup: chave → instante de expiração.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]time.Time
}

// New cria a janela com o TTL dado. now é injetável para teste (nil = time.Now).
func New(ttl time.Duration, now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	return &Cache{ttl: ttl, now: now, entries: make(map[string]time.Time)}
}

// Seen responde se a chave já foi vista dentro da janela e, se não, a registra.
// Check-and-insert atômico: duas requisições simultâneas com a mesma chave
// resultam em exatamente uma triagem.
func (c *Cache) Seen(key string) bool {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if deadline, ok := c.entries[key]; ok && now.Before(deadline) {
		return true
	}
	c.entries[key] = now.Add(c.ttl)
	return false
}

// Len devolve o tamanho atual da janela (para a métrica/gauge, se desejado).
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Sweep remove entradas expiradas periodicamente até o contexto encerrar.
// Sem isto o mapa só cresceria — Seen expira a chave consultada, mas chaves
// nunca mais consultadas ficariam para sempre.
func (c *Cache) Sweep(ctx context.Context) {
	interval := c.ttl / 4
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := c.now()
			c.mu.Lock()
			for k, deadline := range c.entries {
				if now.After(deadline) {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		}
	}
}

// Forget remove a chave da janela. Usado quando o registro em Seen precisa
// ser desfeito — ex.: a fila rejeitou o job depois do check de dedup; sem o
// Forget, o reenvio do Alertmanager bateria em "duplicate" sem nunca ter
// havido triagem.
func (c *Cache) Forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
