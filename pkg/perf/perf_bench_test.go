package perf

import "testing"

func BenchmarkTrackDisabled(b *testing.B) {
	SetEnabled(false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Track("bench.disabled")()
	}
}

func BenchmarkTrackEnabled(b *testing.B) {
	SetEnabled(true)
	b.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Track("bench.enabled")()
	}
}
