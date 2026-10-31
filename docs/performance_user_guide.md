# Performance Monitoring

`pkg/perf` is a shallow, function-level monitor for the hottest paths in the
store, job engine, and SQLite layer. It answers two questions in a running
process:

1. **Most expensive** — which of our own functions consume the most total wall-clock time?
2. **Most active** — which functions run most often?

It is *not* a substitute for `runtime/pprof` or `net/http/pprof`. Those remain
the tools for CPU, heap, and goroutine profiles. `pkg/perf` is a cheap set of
atomic counters you can leave in production.

The design notes live in [performance.md](performance.md).

## Enable it

Monitoring is off by default. Set `PERF_ENABLED` before the process starts
(or before `util.SetEnv` / `server.NewServer`):

```bash
PERF_ENABLED=true
PERF_REPORT_INTERVAL_MS=60000   # 0 disables the periodic log dump
PERF_REPORT_TOP_N=10
```

Accepted truthy values for `PERF_ENABLED`: `true`, `1`, `yes` (any case).

The HTTP server calls `perf.SyncFromEnv()` after applying `DefaultEnv`, then
`perf.StartReporter()`. Tests and libraries can toggle recording at runtime
with `perf.SetEnabled(true)` without touching the environment.

## Overhead

| State | Cost per instrumented call |
|---|---|
| Disabled | one atomic load + a shared no-op closure (~1 ns, no allocation) |
| Enabled | two clock reads, one map lookup, a handful of atomic adds (~50–100 ns) |

Instrumented functions are SQL / JSON / graph-scale, so enabled overhead stays
well under 1%.

## Reading the numbers

### Snapshot API

```go
import "github.com/cfjello/go-store/pkg/perf"

stats := perf.Snapshot()          // all functions, sorted by total time
top := perf.Top(10, perf.ByCalls) // busiest by call count
perf.Reset()                      // zero counters (names are kept)
```

Each `perf.Stat` has `Name`, `Calls`, `Total`, `Min`, `Max`, and `Last`
(`time.Duration`; JSON encodes them as nanoseconds).

### Periodic log dump

When enabled and `PERF_REPORT_INTERVAL_MS` is greater than zero, a background
ticker logs the top N functions by total time and by call count through the
standard `log` package (the same stream as `logs/go-store.log` for `cmd/api`):

```
perf: top by total: store.Set calls=12 total=15ms min=100µs max=2ms last=200µs; ...
perf: top by calls: store.GetMeta calls=40 total=8ms min=50µs max=400µs last=60µs; ...
```

### HTTP endpoint

`GET /perf/stats` is registered next to `/health` **only when monitoring is
enabled**. It returns `perf.Snapshot()` as JSON.

```bash
curl http://localhost:9090/perf/stats
```

## What is instrumented

Names are stable strings of the form `<package>.<Method>`.

| Name | Package |
|---|---|
| `store.Set`, `store.Register`, `store.Get`, `store.Publish` | `pkg/store` |
| `store.GetMeta`, `store.SetMeta`, `store.ValidateCached`, `store.notifyChange` | `pkg/store` |
| `job.DepsGraph`, `job.RunGraph`, `job.ExecRunGraph`, `job.Notify`, `job.runOnce` | `pkg/job` |
| `db.SetData`, `db.UpsertData`, `db.GetData`, `db.GetMeta`, `db.SetMeta`, `db.GetCurrStoreID` | `internal/database` |

Comparing `store.*` against `db.*` timings shows store-layer overhead on top of
raw SQL. Comparing `job.runOnce` against `job.ExecRunGraph` shows graph-rebuild
cost versus execution (including user `updMethod` time).

## Instrumenting your own code

```go
func (o *MyObject) SetData() error {
    defer perf.Track("MyObject.SetData")()
    // ...
    return nil
}
```

Keep names unique and stable. Do not put `Track` inside tight numeric loops.

## Non-goals

- CPU, heap, or goroutine profiling — use `runtime/pprof`.
- Distributed tracing or span trees.
- Per-store-key labels (a later opt-in could add them without changing `Track`).
