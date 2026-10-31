# HTTP Server

The `internal/server` package plus `cmd/api` provide an HTTP front end over the store, job, and perf services. See [web_server_interface.md](web_server_interface.md) for the full interface design and security analysis.

## Run it

```bash
make run           # go run cmd/api/main.go
# or
make build && ./main.exe
```

The server listens on `PORT` (default `9090`):

```
Server is running on port 9090
```

## Configuration

| Setting | Env var | Default | Effect |
|---|---|---|---|
| `restApi.enableWrites` | `REST_ENABLE_WRITES` | `false` | When `false`, every mutating endpoint (`POST`/`PUT`/`PATCH`/`DELETE`) fails with `403 writes_disabled` — the API is read-only. Set to `true`/`1`/`yes` to allow writes. |
| `restApi.maxBodyBytes` | `REST_MAX_BODY_BYTES` | `1048576` | JSON request body cap; larger bodies fail with `413 body_too_large`. |
| `restApi.allowedOrigins` | `CORS_ALLOWED_ORIGINS` | `*` | CORS allow-list origin header. |

## Conventions

- All responses are JSON. Errors use a single envelope:
  `{"error": {"code": "not_found", "message": "..."}}` with a stable machine-readable `code`.
- Store keys in URLs must match `[A-Za-z0-9._~-]{1,128}` (`400 invalid_key` otherwise). Store IDs and job IDs are ULIDs.
- List endpoints paginate with `limit` (default 100, max 1000) and `cursor`, returning `nextCursor` when more pages exist.
- `PUT`/`POST` bodies require `Content-Type: application/json` (`415` otherwise).

## Endpoints

### Probes

| Method | Path | Response |
|---|---|---|
| `GET` | `/` | `{"message": "Hello World"}` — liveness probe. |
| `GET` | `/health` | JSON map from `DBService.Health()`: `status` (`up`/`down`), a human-readable `message`, and `database/sql` pool statistics. |

### Store

| Method | Path | Description |
|---|---|---|
| `GET` | `/store/keys` | List registered keys. Params: `prefix`, `includeDeleted`, `limit`, `cursor`. |
| `POST` | `/store/objects` | **Write.** Register an object (`key`, `schemaKey`, `object`, `init`, `fixed`, `alwaysRun`, `depends`, `notifyTo`). `201` → `MetaData`; `409` if already registered. HTTP-registered objects are plain JSON: they have no Go update method (so `fixed` is forced on) and no struct tags (so `check` is forced off at register time). |
| `GET` | `/store/objects/{key}` | Get data; `?storeId=` pins a historical version. `404` unknown key, `410 deleted` when soft-deleted. |
| `PUT` | `/store/objects/{key}` | **Write.** Store a new version. Body: `data`, `check` (`none`/`check`/`skip`), `jobId`, optional `fixed`/`alwaysRun`. → `MetaData`. |
| `DELETE` | `/store/objects/{key}` | **Write.** Soft-delete (`204`). Historical versions remain fetchable by `storeId`. |
| `POST` | `/store/objects/{key}/publish` | **Write.** Publish a version without validation; triggers the notify list. |
| `GET` | `/store/objects/{key}/versions` | List version IDs, newest first. Params: `limit`, `cursor`. |
| `GET` | `/store/objects/{key}/meta` | Full `MetaData` including `depends`, `notifyTo`, `typeInfo`, and the `deleted` tombstone. |
| `PATCH` | `/store/objects/{key}/meta` | **Write.** Update `check`, `alwaysRun`, or `fixed` (only while uninitialized; `409 fixed_immutable` afterwards). |
| `GET` | `/store/objects/{key}/deps` | List dependency keys → `{"keys": [...]}`. |
| `PUT` / `POST` / `DELETE` | `/store/objects/{key}/deps` | **Write.** Replace / add (or `{"merge": ["srcKey"]}` to union) / remove dependencies. |
| `GET` | `/store/objects/{key}/notify` | List notify targets → `{"keys": [...]}`. |
| `PUT` / `POST` / `DELETE` | `/store/objects/{key}/notify` | **Write.** Replace / add / remove notify targets. |

