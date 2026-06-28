package buffer

import (
	"fmt"
	"testing"
	"time"
)

func newBenchBuffer(b *testing.B, maxMB int, maxAge time.Duration) *Buffer {
	b.Helper()
	buf, err := New(b.TempDir(), maxMB, maxAge)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	return buf
}

// BenchmarkBuffer_Store measures the cost of a single atomic write (write+rename+evict).
func BenchmarkBuffer_Store(b *testing.B) {
	buf := newBenchBuffer(b, 50, time.Hour)
	payload := []byte(`{"agent_id":"bench","points":[{"metric":"cpu.percent","value":42.1,"labels":{},"ts":1750000000}]}`)
	b.ResetTimer()
	for b.Loop() {
		if err := buf.Store(Entry{Kind: KindMetrics, Payload: payload}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuffer_StoreAndAck measures the store+next+ack round-trip that the
// push loop executes on every successful buffer drain.
func BenchmarkBuffer_StoreAndAck(b *testing.B) {
	buf := newBenchBuffer(b, 50, time.Hour)
	n := 0
	b.ResetTimer()
	for b.Loop() {
		n++
		payload := fmt.Appendf(nil, `{"i":%d}`, n)
		if err := buf.Store(Entry{Kind: KindMetrics, Payload: payload}); err != nil {
			b.Fatal(err)
		}
		e, err := buf.Next()
		if err != nil {
			b.Fatal(err)
		}
		if e != nil {
			if err := e.Ack(); err != nil {
				b.Fatal(err)
			}
		}
	}
}