# Jobs and Run Graphs

The `job` package turns dependency metadata into executable update flows. When an object's inputs change, the job service rebuilds the object's dependency DAG and recomputes every dirty node in leaves-first order by calling each object's update method.

## The update method contract

Every object that participates in a job flow must implement the **no-argument update method** named by the store's `UpdMethod()` — `SetData` by default (configurable via `NamesConfig.UpdMethod`) — **and** expose a `data` field (`NamesConfig.DataName`) that holds the persisted payload. `Register` rejects an updatable object that has the method but no data field:

```go
func (e *Espresso) SetData() error {
	// Re-read inputs, recompute, and store a new version of e.
	water := e.loadWater() // e.g. s.Get("", "water-1", &water)
	...
	_, err := e.store.Set(types.SetArgs{Key: "espresso-1", Object: e})
	return err
}
```

A missing method, a method that takes arguments, a panic, or a non-nil `error` result fails the run. Objects registered without this method are treated as standalone static data and marked `Fixed`, so job execution never tries to call it. Calls are serialized per object key, so concurrent flows never enter the same object's update method twice at once.

When `Set`/`Publish` persist an updatable object, only the field named by `NamesConfig.DataName` (`data` by default) is written to `data_json`. Static objects without that field are stored whole.

## Create the service

```go
s := store.NewStoreService(false)
js := job.NewJobService(s) // installs itself as the store's change notifier
```

`NewJobService` returns a process-wide instance. Construction wires notifications: every successful `Set`/`Publish` on a key enqueues runs for the keys on its notify list (see [Notifications](notifications_user_guide.md)).

## Build and inspect the dependency graph

```go
g, err := js.DepsGraph("latte-1")       // DAG of key's transitive dependencies
order, err := js.ExecutionOrder(g)            // root first, deepest deps last
leavesFirst, err := js.ExecutionOrderLeavesFirst(g) // deps first, root last
```

- Cycles are detected and reported as errors (`dependency cycle detected: "a" -> "b"`).
- Vertices marked `Fixed` — and all of their descendants — are excluded from both orders. Use `Fixed` for immutable inputs.

## Run graphs

`RunGraph` builds the DAG for a root key, computes the leaves-first order, snapshots per-node state (store ID, level, `Fixed`, `AlwaysRun`, dependencies) into a `types.RunGraph`, and publishes that snapshot under the key `<root>_job`:

```go
rg, err := js.RunGraph("latte-1")
if err != nil {
	return err
}
fmt.Println(rg.Order) // e.g. [water-1 beans-1 espresso-1 milk-1 latte-1]
```

The snapshot is itself a stored object, so you can fetch it later via `Get` on `<root>_job`.

## Execute a run graph

```go
rg, err := js.ExecRunGraph("latte-1")
```

`ExecRunGraph` loads the newest `<root>_job` snapshot, refreshes every node's current store ID, and recomputes each **dirty** node by calling its update method:

- **Dirty leaf**: its store ID is older than the root's store ID (or it has no stored version yet).
- **Dirty non-leaf**: one or more of its children have a newer store ID.
- **`AlwaysRun`**: always dirty — the node recomputes on every execution.
- **`Fixed`**: never dirty — the node (and its subtree) is skipped.

Execution is level-aware: nodes at the same depth run concurrently, and moving up a branch waits for every deeper in-flight call to finish. A failed lookup or call stops later launches and is returned as an error; the partially updated `RunGraph` is discarded.

## Notification-driven runs

You rarely call `ExecRunGraph` directly. The standard flow is notification-driven:

```go
// 1. Declare the flow.
s.Register(types.RegisterArgs{Key: "water-1", Object: water, NotifyTo: types.NotifyList{"espresso-1"}})
s.Register(types.RegisterArgs{Key: "espresso-1", Object: espresso,
	DependsOn: types.Dependencies{DependsOn: []string{"water-1"}},
	NotifyTo:  types.NotifyList{"latte-1"}})
s.Register(types.RegisterArgs{Key: "latte-1", Object: latte,
	DependsOn: types.Dependencies{DependsOn: []string{"espresso-1", "milk-1"}}})

// 2. Change a leaf. espresso-1 recomputes, then latte-1, in the background.
_, err := s.Set(types.SetArgs{Key: "water-1", Object: water})

// 3. Drain the queue (tests and graceful shutdown).
js.WaitIdle()
```

Runs per key are coalesced — one running, at most one pending — so hot keys cannot stack unbounded work. Failures go to `js.OnError(key, err)` when set, otherwise to `log.Printf`.

## Controlling recomputation with flags

| Flag | Registration | Effect |
|---|---|---|
| `Fixed` | `RegisterArgs.Fixed` (changeable via `SetArgs.Fixed` only before `Init`) | Immutable input: excluded from execution order, never re-run. |
| `AlwaysRun` | `RegisterArgs.AlwaysRun` (changeable any time via `SetArgs.AlwaysRun`) | Recomputed on every execution, even when nothing upstream changed. |

## Error handling checklist

- `RunGraph` fails when the root is unregistered, a dependency is unregistered, or the dependency graph has a cycle.
- `ExecRunGraph` fails when there is no stored run graph for the key (call `RunGraph` first — `Notify` does this for you), when a dirty node has no cached object, or when an update method call fails.
- Dirty checks compare ULIDs as strings; because ULIDs are time-ordered, "newer" is "lexicographically larger".
