package transport

import (
	"testing"
	"time"
)

// BenchmarkBackoff_FailSuccess measures the cost of the backoff state machine
// over a realistic fail-then-recover cycle. Tracked over time to catch regressions
// in the hot push-loop path.
func BenchmarkBackoff_FailSuccess(b *testing.B) {
	for b.Loop() {
		bo := newBackoff(30*time.Second, 300*time.Second)
		bo.Fail()
		bo.Fail()
		bo.Success()
		bo.Success()
		bo.Success()
	}
}