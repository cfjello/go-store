# Utilities and Types

Small building blocks used across the package, all importable on their own.

## ULID generation (`pkg/util`)

Store versions, job IDs, and soft-delete stamps are [ULIDs](https://github.com/oklog/ulid) — 26-character, time-ordered, lexicographically sortable identifiers.

```go
import "github.com/cfjello/go-store/pkg/util"

id := util.Ulid()      // package-level generator; safe for concurrent use,
                       // monotonic within the same millisecond
id2 := util.NewULID()  // one-shot generator (fresh entropy each call)

gen := util.ULIDGenerator() // your own concurrency-safe generator closure
id3 := gen()
```

Prefer the shared `util.Ulid` in hot paths; use `ULIDGenerator()` when you need an independent stream (e.g. per-worker).

## `util.UniqueStrings`

Deduplicate a string slice, preserving first-seen order. Used internally to keep dependency and notify lists clean:

```go
keys := util.UniqueStrings([]string{"a", "b", "a"}) // ["a", "b"]
```

## `util.SetEnv`

Applies the package's default environment variables without overwriting values you have already set. Called automatically by the database and the server — see the [Configuration guide](configuration_user_guide.md).

## Type helpers (`pkg/types`)

### `types.Ptr`

`SetArgs.Fixed` and `SetArgs.AlwaysRun` are `*bool` so that "not specified" (nil) is distinguishable from "set to false". `Ptr` makes literals ergonomic:

```go
s.Set(types.SetArgs{Key: "maker-1", Object: m, Fixed: types.Ptr(true)})
```

### `types.CheckMode`

Tri-state validation control for `Set`: `CheckNone` (defer to metadata), `CheckOn`, `CheckSkip`. See the [Validation guide](validation_user_guide.md).

### `types.ExtError`

A structured error type when you need more than a message:

```go
type ExtError struct {
	Message string
	Name    string
	Cause   error
	Info    map[string]string
}
```

### Core records

The same structs the store persists are available for your own tooling: `MetaData`, `RegisterArgs`, `SetArgs`, `PublishArgs`, `Dependencies`, `NotifyList`, `JobVertex`, `RunNode`, `RunGraph`, `ADT_Job`. See the [Store guide](store_user_guide.md) and [Jobs guide](jobs_user_guide.md) for how they are used.

## Type reflection (`pkg/dynReflect`)

`dynReflect` captures the **shape** of a Go type as JSON-serializable `TypeInfo`. The store records it automatically in `MetaData.TypeInfo` at `Register` for the persisted payload (the `data` field, or the whole static object), and a checked `Set` compares the write payload against that snapshot.

```go
import "github.com/cfjello/go-store/pkg/dynReflect"

// Object -> type JSON
jsonType, err := dynReflect.ObjectToTypeJSON(myStruct)
// {"type":"main.CoffeeMaker","kind":"struct","fields":{...}}

// Type JSON -> a new zero-valued instance of an equivalent type
obj, err := dynReflect.JSONToObjectType(jsonType)
```

Notes and limitations:

- `TypeInfo` records `Type` (the Go type string), `Kind`, and recursively `Fields` (structs), `Elem` (pointers/slices/arrays/channels), `KeyType`/`ValueType` (maps). Unexported struct fields are skipped. The walk is type-driven (empty vs filled collections match) and truncates cycles.
- `JSONToObjectType` rebuilds **structurally equivalent anonymous types** — named types and methods cannot be recovered from the JSON alone. Use it for generic tooling (inspect, decode, generate), not as a substitute for the real type.

## Import map

| Package | Provides |
|---|---|
| `pkg/store` | The object store — [Store guide](store_user_guide.md) |
| `pkg/job` | Dependency graphs and run execution — [Jobs guide](jobs_user_guide.md) |
| `pkg/types` | Shared records and validation of key lists |
| `pkg/config` | Default configuration — [Configuration guide](configuration_user_guide.md) |
| `pkg/util` | ULIDs, `SetEnv`, `UniqueStrings` |
| `pkg/dynReflect` | Reflection-based type capture |
| `internal/database` | SQLite service — [Database guide](database_user_guide.md) |
| `internal/server` | HTTP server — [Server guide](server_user_guide.md) |
