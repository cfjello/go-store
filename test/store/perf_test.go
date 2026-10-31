package store_test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/types"
)

func TestPerfCountersOnSet(t *testing.T) {
	perf.Reset()
	perf.SetEnabled(true)
	t.Cleanup(func() {
		perf.SetEnabled(false)
		perf.Reset()
	})

	key := keyOf(t)
	obj := &staticPlain{N: 1}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Init: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Set(types.SetArgs{Key: key, Object: obj}); err != nil {
		t.Fatal(err)
	}

	stats := perf.Snapshot()
	if n := perfCalls(stats, "store.Set"); n < 1 {
		t.Fatalf("store.Set calls=%d, stats=%+v", n, stats)
	}
	if n := perfCalls(stats, "db.SetData"); n < 1 {
		t.Fatalf("db.SetData calls=%d, stats=%+v", n, stats)
	}
}

func perfCalls(stats []perf.Stat, name string) int64 {
	for _, s := range stats {
		if s.Name == name {
			return s.Calls
		}
	}
	return 0
}
