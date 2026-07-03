package metrics

import (
	"strings"
	"testing"
)

func TestExpositionFormat(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("test_total", "contador de teste")
	g := r.NewGauge("test_depth", "gauge de teste")
	h := r.NewHistogram("test_seconds", "histograma de teste", []float64{1, 5})

	c.Inc()
	c.Add(2)
	g.Set(7)
	h.Observe(0.5) // cai em le=1 e le=5
	h.Observe(3)   // cai só em le=5
	h.Observe(10)  // só em +Inf

	var sb strings.Builder
	r.WriteTo(&sb)
	out := sb.String()

	for _, want := range []string{
		"# TYPE test_total counter",
		"test_total 3",
		"# TYPE test_depth gauge",
		"test_depth 7",
		"# TYPE test_seconds histogram",
		`test_seconds_bucket{le="1"} 1`,
		`test_seconds_bucket{le="5"} 2`,
		`test_seconds_bucket{le="+Inf"} 3`,
		"test_seconds_sum 13.5",
		"test_seconds_count 3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exposição não contém %q:\n%s", want, out)
		}
	}
}
