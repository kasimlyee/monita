package redact

import "testing"

var benchInputs = []string{
	`normal log line with no secrets at all`,
	`api_key = "abc123defgh456ijklmn" request successful`,
	`Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig`,
	`password="hunter2secret" user=alice action=login`,
	`AKIA1234567890ABCDEF accessed bucket my-data`,
	`SECRET=xK9mP2qR7vL4nJ8wA1bC config loaded`,
}

// BenchmarkRedact tracks allocations-per-op on the hot log processing path.
// The redaction pipeline runs on every log line before it touches the buffer.
func BenchmarkRedact(b *testing.B) {
	r, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, line := range benchInputs {
			r.Redact(line)
		}
	}
}