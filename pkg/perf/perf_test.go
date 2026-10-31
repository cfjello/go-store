package perf

import (
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	SetEnabled(false)
	Reset()
	m.Run()
}

func TestTrackDisabledRecordsNothing(t *testing.T) {
	SetEnabled(false)
	Reset()
	Track("disabled.fn")()
	if got := Snapshot(); len(got) != 0 && callsOf(got, "disabled.fn") != 0 {
		t.Fatalf("disabled Track should not increment counters, got %+v", got)
	}
}

func TestTrackIncrementsAndBounds(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	Reset()

	done := Track("unit.fn")
	time.Sleep(1 * time.Millisecond)
	done()
	done = Track("unit.fn")
	time.Sleep(2 * time.Millisecond)
	done()

	stats := Snapshot()
	s := findStat(stats, "unit.fn")
	if s.Calls != 2 {
		t.Fatalf("calls: got %d want 2", s.Calls)
	}
	if s.Total <= 0 {
		t.Fatal("total should be > 0")
	}
	if s.Min <= 0 || s.Max <= 0 || s.Last <= 0 {
		t.Fatalf("min/max/last should be > 0, got %+v", s)
	}
	if s.Min > s.Max {
		t.Fatalf("min %v > max %v", s.Min, s.Max)
	}
	if s.Total < s.Max {
		t.Fatalf("total %v < max %v", s.Total, s.Max)
	}
}

func TestSnapshotSortedByTotal(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	Reset()

	Track("fast")()
	done := Track("slow")
	time.Sleep(3 * time.Millisecond)
	done()

	stats := Snapshot()
	if len(stats) < 2 {
		t.Fatalf("expected at least 2 stats, got %d", len(stats))
	}
	if stats[0].Name != "slow" {
		t.Fatalf("first by total should be slow, got %q", stats[0].Name)
	}
}

func TestTopByCalls(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	Reset()

	Track("once")()
	Track("twice")()
	Track("twice")()

	top := Top(1, ByCalls)
	if len(top) != 1 || top[0].Name != "twice" || top[0].Calls != 2 {
		t.Fatalf("Top by calls: %+v", top)
	}
	if Top(0, ByCalls) != nil {
		t.Fatal("Top(0) should be nil")
	}
}

func TestResetZerosCounters(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	Reset()
	Track("reset.fn")()
	Reset()
	s := findStat(Snapshot(), "reset.fn")
	if s.Calls != 0 || s.Total != 0 || s.Min != 0 || s.Max != 0 || s.Last != 0 {
		t.Fatalf("Reset should zero counters, got %+v", s)
	}
}

func TestTrackConcurrent(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		Reset()
	})
	Reset()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			Track("conc.fn")()
		}()
	}
	wg.Wait()
	s := findStat(Snapshot(), "conc.fn")
	if s.Calls != n {
		t.Fatalf("calls: got %d want %d", s.Calls, n)
	}
}

func TestStartReporterNoopWhenDisabled(t *testing.T) {
	SetEnabled(false)
	stop := StartReporter()
	stop()
}

func findStat(stats []Stat, name string) Stat {
	for _, s := range stats {
		if s.Name == name {
			return s
		}
	}
	return Stat{Name: name}
}

func callsOf(stats []Stat, name string) int64 {
	return findStat(stats, name).Calls
}
