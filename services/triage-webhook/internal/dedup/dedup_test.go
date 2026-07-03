package dedup

import (
	"testing"
	"time"
)

func TestSeenRespectsTTL(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	c := New(time.Hour, clock)

	if c.Seen("k") {
		t.Fatal("primeira vez não pode ser 'seen'")
	}
	if !c.Seen("k") {
		t.Fatal("dentro da janela deve ser 'seen'")
	}

	now = now.Add(2 * time.Hour)
	if c.Seen("k") {
		t.Fatal("depois do TTL a chave expirou; não pode ser 'seen'")
	}
}

func TestForgetUndoesSeen(t *testing.T) {
	c := New(time.Hour, nil)
	_ = c.Seen("k")
	c.Forget("k")
	if c.Seen("k") {
		t.Fatal("após Forget a chave não pode constar na janela")
	}
}
