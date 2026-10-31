# Performance Monitoring

A shallow, application-level monitoring package (`pkg/perf`) that answers two
questions cheaply in a running system:

1. **Most expensive** — which of our own function calls consume the most total
   wall-clock time?
2. **Most active** — which function calls run most often?

It is deliberately *shallow*: explicit instrumentation points at chosen
functions, function-name granularity, and atomic counters — no runtime
instrumentation, no stack sampling, no per-key labels. For deep CPU/memory
analysis use Go's built-in `runtime/pprof` and `net/http/pprof`; `pkg/perf`
complements them with always-affordable production counters.

## Design decisions (confirmed with the user)

| Question | Decision |
|---|---|
| Package location | `pkg/perf` — public, importable by consumers of go-store |
| Enablement | Env var `PERF_ENABLED` via `config.DefaultEnv`; nil-sink no-op when off |
| Reporting | Periodic top-N log dump, `GET /perf/stats` on the existing server, and an on-demand snapshot API |
| Granularity | Function name only (e.g. `store.Set`) — a fixed counter set, cheapest possible hot path |
| Initial scope | `pkg/store`, `pkg/job`, `internal/database` |

## Package design: `pkg/perf`

### Core API

```go
package perf

// Track begins a timing sample for name and returns the completion function.
// Typical use:
//
//	func (s *Store) Set(args types.SetArgs) (types.MetaData, error) {
//	    defer perf.Track("store.Set")()
//	    ...
//	}
func Track(name string) (done func())
```

- When disabled, `Track` is one `atomic.Bool` load plus a branch and returns a
  shared no-op closure — no allocation, no map lookup, no clock read.
- When enabled, the cost is two `time.Now()` calls, one map lookup, and a
  handful of atomic adds (~50–100 ns) — fine at function granularity, not
  inside tight loops.

### Counters

One lock-free record per instrumented function:

```go
type Stat struct {
    Name    string        `json:"name"`
    Calls   int64         `json:"calls"`
    Total   time.Duration `json:"total"`
    Min     time.Duration `json:"min"`
    Max     time.Duration `json:"max"`
    Last    time.Duration `json:"last"`
}
```

Backed by `atomic.Int64` fields (`Min` via a CAS loop) in a `sync.Map`
registry keyed by name. The name set is fixed at the call sites, so the map
stays tiny and lookups amortize.

### Enablement

- `PERF_ENABLED` added to [config.go](../pkg/config/config.go) `DefaultEnv`
  (default `"false"`), picked up by the existing `util.SetEnv()` mechanism.
- Read once at package init into an `atomic.Bool`; `perf.SetEnabled(bool)`
  allows tests and a possible future admin endpoint to toggle at runtime.

## Instrumentation points

Names are stable strings of the form `<package>.<Method>`. Rationale cites
what makes each function expensive or active.

### `pkg/store` ([Store.go](../pkg/store/Store.go), [Notify.go](../pkg/store/Notify.go), [Valid.go](../pkg/store/Valid.go))

| Name | Why it is worth monitoring |
|---|---|
| `store.Set` | The primary write path: ULID generation, `GetMeta` read, optional validation, `storedPayload` reflection, JSON marshal + SQLite write, notify fan-out |
| `store.Register` | Reflection-heavy: `dynReflect.BuildTypeInfo` walks the payload type (data field or whole static object); also `SetMeta` + conditional `SetData` writes |
| `store.Get` | `GetCurrStoreID` lookup plus `DB.GetData` (SQLite read + JSON unmarshal) |
| `store.Publish` | DB write plus metadata lookup and notification |
| `store.GetMeta` / `store.SetMeta` | The hottest wrappers — called by `IsRegistered`, `withMetaDeps`, `withMetaNotify`, and the job package's graph builders |
| `store.ValidateCached` | validator.v10 reflection-based validation on every checked `Set` |
| `store.notifyChange` | Fan-out loop; an erroring or slow notifier multiplies write latency |

### `pkg/job` ([Job.go](../pkg/job/Job.go), [Notify.go](../pkg/job/Notify.go))