### Jobs

| Method | Path | Description |
|---|---|---|
| `GET` | `/job/definitions/{key}` | Dependency DAG for a registered object: `vertices` (`key`, `level`, `fixed`, `alwaysRun`) and `edges` (node → dependency). |
| `GET` | `/job/definitions/{key}/order` | Execution order; `?direction=root-first` (default) or `leaves-first`. Fixed vertices and their descendants are excluded. |
| `POST` | `/job/run-graphs` | **Write.** Build and persist a run graph snapshot. Body: `{"rootKey": "..."}`. `201` → `RunGraph`. |
| `GET` | `/job/run-graphs/{key}` | Newest stored run graph for root `key`. `404` when none was built. |
| `POST` | `/job/run-graphs/{key}/exec` | **Write.** Execute the newest stored run graph (synchronous; the server's 30s write timeout bounds the response). → updated `RunGraph`. |
| `POST` | `/job/notifications` | **Write.** Enqueue an asynchronous update run. Body: `{"key": "..."}`. `202`; runs are serialized per key and coalesced. |
| `GET` | `/job/status` | Notification runner state: `inFlight` count and per-key `runs` with `pending` flags. |

### Performance

The routes are always registered; `GET /perf/stats` reports `404 perf_disabled` while recording is off.

| Method | Path | Description |
|---|---|---|
| `GET` | `/perf/stats` | Function timings. Params: `sort=total` (default) or `calls`, `top=N`, `name=<substring>`. |
| `GET` | `/perf/config` | `{"enabled", "reportIntervalMs", "reportTopN"}` — available regardless of state. |
| `PUT` | `/perf/enabled` | **Write.** Toggle recording at runtime. Body: `{"enabled": true}`. |
| `POST` | `/perf/reset` | **Write.** Zero all counters (`204`); recorded names are kept. |

Example:

```bash
curl http://localhost:9090/health
```

```json
{
  "status": "up",
  "message": "It's healthy",
  "open_connections": "1",
  "in_use": "0",
  "idle": "1",
  "wait_count": "0",
  "wait_duration": "0s",
  "max_idle_closed": "0",
  "max_lifetime_closed": "0"
}
```

## Behavior notes

- **Read-only by default**: with `REST_ENABLE_WRITES=false` (the default) the server only exposes data; all mutations return `403 writes_disabled`. There is no authentication yet — enable writes only on a trusted network or behind an authenticating proxy, and see the security section of [web_server_interface.md](web_server_interface.md).
- **CORS**: origins come from `CORS_ALLOWED_ORIGINS` (default `*` — tighten before exposing the server). `OPTIONS` preflight requests return `204`.
- **Timeouts**: read 10s, write 30s, idle 60s.
- **Startup**: `NewServer` applies environment defaults (`util.SetEnv`), opens the singleton database (`database.New(false)`), and builds the store and job services on top. The database mode therefore follows your `SQLITE_DB_URL` (see the [Database guide](database_user_guide.md)).
- **Logging**: `cmd/api` appends to `logs/go-store.log` with date/time and file:line flags.

## Graceful shutdown

`SIGINT`/`SIGTERM` trigger a coordinated shutdown:

1. The signal handler inside the server closes the database (detaching any persist layer first) and shuts the HTTP server down with a 15-second deadline.
2. `cmd/api` adds a second, outer handler: it stops accepting new connections, gives in-flight requests 5 seconds to finish, then exits. Press `Ctrl+C` a second time to force-quit.

## Extending the server

Add routes in `RegisterRoutes` ([internal/server/routes.go](../internal/server/routes.go)) using Go 1.22+ method patterns:

```go
mux.HandleFunc("GET /objects/{key}", s.objectsHandler)
```

Handlers live in `store_handlers.go`, `job_handlers.go`, and `perf_handlers.go`; shared helpers (error envelope, key validation, body limits, pagination, the write gate) are in `api.go`. The `Server` struct holds the `*database.DBService`, `*store.Store`, `*job.JobService`, and the `config.RestAPI` settings. Keep store-level operations in the `store` package and use the server as a thin transport layer; gate every new mutating handler with `s.requireWrites(w)`.
