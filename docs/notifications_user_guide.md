# Notifications

Notifications push change events **upward**: when an object receives new data, every object on its notify list is told. With a `job.JobService` connected (the default wiring), a notification enqueues an update run that recomputes the target's dependency graph.

For the guarantees and the concurrency design, see the in-depth reference: [Notifications](notifications.md).

## Declare notify targets at registration

```go
s.Register(types.RegisterArgs{
	Key:      "beans-1",
	Object:   beans,
	NotifyTo: types.NotifyList{"espresso-1", "dashboard-1"},
})
```

Now every successful `Set` or `Publish` on `beans-1` notifies `espresso-1` and `dashboard-1`.

When `ParentKey` names another registered object, the child inherits the parent's notify list and merges its own — the same rule as for [dependencies](dependencies_user_guide.md).

## Manage the notify list at runtime

```go
err := s.SetNotify("beans-1", []string{"espresso-1"}) // replace the whole list
targets, err := s.GetNotify("beans-1")                // copy of the current list
err = s.AddNotify("beans-1", "grinder-1")             // append, deduplicated
err = s.RemoveNotify("beans-1", "dashboard-1")        // drop entries
```

Validation rules (same shape as dependency validation):

- No empty targets.
- An object cannot notify itself.
- Every target must be a registered object.

## What happens on a change

1. `s.Set(...)` (or `s.Publish(...)`) writes the new version.
2. The store looks up the key's notify list and calls its registered `Notifier` once per target.
3. `job.NewJobService(s)` installs itself as that notifier: `Notify(target)` enqueues a background run that rebuilds and executes the target's dependency graph (see the [Jobs guide](jobs_user_guide.md)).

Notification errors are logged, never returned — a failed notification can never misreport a successful write.

```go
// beans-1 changed; espresso-1 and dashboard-1 recompute in the background.
_, err := s.Set(types.SetArgs{Key: "beans-1", Object: beans})

// Wait for all queued notification runs to drain (tests, graceful shutdown):
js.WaitIdle()
```

## Runs are coalesced

Per key there is at most one running update plus one pending re-run. A burst of ten `Set`s on `beans-1` while `espresso-1` is still recomputing collapses into a single follow-up run — notification storms cannot stack.

Failed runs surface through `JobService.OnError` when set, otherwise via `log.Printf`:

```go
js.OnError = func(key string, err error) {
	metrics.Count("notify_failure", key)
}
```

## Custom notifiers

The store's notifier is pluggable. Implement the one-method interface and install it during setup (before serving traffic; `nil` disables notifications):

```go
type audit struct{}

func (a audit) Notify(key string) error {
	return auditLog.Write("changed", key)
}

s.SetNotifier(audit{})
```

Note that installing your own notifier replaces the job service wiring — set up both behaviors inside your implementation if you need them.
