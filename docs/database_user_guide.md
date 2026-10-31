# Database and Persistence

All persistence is SQLite, accessed through the internal `database` package. You normally interact with it through the [store](store_user_guide.md); this guide explains the modes, the environment variables, and the storage model for when you need to tune or inspect it.

## Modes

`SQLITE_DB_URL` selects the main database when the singleton is first created:

| `SQLITE_DB_URL` | Mode | Connection pool | Persistence |
|---|---|---|---|
| `:memory:` (must be set explicitly) | In-memory | Pinned to **1** connection (a second pooled connection would open its own empty database) | Gone at process exit, unless a persist layer is attached |
| `file:/path/to/store.db` | File | `SQLITE_MAX_CONNS` (default 4); WAL mode allows concurrent readers with a single writer | Everything is persisted |

> **Unset is not in-memory.** `util.SetEnv()` fills an empty `SQLITE_DB_URL` with the package default file path (`file:F:/Sqlite3/go-store.db`) before the database opens. In-memory mode is only reached by setting `:memory:` explicitly before the singleton is created.

Default DSN flags (file mode):

```
?_journal_mode=WAL&_sync=normal&_mode=rwc&_busy_timeout=5000
```

Override them with `SQLITE_DB_FLAGS`. `_busy_timeout` absorbs `SQLITE_BUSY` under concurrent writers.

The parent directory of a file DSN is created automatically. The database is a **process-wide singleton**: the first `database.New(reset)` (or `store.NewStoreService(reset)`) decides the mode for the whole process, and `reset` is honored on that first call only.

## The persist layer

In-memory mode may attach one file database as a persist layer:

```
SQLITE_DB_URL=:memory:
SQLITE_ATTACH_URL=file:/path/to/persist.db
FILE_DB_ALIAS=persist        # default when unset
```

With a persist layer attached, only explicit `Publish` writes (or upserts with `Publish: true`) are copied to disk; ordinary `Set` versions stay in memory. Reads fall back to the persist layer when a row is missing from the main database. A file-mode main database is already persistent, so nothing is attached there.

At runtime you can manage the attachment directly:

```go
err := db.AttachFileDB("file:/path/to/persist.db", "persist")
err = db.DetachFileDB("persist")
```

## Storage model

Four tables hold everything (created in both the main and the attached schema):

| Table | Contents |
|---|---|
| `meta` | One row per registered key: `meta_key`, `meta_parent_id` (the schema/parent key), `meta_json` (the `types.MetaData` document). |
| `data` | One row per object version: `data_id` (the store ID, a ULID), `meta_key`, `job_id`, `data_json` (the marshaled payload: the object's `data` field when present, otherwise the whole object). Primary key `(data_id, job_id)`. |
| `graph` | Stored dependency/run graphs, keyed by `graph_id`. |
| `job` | Job definitions, keyed by `(job_id, meta_key)`. |

Every write is a **new row**, never an update-in-place: versions are immutable and time-ordered by their ULID store IDs. `GetCurrStoreID(key)` finds the newest version of a key; the store's `Get` then fetches that row. `UnRegister` only stamps the metadata's `SoftDel` field — history is preserved.

Prepared statements are built once at startup (`PrepareStmts`) and reused for the life of the process.

## Low-level service API

For tooling and tests the `DBService` exposes:

```go
db := database.New(false)              // singleton
ok := db.SetData(key, args)            // insert a version
ok = db.UpsertData(key, args)          // insert-or-update a version
val, err := db.GetData(storeID, &out)  // fetch one version (falls back to persist layer)
ok = db.SetMeta(key, meta)             // write metadata
meta, err := db.GetMeta(key, schemaKey)
id, err := db.GetCurrStoreID(key)
health := db.Health()                  // map with status + pool statistics
err = db.Close()                       // detaches the persist layer, closes, clears the singleton
```

`Health()` returns `status: up/down` plus `database/sql` pool statistics (open/in-use/idle connections, wait counts, close counters) and a human-readable `message`; it is what the HTTP server's `/health` endpoint serves.

## Environment variable reference

| Variable | Default | Purpose |
|---|---|---|
| `SQLITE_DB_URL` | `file:F:/Sqlite3/go-store.db` (package default) | Main database — set `:memory:` explicitly for in-memory |
| `SQLITE_DB_FLAGS` | `?_journal_mode=WAL&_sync=normal&_mode=rwc&_busy_timeout=5000` | DSN query flags |
| `SQLITE_MAX_CONNS` | `4` | Pool size in file mode |
| `SQLITE_ATTACH_URL` | unset | Persist-layer file to attach in in-memory mode |
| `FILE_DB_ALIAS` | `persist` | Schema alias for the attached persist layer |
| `CGO_ENABLED` | `1` | Required by `mattn/go-sqlite3` |

> The shipped defaults point at a Windows path (`F:/Sqlite3/...`). Set `SQLITE_DB_URL` explicitly (or unset it for in-memory) on other platforms. See the [Configuration guide](configuration_user_guide.md) for how defaults are applied.

## Operational notes

- **Tests share one database.** The singleton means every test in a package binary uses the same store; use unique key prefixes and do not re-run with `go test -count>1` against stateful assumptions.
- **Close clears the singleton.** After `db.Close()`, the next `database.New` opens a fresh connection.
- **Concurrency.** In in-memory mode everything is serialized through one connection; use file mode with WAL for concurrent readers.
