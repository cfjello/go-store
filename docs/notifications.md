# Notifications

Change notifications let a registered object trigger the update job flow of
other registered objects. When an object's data changes (a successful
`Store.Set` or `Store.Publish`), the store notifies every object named in the
object's *notify list*. Each notified object then re-runs its own dependency
job flow — the existing `RunGraph` + `ExecRunGraph` machinery from the `job`
package — updating its dependencies leaves-first and finally itself.

## Background: the existing job flow

- Objects are registered in the store with metadata that includes a
  `DependsOn` list ([Deps.go](../pkg/types/Deps.go)). Edges point downward:
  an object names the objects it depends on.
- `JobService.DepsGraph(key)` builds a DAG of those dependencies; cycles are
  rejected at build time (`graph.Acyclic`, `PreventCycles`, plus an explicit
  recursion-path check).
- `JobService.RunGraph(rootKey)` computes the leaves-first execution order
  and publishes a `types.RunGraph` snapshot under `rootKey + "_job"`.
- `JobService.ExecRunGraph(key)` loads the newest snapshot, refreshes every
  node's current store ID, and computes `IsDirty` per node:
  - a node with no stored version is dirty;
  - a `Fixed` node is never dirty (and, together with its descendants, is
    excluded from the execution order);
  - an `AlwaysRun` node is always dirty;
  - a leaf is dirty when its store ID is older than the root's store ID;
  - a non-leaf is dirty when any child's store ID is newer than its own.

  Dirty nodes are updated by calling the store's `updMethod` (default
  `SetData`) on the cached object, concurrently per level, waiting for deeper
  levels before moving up a branch.

Notifications add the missing trigger: an object that received new data tells
the objects that consume it to re-run this flow.

## Design decisions (confirmed with the user)

| Question | Decision |
|---|---|
| Trigger | Automatic: every successful `Store.Set` / `Store.Publish` notifies, but only the explicitly named objects in the object's notify list |
| Configuration | A `NotifyTo` list per object, managed with store calls mirroring the `DependsOn` calls (`SetNotify`/`GetNotify`/`AddNotify`/`RemoveNotify`, plus `RegisterArgs.NotifyTo`) |
| Targets | Every notified object runs its own dependency job flow |
| Execution | Asynchronous: `Set` returns immediately; runs are dispatched in the background |
| Which nodes re-run | Existing dirty-check logic — only nodes whose inputs actually got newer store IDs |
| Circular flows | Coalescing only (one running + one queued run per key); convergence is guaranteed by the dirty-check invariant (analysis below) |

## Why an explicit notify list instead of reverse dependency lookup

Dependencies point downward (object → its dependencies), so "who depends on
me?" would require scanning all metadata. An explicit `NotifyTo` list is
cheaper, is consistent with the existing `DependsOn` API, and lets an object
notify targets that are *not* declarable as dependents — for example a
monitoring object that must not appear in the notifier's dependency graph.
Notify lists are validated against the registry exactly like dependencies, so
a typo is caught at configuration time.

## New API surface

### types package (new file `pkg/types/Notify.go`)

```go
// NotifyList is the list of registered object keys to notify when this
// object's data changes. Methods mirror types.Dependencies.
type NotifyList []string

func (n *NotifyList) Set(targets []string)     // replace, deduplicated
func (n NotifyList) Get() []string             // copy of current targets
func (n *NotifyList) Add(targets ...string)    // append, skipping duplicates
func (n *NotifyList) Merge(other NotifyList)   // union of another list
func (n *NotifyList) Remove(targets ...string) // drop targets

// ValidateNotify mirrors ValidateDeps with notification-specific messages:
// no empty target, no direct self-notification, every target registered.
func ValidateNotify(self string, isValid func(string) bool, targets ...string) error
```

`types.MetaData` and `types.RegisterArgs` each gain:

```go
NotifyTo NotifyList `json:"notifyTo,omitempty"`
```

`ValidateDeps` and `ValidateNotify` share a common internal helper so the
checks stay identical; only the error wording differs.

### store package (`pkg/store/Store.go`, new file `pkg/store/Notify.go`)

```go
// Notifier receives change notifications for registered objects. The job
// package registers its JobService as the store's Notifier, which avoids an
// import cycle (store must not import job).
type Notifier interface {
	Notify(key string) error
}

func (s *Store) SetNotifier(n Notifier)

// Notify-list management, mirroring SetDeps/GetDeps/AddDeps/RemoveDeps:
func (s *Store) SetNotify(key string, targets []string) error
func (s *Store) GetNotify(key string) ([]string, error)
func (s *Store) AddNotify(key string, targets ...string) error
func (s *Store) RemoveNotify(key string, targets ...string) error
```

- `Register` resolves `RegisterArgs.NotifyTo` into the metadata exactly like
  dependencies: when `ParentKey` differs from `Key`, the parent's notify list
  is merged with the argument list (a schema's notify list propagates to its
  instances). Direct self-notification is rejected.
