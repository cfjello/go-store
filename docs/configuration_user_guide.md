# Configuration

Configuration is deliberately simple: a `config` package with typed defaults, a `DefaultEnv` map of environment variable fallbacks, and a `util.SetEnv()` helper that applies them. Environment variables always win; the defaults only fill gaps.

## How defaults are applied

```go
import "github.com/cfjello/go-store/pkg/util"

util.SetEnv() // set every DefaultEnv entry that is not already in the environment
```

`SetEnv` is called automatically by `database.New` and by the HTTP server, so in most programs you never call it yourself. It never overwrites a variable you have already set — export real values (or use a `.env` file, loaded automatically by `cmd/api` via `godotenv`) to override the defaults.

## Environment variables

| Variable | Default | Used by |
|---|---|---|
| `PORT` | `9090` | HTTP server listen port |
| `APP_ENV` | `local` | Free-form environment label |
| `SQLITE_DB_URL` | `file:F:/Sqlite3/go-store.db` | Main database — unset or `:memory:` for in-memory |
| `SQLITE_DB_FLAGS` | `?_journal_mode=WAL&_sync=normal&_mode=rwc&_busy_timeout=5000` | SQLite DSN flags |
| `SQLITE_MAX_CONNS` | `4` | Connection pool size (file mode) |
| `SQLITE_ATTACH_URL` | unset | Persist-layer file database (in-memory mode) |
| `FILE_DB_ALIAS` | `persist` | Alias of the attached persist layer |
| `CGO_ENABLED` | `1` | Required for the SQLite driver |
| `ENV_SOURCED` | `true` | Marker that defaults were applied |
| `HIERARCHY_KEY_SIZE` | `4` | Reserved for hierarchical key support |
| `PERF_ENABLED` | `false` | Shallow function-level monitoring (`true`/`1`/`yes` to enable) |
| `PERF_REPORT_INTERVAL_MS` | `60000` | Periodic top-N log dump interval; `0` disables the ticker |
| `PERF_REPORT_TOP_N` | `10` | How many functions the log dump prints |
| `REST_ENABLE_WRITES` | `false` | Allow mutating REST endpoints (`true`/`1`/`yes`); the API is read-only by default |
| `REST_MAX_BODY_BYTES` | `1048576` | JSON request body cap for the HTTP server |
| `CORS_ALLOWED_ORIGINS` | `*` | CORS allow-list origin header |

See the [Database guide](database_user_guide.md) for how the `SQLITE_*` variables interact, the [Performance guide](performance_user_guide.md) for `PERF_*`, and the [HTTP Server guide](server_user_guide.md) for `REST_*`/`CORS_*`.

## The Config struct

`config.DefaultConfig()` returns the built-in configuration:

```go
cfg := config.DefaultConfig()

cfg.Nodes          // NodeConfig: job thresholds, timers (used by monitoring logic)
cfg.MonitorDefaults// MonitorDefaults: monitor name, port 9999, RunServer
cfg.Names          // NamesConfig — see below
cfg.KvDatabase     // KvDatabase: reserved for a Deno KV backend
cfg.Sqlite3        // Sqlite3: flags/file (empty in the shipped defaults; env vars win)
cfg.DefaultEnv     // map[string]string applied by util.SetEnv
```

### Names that change behavior

`NamesConfig` defines two names the store depends on:

| Field | Default | Meaning |
|---|---|---|
| `Names.DataName` | `data` | Name of the field that holds the persisted payload (`Data`, `data`, or `json:"data"`). Required on objects that implement the update method. When the field is missing and the object has no update method, the whole object is stored as static data. |
| `Names.UpdMethod` | `SetData` | **The no-argument method called on cached objects during job runs and iterator restock.** Objects without this method are registered as `Fixed` static data. Objects with this method must also have the data field. If your domain uses another name (say `Refresh`), change it before creating the store and implement that method on your objects. |

The store reads these at construction; build your own `Config`-backed setup before `store.New` if you need different names.

## Recommended setup order

1. Export environment variables (or provide `.env`).
2. Optionally call `util.SetEnv()` explicitly — otherwise the first `database.New`/`NewServer` does it.
3. Create the store: `store.NewStoreService(false)`.

Because the store and database are singletons, configuration must be settled **before the first construction** — later changes to environment variables have no effect on the running process.

## Example: file-backed local development

```bash
# .env or shell exports
PORT=9090
SQLITE_DB_URL=file:/var/data/go-store.db
SQLITE_MAX_CONNS=8
```

```go
func main() {
	util.SetEnv()
	s := store.NewStoreService(false) // file-backed, WAL, pool of 8
	_ = s
}
```
