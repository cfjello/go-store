# Getting Started

This guide takes you from a fresh checkout to a working program that registers an object, stores versions of it, and reads them back.

## Prerequisites

- Go 1.25 or later.
- CGO enabled (`CGO_ENABLED=1`, the default on most desktops) — the storage layer uses [`github.com/mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3), a cgo package. On Windows you need a C compiler such as MinGW-w64 gcc or MSVC build tools.

## Install

```bash
go get github.com/cfjello/go-store
```

## Choose a database mode

The store persists to SQLite. Before you open the store, decide where the data lives by setting environment variables (see the [Database guide](database_user_guide.md) for the full reference):

| Mode | Setup |
|---|---|
| In-memory | Set `SQLITE_DB_URL=:memory:` **before** the store is first created. Data vanishes when the process exits. |
| File database | `SQLITE_DB_URL=file:/path/to/go-store.db` (WAL mode is enabled by default flags). |
| In-memory + persist layer | In-memory main database plus `SQLITE_ATTACH_URL=file:/path/to/persist.db`; only `Publish` writes reach disk. |

> **Watch out:** when `SQLITE_DB_URL` is completely unset, the package default (`file:F:/Sqlite3/go-store.db` from `DefaultEnv`) is applied — not in-memory. The quickest zero-configuration start is to set `:memory:` explicitly:

```go
package main

import (
	"fmt"
	"os"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

type Counter struct {
	Name  string `json:"name"  validate:"required"`
	Value int    `json:"value" validate:"gte=0"`
}

func main() {
	// In-memory database. Must be set before the store singleton is created.
	os.Setenv("SQLITE_DB_URL", ":memory:")

	// NewStoreService returns the process-wide singleton store backed by SQLite.
	s := store.NewStoreService(true) // reset=true drops and recreates the schema

	// 1. Register the object under a key. Init=true also stores the first version.
	counter := &Counter{Name: "clicks", Value: 0}
	meta, err := s.Register(types.RegisterArgs{
		Key:    "counter-1",
		Object: counter,
		Init:   true,  // store the initial value immediately
		Check:  true,  // validate on every Set
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("registered:", meta.Key)

	// 2. Store a new version. Every Set creates a new ULID-addressed row.
	counter.Value = 42
	if _, err := s.Set(types.SetArgs{Key: "counter-1", Object: counter}); err != nil {
		panic(err)
	}

	// 3. Read the latest version back into a typed value.
	var loaded Counter
	if _, err := s.Get("", "counter-1", &loaded); err != nil {
		panic(err)
	}
	fmt.Printf("loaded: %+v\n", loaded)
}
```

Key points:

- **Register before Set.** `Set` fails for keys that were never registered.
- **`Init: true`** performs registration and the first data write in one call.
- **`Get("", key, &out)`** resolves the newest version automatically; pass a store ID to pin a specific version.
- `reset=true` is honored only on the first call in the process — the store and the database are singletons.

## Next steps

- Model relationships between objects with [Dependencies](dependencies_user_guide.md).
- React to changes with [Notifications](notifications_user_guide.md) and [Jobs](jobs_user_guide.md).
- Walk large stored collections with [Iterators](iterators_user_guide.md).
- Run the built-in HTTP server from the [Server guide](server_user_guide.md).

## Run the bundled examples

The [example/](../example/) directory contains complete programs:

| Example | Shows |
|---|---|
| `example/coffeemaker` | A stateful appliance registered under a schema key, storing versions as it operates. |
| `example/job` | The same domain modeled as a job flow with dependent objects. |
| `example/graph` | Dependency graph primitives used by the job engine. |

```bash
go run ./example/coffeemaker
go run ./example/job
```

## Build and test this repository

The repository ships a Makefile (see the [README](../README.md#makefile) for all targets):

```bash
make build   # build cmd/api to main.exe
make test    # run the test suite
make run     # run the HTTP API server
```