| Name | Why it is worth monitoring |
|---|---|
| `job.DepsGraph` | Recursive `GetMeta` DB read per node via `buildDepsGraph` |
| `job.RunGraph` | Transitive reduction + topological sort + adjacency walk + `UpsertData` publish |
| `job.ExecRunGraph` | The execution engine: per-node `GetCurrStoreID` reads, `isDirty` evaluation, and concurrent `updMethod` calls — user update-code time is included in this span |
| `job.Notify` | Enqueue path: cheap per call but potentially very high frequency |
| `job.runOnce` | Umbrella cost of one notification run (graph rebuild + execute); the key health metric for the notification flow |

### `internal/database` ([database.go](../internal/database/database.go))

| Name | Why it is worth monitoring |
|---|---|
| `db.SetData` / `db.UpsertData` | JSON marshal + SQLite write; all persistence funnels through here |
| `db.GetData` | SQLite read + JSON unmarshal |
| `db.GetMeta` / `db.SetMeta` | Small but called constantly from both packages |
| `db.GetCurrStoreID` | Tiny query, very high frequency in `ExecRunGraph` loops |

Comparing `store.*` against `db.*` timings shows how much overhead the store
layer adds on top of raw SQL; comparing `job.runOnce` against
`job.ExecRunGraph` shows the cost of graph rebuilding versus execution.

## Reporting

### Snapshot API

```go
func Snapshot() []Stat                 // sorted by Total, descending
func Top(n int, by SortBy) []Stat      // SortBy: ByTotal | ByCalls
func Reset()                           // zero all counters (tests, report windows)
```

Returns copies, so callers may sort and filter freely. Used directly by tests
and by both reporters below.

### Periodic log dump

`perf.StartReporter()` starts a goroutine with a `time.Ticker` from
`PERF_REPORT_INTERVAL_MS` (default `60000`, `0` disables). Each tick logs the
top N (`PERF_REPORT_TOP_N`, default 10) functions by total time and by call
count through the standard `log` package, matching the existing logs in
[logs/](../logs). Started from `cmd/api/main.go` when enabled.

### HTTP endpoint

`GET /perf/stats` registered in [routes.go](../internal/server/routes.go)
alongside `/health`, returning `Snapshot()` as JSON. Registered only when
monitoring is enabled, so the route table is unchanged otherwise.

## Overhead budget

| State | Cost per instrumented call |
|---|---|
| Disabled | 1 atomic load + branch (~1 ns) |
| Enabled | 2 clock reads + 1 map lookup + ~4 atomic ops (~50–100 ns) |

Instrumented functions are all millisecond-or-microsecond scale (SQL
round-trips, JSON codec, graph algorithms), so enabled overhead stays well
under 1% — genuinely "shallow".

## Testing plan

- `pkg/perf/perf_test.go`: unit tests with `Reset` between cases — counters
  increment, min/max track, disabled mode records nothing, snapshot ordering.
- `pkg/perf/perf_bench_test.go`: `BenchmarkTrackEnabled` / `...Disabled` to
  keep the overhead budget honest in CI.
- One integration smoke test asserting `store.Set` and `db.SetData` counters
  increment after a real `Store.Set`. Remember the process-wide singleton DB:
  tests share one in-memory database and must use unique key prefixes, and
  packages are not safe for `go test -count>1`.

## Rollout checklist

1. `pkg/perf` core: `Track`, `Stat`, registry, `Snapshot`/`Top`/`Reset`, enable flag.
2. Config: `PERF_ENABLED`, `PERF_REPORT_INTERVAL_MS`, `PERF_REPORT_TOP_N` in `DefaultEnv`.
3. Instrument `pkg/store` (table above).
4. Instrument `pkg/job`.
5. Instrument `internal/database`.
6. Reporting: log reporter + `GET /perf/stats`.
7. Docs: a `docs/performance_user_guide.md` following the existing user-guide
   pattern, linked from the README.

## Non-goals

- CPU, heap, or goroutine profiling — use `runtime/pprof` / `net/http/pprof`.
- Distributed tracing or span trees — a flat per-function profile is enough.
- Per-store-key labels — adds map overhead to hot paths; revisit only if a
  specific hot key needs isolation (a key-level opt-in could be added later
  without changing the core API).
- Per-goroutine attribution — `ExecRunGraph` deliberately runs user updates
  concurrently per level; per-call attribution would require span context
  plumbing that a shallow monitor avoids.
