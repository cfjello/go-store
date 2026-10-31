package perf

import (
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stat is a point-in-time copy of one function's counters.
type Stat struct {
	Name  string        `json:"name"`
	Calls int64         `json:"calls"`
	Total time.Duration `json:"total"`
	Min   time.Duration `json:"min"`
	Max   time.Duration `json:"max"`
	Last  time.Duration `json:"last"`
}

// SortBy selects the ordering used by Top.
type SortBy int

const (
	ByTotal SortBy = iota
	ByCalls
)

type record struct {
	calls atomic.Int64
	total atomic.Int64
	min   atomic.Int64
	max   atomic.Int64
	last  atomic.Int64
}

var (
	enabled atomic.Bool
	regs    sync.Map // map[string]*record
	noop    = func() {}
)

func init() {
	SyncFromEnv()
}

// SyncFromEnv sets the enable flag from PERF_ENABLED. Call after util.SetEnv
// so DefaultEnv fallbacks (and .env files) are visible.
func SyncFromEnv() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("PERF_ENABLED")))
	SetEnabled(v == "true" || v == "1" || v == "yes")
}

// SetEnabled toggles recording at runtime. Disabled Track is a single atomic
// load plus a shared no-op closure.
func SetEnabled(on bool) {
	enabled.Store(on)
}

// Enabled reports whether recording is currently on.
func Enabled() bool {
	return enabled.Load()
}

// Track begins a timing sample for name and returns the completion function.
// Typical use:
//
//	func (s *Store) Set(args types.SetArgs) (types.MetaData, error) {
//	    defer perf.Track("store.Set")()
//	    ...
//	}
func Track(name string) func() {
	if !enabled.Load() {
		return noop
	}
	start := time.Now()
	rec := loadOrStore(name)
	return func() {
		rec.add(time.Since(start))
	}
}

func loadOrStore(name string) *record {
	if v, ok := regs.Load(name); ok {
		return v.(*record)
	}
	rec := &record{}
	actual, _ := regs.LoadOrStore(name, rec)
	return actual.(*record)
}

func (r *record) add(d time.Duration) {
	ns := int64(d)
	r.calls.Add(1)
	r.total.Add(ns)
	r.last.Store(ns)
	for {
		old := r.max.Load()
		if ns <= old || r.max.CompareAndSwap(old, ns) {
			break
		}
	}
	for {
		old := r.min.Load()
		if old != 0 && old <= ns {
			break
		}
		if r.min.CompareAndSwap(old, ns) {
			break
		}
	}
}

func (r *record) snapshot(name string) Stat {
	return Stat{
		Name:  name,
		Calls: r.calls.Load(),
		Total: time.Duration(r.total.Load()),
		Min:   time.Duration(r.min.Load()),
		Max:   time.Duration(r.max.Load()),
		Last:  time.Duration(r.last.Load()),
	}
}

func (r *record) reset() {
	r.calls.Store(0)
	r.total.Store(0)
	r.min.Store(0)
	r.max.Store(0)
	r.last.Store(0)
}

// Snapshot returns a copy of every recorded function, sorted by total time
// descending (name ascending as a tiebreaker).
func Snapshot() []Stat {
	stats := collect()
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Total != stats[j].Total {
			return stats[i].Total > stats[j].Total
		}
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// Top returns the n busiest functions according to by. n <= 0 yields nil.
func Top(n int, by SortBy) []Stat {
	if n <= 0 {
		return nil
	}
	stats := collect()
	sort.Slice(stats, func(i, j int) bool {
		switch by {
		case ByCalls:
			if stats[i].Calls != stats[j].Calls {
				return stats[i].Calls > stats[j].Calls
			}
		default:
			if stats[i].Total != stats[j].Total {
				return stats[i].Total > stats[j].Total
			}
		}
		return stats[i].Name < stats[j].Name
	})
	if n > len(stats) {
		n = len(stats)
	}
	return stats[:n]
}

// Reset zeros every counter. Recorded names are kept so subsequent snapshots
// still list them at zero.
func Reset() {
	regs.Range(func(_, v any) bool {
		v.(*record).reset()
		return true
	})
}

func collect() []Stat {
	stats := make([]Stat, 0)
	regs.Range(func(k, v any) bool {
		stats = append(stats, v.(*record).snapshot(k.(string)))
		return true
	})
	return stats
}
