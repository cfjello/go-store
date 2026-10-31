# go-store

A versioned, dependency-aware object store for Go, backed by SQLite. Register any Go value under a key, and go-store gives you immutable ULID-addressed versions, struct-tag validation, dependency graphs, change notifications, and a job engine that recomputes derived objects in the right order — in-memory, on disk, or both.

## Features

- **Versioned object storage** — every write is a new immutable row addressed by a time-ordered [ULID](https://github.com/oklog/ulid); fetch the latest version or pin any historical one.
- **Metadata-driven objects** — each key carries a metadata record: schema/parent key, validation flag, `Fixed`/`AlwaysRun` recomputation hints, dependencies, and notify targets.
- **Struct-tag validation** — opt-in validation of writes via [go-playground/validator](https://pkg.go.dev/github.com/go-playground/validator/v10), overridable per write.
- **Dependency graphs** — declare what an object is built from; build the transitive DAG, detect cycles, and get topological execution orders.
- **Change notifications** — objects notify registered consumers after every successful write; notification runs are coalesced and executed in the background.
- **Job engine** — rebuilds a root object's run graph, detects dirty nodes by comparing store IDs, and recomputes them leaves-first with level-aware concurrency.
- **Iterators** — shared pull-based cursors over a stored collection, with optional restock through the object's update method.
- **Flexible persistence** — in-memory SQLite for speed, file SQLite with WAL for durability, or in-memory plus an attached file "persist layer" for explicit publish-to-disk.
- **HTTP server** — REST API over `/store`, `/job`, and `/perf` resources with CORS middleware, request body limits, and graceful shutdown. Read-only by default; set `REST_ENABLE_WRITES=true` to allow mutations.
- **Shallow performance counters** — optional function-level timings for store, job, and database hot paths (`PERF_ENABLED`), with a snapshot API, periodic log dump, and `GET /perf/stats`.

## User Guide

Start with the [Getting Started guide](docs/getting_started_user_guide.md), then dive into each feature:

| Guide | Covers |
|---|---|
| [Getting Started](docs/getting_started_user_guide.md) | Installation, database modes, your first register/set/get, running the examples |
| [Core Store](docs/store_user_guide.md) | `Register`, `Set`, `Get`, `Publish`, `UnRegister`, metadata, versioning, the object cache |
| [Dependencies](docs/dependencies_user_guide.md) | Declaring and managing `DependsOn` lists, parent-key inheritance |
| [Validation](docs/validation_user_guide.md) | Struct tags, the `Check` flag, per-write `CheckMode` overrides |
| [Iterators](docs/iterators_user_guide.md) | Walking stored collections with `OpenIter`/`Next`, restock, counters |
| [Notifications](docs/notifications_user_guide.md) | Notify lists, change propagation, coalesced background runs |
| [Jobs and Run Graphs](docs/jobs_user_guide.md) | `JobService`, dependency DAGs, run graphs, dirty checking, `Fixed`/`AlwaysRun` |
| [Database and Persistence](docs/database_user_guide.md) | SQLite modes, the persist layer, storage model, `SQLITE_*` variables |
| [Configuration](docs/configuration_user_guide.md) | Environment variables, `DefaultConfig`, `util.SetEnv`, naming the update method |
| [HTTP Server](docs/server_user_guide.md) | `/store`, `/job`, `/perf` endpoints, the write gate, CORS, timeouts, extending routes |
| [Performance](docs/performance_user_guide.md) | `pkg/perf` counters, `PERF_ENABLED`, snapshot API, log dump, `GET /perf/stats` |
| [Utilities and Types](docs/utilities_user_guide.md) | ULID generation, `types.Ptr`, `ExtError`, reflection-based type capture |

In-depth design references:

- [Iterators](docs/iterators.md) — precise binding, restock, and concurrency semantics.
- [Notifications](docs/notifications.md) — notification guarantees and the convergence invariant.
- [Performance](docs/performance.md) — monitor design, instrumentation points, overhead budget.
- [Web Server Interface](docs/web_server_interface.md) — REST endpoint design, write gate, and security analysis.

## Quick Start

Requires Go 1.25+ with CGO enabled. Import the convenience package as `gostore` (a Go package name cannot contain a hyphen). The original `pkg/store`, `pkg/types`, and `pkg/job` paths still work.

```bash
go get github.com/cfjello/go-store
```

```go
package main

import (
	"fmt"
	"os"

	gostore "github.com/cfjello/go-store"
)

type Counter struct {
	Name  string `json:"name"  validate:"required"`
	Value int    `json:"value" validate:"gte=0"`
}

func main() {
	// In-memory database; set SQLITE_DB_URL to a file URL instead for persistence.
	// Must be set before the store singleton is created.
	os.Setenv("SQLITE_DB_URL", ":memory:")

	s := gostore.NewStoreService(true)

	counter := &Counter{Name: "clicks", Value: 0}
	if _, err := s.Register(gostore.RegisterArgs{Key: "counter-1", Object: counter, Init: true, Check: true}); err != nil {
		panic(err)
	}

	counter.Value = 42 // every Set stores a new immutable version
	if _, err := s.Set(gostore.SetArgs{Key: "counter-1", Object: counter}); err != nil {
		panic(err)
	}

	var loaded Counter
	if _, err := s.Get("", "counter-1", &loaded); err != nil { // "" = latest version
		panic(err)
	}
	fmt.Printf("%+v\n", loaded)
}
```

## Examples

Complete programs live in [example/](example/):

- [example/coffeemaker](example/coffeemaker/coffeemaker.go) — a stateful appliance registered under a schema key, persisting every state change.
- [example/job](example/job/job.go) — the same domain modeled with dependencies and job flows.
- [example/graph](example/graph/graphEx1.go) — the graph primitives behind the job engine.

```bash
go run ./example/coffeemaker
go run ./example/job
```

## MakeFile

Run build make command with tests
```bash
make all
```

Build the application
```bash
make build
```

Run the application
```bash
make run
```

Live reload the application:
```bash
make watch
```

Run the test suite:
```bash
make test
```

Run the test suite with a coverage report:
```bash
make coverage
```

Vet, lint, and tidy:
```bash
make vet
make lint
make tidy
```

Clean up binary from the last build:
```bash
make clean
```

## Project Structure

```
cmd/api          HTTP API entrypoint
docs/            User guides and design references
example/         Runnable example programs
internal/
  database/      SQLite service, schema, prepared statements
  server/        HTTP server, routes, health checks
pkg/
  config/        Default configuration and environment
  dynReflect/    Reflection-based type capture
  job/           Dependency graphs and run execution
  store/         The object store (register, set, get, iterate, notify)
  types/         Shared records and key-list validation
  util/          ULIDs, environment defaults, helpers
test/            Integration-style tests
```

