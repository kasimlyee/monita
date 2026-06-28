package metrics

import (
	"context"
	"testing"
	"time"
)

// BenchmarkCollector_Sample measures the cost of one full metrics sample across
// all enabled categories. Tracks allocations-per-op — the label-map caching in
// Collector is specifically designed to keep this low across repeated ticks.
func BenchmarkCollector_Sample(b *testing.B) {
	c := New([]string{"cpu", "memory", "disk", "load", "network"}, 10*time.Second)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.sample(ctx); err != nil {
			b.Fatal(err)
		}
	}
}