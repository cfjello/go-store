# Dependencies

Dependencies declare what an object is built **from**. They point downward: `maker-1` depends on `water-1` and `beans-1`. The job engine uses them to order recomputation (see the [Jobs guide](jobs_user_guide.md)); the notify list is the upward-pointing counterpart (see [Notifications](notifications_user_guide.md)).

## Declaring dependencies at registration

```go
s.Register(types.RegisterArgs{
	Key:       "latte-1",
	Object:    latte,
	DependsOn: types.Dependencies{DependsOn: []string{"milk-1", "espresso-1"}},
})
```

When `ParentKey` names a different (already registered) object, the child **inherits the parent's dependency list** and merges its own on top:

```go
// "CoffeeMaker" was registered with DependsOn: ["water-1"]
s.Register(types.RegisterArgs{
	Key:       "maker-9",
	ParentKey: "CoffeeMaker",
	Object:    maker,
	DependsOn: types.Dependencies{DependsOn: []string{"beans-1"}},
})
// maker-9 now depends on water-1 and beans-1.
```

## Managing dependencies at runtime

All helpers validate before writing and persist the metadata on success:

```go
err := s.SetDeps("latte-1", []string{"milk-1", "espresso-1"}) // replace the whole list
deps, err := s.GetDeps("latte-1")                            // copy of the current list
err = s.AddDeps("latte-1", "syrup-1")                        // append, deduplicated
err = s.RemoveDeps("latte-1", "syrup-1")                     // drop entries
err = s.MergeDeps("latte-1", []string{"mocha-1"})            // union in another object's deps
```

- Lists are always stored **deduplicated**; `AddDeps` and `MergeDeps` never create duplicates.
- `MergeDeps(key, srcKeys)` copies the dependency lists of `srcKeys` into `key` — handy when `key` aggregates several objects.

## Validation rules

Every mutation runs the same checks:

- No empty keys.
- No self-dependency (`"x"` cannot depend on `"x"`).
- Every dependency must be a **registered** object.

Violations return an error and leave the existing list untouched.

## How dependencies are used

1. `job.DepsGraph(key)` builds the dependency DAG rooted at `key`, recursively expanding children and detecting cycles.
2. `job.RunGraph` / `ExecRunGraph` turn that DAG into a leaves-first execution plan so every dependency recomputes before its dependents.
3. `Fixed` objects (and everything beneath them) are pruned from the execution order — a `Fixed` dependency is treated as an immutable input.

Keep dependency lists accurate: stale entries make jobs re-run objects they do not need; missing entries make jobs skip objects they do need.

## Example

```go
s.Register(types.RegisterArgs{Key: "water-1", Object: water})
s.Register(types.RegisterArgs{Key: "beans-1", Object: beans})
s.Register(types.RegisterArgs{
	Key:       "espresso-1",
	Object:    espresso,
	DependsOn: types.Dependencies{DependsOn: []string{"water-1", "beans-1"}},
})

// espresso-1 recomputes after water-1 and beans-1 whenever a job runs.
js := job.NewJobService(s)
rg, err := js.RunGraph("espresso-1")
if err != nil {
	return err
}
fmt.Println(rg.Order) // leaves first: [water-1 beans-1 espresso-1]
```