- On a *successful* `Set` and `Publish`, the store calls
  `notifier.Notify(target)` for each entry in the object's notify list.
  Notification happens after the data write; `Set`/`Publish` never block on
  the resulting runs.
- `Register` with `Init: true` writes initial data directly through
  `DB.SetData` and therefore does **not** notify: registration is setup, not
  a change event.
- Notification errors inside `Set`/`Publish` (for example a target that was
  unregistered after being configured) are logged, never returned — the data
  write has already succeeded and must not be reported as failed.

### job package (new file `pkg/job/Notify.go`)

`JobService` implements `store.Notifier` and `NewJobService` registers the
service with the store via `store.SetNotifier(js)`.

```go
// Notify validates key and enqueues an update run for it. It returns
// immediately; the run executes in the background.
func (js *JobService) Notify(key string) error

// WaitIdle blocks until no queued or running notification runs remain.
// Intended for tests and graceful shutdown.
func (js *JobService) WaitIdle()

// OnError, when set, is called with the error of every failed notification
// run. When nil, errors are reported via log.Printf.
JobService.OnError func(key string, err error)
```

## Queue semantics and race-condition control

All queue state lives on the `JobService` instance, guarded by one mutex
(`runsMu`), so manually constructed services in tests behave identically to
the singleton.

Per notified key the service keeps a small state machine:

```
type runState struct {
	running bool  // a run for this key is in flight
	pending bool  // at least one notification arrived while running
}
```

`Notify(key)`:

1. Validates the key (non-empty, registered) and returns an error
   synchronously when invalid — nothing is enqueued for a bad key.
2. Under `runsMu`: if the key is already running, set `pending = true` and
   return (any number of notifications during a run coalesce into **one**
   queued re-run). Otherwise mark `running = true` and launch `runLoop(key)`
   in a new goroutine.

`runLoop(key)`:

```
for {
	err := runOnce(key)              // RunGraph(key) + ExecRunGraph(key)
	if err != nil { report via OnError or log }
	lock(runsMu)
	if state.pending { state.pending = false; unlock; continue }
	delete(states, key); inFlight--; broadcast idle; unlock; return
}
```

This directly implements the required ordering guarantee: **an object that
receives a notification while its job is running always finishes the current
run before the same job is re-run once more.**

Race conditions considered:

| Race | Handling |
|---|---|
| Notification during an in-flight run for the same key | Coalesced into a single pending re-run; never more than one running + one queued run per key |
| Notification arriving exactly as `runLoop` decides to exit | Both transitions hold `runsMu`: either the notifier sees `running` and sets `pending` (re-run happens), or it sees no state and starts a fresh run. No notification is lost |
| Two notified roots share dependency nodes; their `ExecRunGraph` calls overlap | `ExecRunGraph` acquires a per-object mutex around every `updMethod` call, so an object's update method never runs concurrently with itself, across all runs. Locks are held one at a time, never nested — no deadlock |
| Dependencies changed after the last published snapshot | Every triggered run rebuilds the graph (`RunGraph`) before executing it, so runs always reflect current `DependsOn` metadata and current store IDs |
| `Set` from many goroutines at once | Notification emission is a non-blocking enqueue under `runsMu`; the store path stays lock-free apart from that |
| Target unregistered between configuration and notification | `Notify` fails fast (logged by the store); a target unregistered mid-run makes `RunGraph` fail and the error is reported via `OnError`/`log` |
| Async run failure | Reported via `OnError` when set, otherwise `log.Printf`; failures never propagate into another object's `Set` |

`WaitIdle` tracks in-flight run loops with a counter under `runsMu` plus a
`sync.Cond`, so it also wakes correctly when a run spawns further
notifications (unlike a bare `sync.WaitGroup`, whose `Add`-during-`Wait`
semantics are fragile here).

## Circular flows: why coalescing alone is sufficient

The dependency graph is a DAG — `A` depending on `B` and `B` depending on `A`
is rejected. Notify lists, however, are independent edges and **may** form
cycles (`A` notifies `B`, `B` notifies `A`). Such cycles converge without a
circuit breaker because of the dirty-check invariant:

1. `ExecRunGraph` only invokes an object's `updMethod` when the node is dirty,
   i.e. when some input actually has a newer store ID (or the node was never
   stored, or is `AlwaysRun`).
2. A notification only leads to further notifications when the run actually
   writes new data (`Set`/`Publish`).
3. Store IDs are monotonic ULIDs. Once a run completes, every node it wrote
   is newer than its dependencies, so a re-run of the same graph finds
   nothing dirty, writes nothing, and emits no notifications. The cycle dies.

Worked example — `A` depends on `B`; `A` notifies `B` and `B` notifies `A`:

- `Set(A)` → notify `B` → run `B`'s graph: `B` has no dependencies and its
  store ID equals its root store ID → clean → nothing runs → no notify.
  Done.
