package logs

import "testing"

var benchLines = []string{
	`level=error msg="connection refused" host="db.internal" port=5432`,
	`{"level":"warn","msg":"retry","attempt":3,"err":"timeout"}`,
	`[ERROR] 2026-06-28T12:00:00Z disk usage above threshold`,
	`plain log line with no level marker at all`,
}

func BenchmarkExtractLevel(b *testing.B) {
	for b.Loop() {
		for _, line := range benchLines {
			ExtractLevel(line)
		}
	}
}

func BenchmarkPassesFilter(b *testing.B) {
	for b.Loop() {
		for _, line := range benchLines {
			PassesFilter(line, "warn")
		}
	}
}

func BenchmarkCoalescer_Add(b *testing.B) {
	entries := make([]Entry, 64)
	for i := range entries {
		entries[i] = Entry{Source: "/var/log/app.log", Message: "connection refused", Count: 1}
	}
	b.ResetTimer()
	for b.Loop() {
		var c Coalescer
		for _, e := range entries {
			c.Add(e)
		}
		c.Flush()
	}
}