- `Set(B)` → notify `A` → run `A`'s graph `{B, A}`: `B` is newer than `A` →
  `A` dirty → `A.SetData()` runs and (typically) writes `A` → notify `B` →
  run `B`: clean → Done.

So the user's reasoning is correct: **the flow runs only while new data
flows.** One caveat to be aware of: an `updMethod` that calls `Set`
*unconditionally* (writing a fresh store ID even when nothing changed, e.g.
stamping `UpdatedAt` on every run) or an `AlwaysRun` node inside a notify
cycle creates a perpetual source of "new data" and will keep the cycle
running forever — serialized and coalesced, but unbounded. Keep `updMethod`
writes conditional on real changes, and keep `AlwaysRun` nodes out of notify
cycles. If a safety valve is ever wanted, a max-consecutive-runs breaker can
be added to `runLoop` without changing any semantics (listed under future
work).

## End-to-end flow

```
object K: Store.Set(K, newData)
    │  (write succeeds, new store ID for K)
    ▼
store: for each target T in meta[K].NotifyTo: notifier.Notify(T)
    │  (non-blocking enqueue)
    ▼
job dispatcher (per target T, serialized + coalesced):
    RunGraph(T)      → rebuild T's dependency DAG, publish fresh snapshot
    ExecRunGraph(T)  → leaves-first, per-level concurrency, dirty-checks,
                       per-object update locks
    │  updMethod writes new data for node N
    ▼
    store: notify meta[N].NotifyTo ...  (recurses; converges via dirty checks)
```

## Testing plan

- **store**: notify-list CRUD + validation (empty target, self, unregistered,
  parent merge in `Register`); `Set` and `Publish` emit to a fake notifier;
  `Register` does not emit; emission without a configured notifier is a no-op.
- **job**: `Set` on a source object triggers the target's run end-to-end
  (dirty node updated, observed via `WaitIdle`); repeated notifications
  during a blocking run coalesce into exactly one extra run; a notify cycle
  (`A`↔`B`) terminates; shared dependencies of two concurrently notified
  roots never enter `updMethod` concurrently; `Notify` on an unknown key
  errors; failed runs surface through `OnError`.

## Supporting fixes required by this feature

Concurrent notification runs exposed three latent issues that had to be fixed
for the feature to be correct:

1. **In-memory SQLite is per connection** (`internal/database`). The pool was
   free to open a second connection under concurrent use, and each new
   connection sees its own empty `:memory:` database — concurrent runs could
   suddenly find objects "not registered". The database layer now has two
   explicit modes, selected by `SQLITE_DB_URL`:
   - **In-memory mode** (`SQLITE_DB_URL` empty or `:memory:`): the pool is
     pinned with `db.SetMaxOpenConns(1)`, since a second connection can never
     see the same data; this also makes `ATTACH` (per-connection state)
     deterministic. `SQLITE_ATTACH_URL` optionally attaches a file database
     as the persist layer (alias from `FILE_DB_ALIAS`, default `persist`).
   - **File mode** (any other `SQLITE_DB_URL`): the main database *is* the
     persistent store, opened with `SQLITE_DB_FLAGS`
     (`?_journal_mode=WAL&_sync=normal&_mode=rwc&_busy_timeout=5000`). WAL
     allows concurrent readers plus one writer across connections, so the
     pool is sized by `SQLITE_MAX_CONNS` (default 4) and `_busy_timeout`
     smooths over writer contention. Nothing is attached and no dual writes
     or read fallbacks run: `DbAlias == ""` disables the persist code paths.
   `PersistJob` was updated to bind its prepared statements to its
   transaction (`tx.StmtContext`) and is a no-op without an attached persist
   layer. Table drops are gated on `reset` so a file main database survives
   restarts unless a reset is explicitly requested.
2. **`util.Ulid` was not goroutine-safe** (`pkg/util/Ulid.go`). The shared
   generator wraps a stateful monotonic entropy source that two concurrent
   runs can call at the same time; it is now guarded by a mutex.
3. **`Store.Set` clobbered object flags** (`pkg/types`, `pkg/store`).
   `SetArgs.Fixed`/`AlwaysRun` were plain bools, so any `Set` with default
   arguments silently reset a registered object's `AlwaysRun` (or `Fixed`)
   to false. Since `Set` is the notification trigger, per-write arguments
   must not overwrite object configuration. Both fields are now `*bool`:
   `nil` (the default) leaves the registered value intact, and an explicit
   `types.Ptr(v)` changes it — mirroring `CheckMode`'s "unspecified defers
   to metadata" semantics. No existing caller set either field.

## Future work (not in scope)

- Optional circuit breaker: cap consecutive triggered runs per key.
- Reusing the stored run-graph snapshot when dependencies are unchanged,
  instead of rebuilding per run.
- Persisted/durable notification queue across restarts.
- HTTP endpoint in `internal/server` for external notification sources.
