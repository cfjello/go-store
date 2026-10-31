# Web Server Interface Design

Proposed HTTP interface for the go-store server ([internal/server](../internal/server/server.go), entrypoint [cmd/api](../cmd/api/main.go)). It exposes three resource groups:

| Group | Backing package | Purpose |
|---|---|---|
| `/store` | [pkg/store](../pkg/store/Store.go) | Access registered objects: data versions and metadata |
| `/job` | [pkg/job](../pkg/job/Job.go) | Access job definitions, run graphs, and executions |
| `/perf` | [pkg/perf](../pkg/perf/perf.go) | View and control performance metrics |

## Implementation status

Implemented, with these deviations from the original proposal:

- **Write gate instead of auth (interim).** All mutating endpoints exist but are disabled by default: `REST_ENABLE_WRITES=false` (config `restApi.enableWrites`) makes every `POST`/`PUT`/`PATCH`/`DELETE` return `403 writes_disabled`. Token/role auth remains open (risk #1) and is required before enabling writes on an untrusted network.
- **Iterator streaming endpoint deferred** to a later version, as flagged below.
- **`exec` is synchronous** with no `timeout` parameter; the server's 30-second write timeout bounds the response.
- Soft-deleted objects return `410` with code `deleted` (not `404`) on `GET /store/objects/{key}`.
- CORS origins are configurable via `CORS_ALLOWED_ORIGINS` (default still `*`).
- Hard-delete/purge of versions was **not** added; soft-delete retention semantics apply (risk #6).

New supporting code: `DBService.ListKeys`/`ListVersions` ([internal/database/list.go](../internal/database/list.go)), `JobService.Status`/`LatestRunGraph` ([pkg/job/status.go](../pkg/job/status.go)), `perf.Settings`/`CurrentSettings` ([pkg/perf/reporter.go](../pkg/perf/reporter.go)), REST settings in [pkg/config/config.go](../pkg/config/config.go). Handlers: `internal/server/{store,job,perf}_handlers.go`; shared helpers in `internal/server/api.go`.

---

## Conventions

- **Format**: JSON in and out, `Content-Type: application/json`. Timestamps/durations as strings (Go `time.Duration` style, matching `/health` and `/perf/stats` today).
- **Keys and IDs**: store keys are application-chosen strings; store IDs and job IDs are time-ordered ULIDs. To be URL-safe, keys must match `[A-Za-z0-9._~-]{1,128}`. The server validates this on every route (see Security).
- **Errors**: a single shape for all non-2xx responses:
  ```json
  { "error": { "code": "not_found", "message": "object \"orders\" is not registered" } }
  ```
  `code` is a stable machine-readable token; `message` is human-readable and must not contain internal details (SQL text, file paths).
- **Status codes**: `200` OK, `201` created, `202` accepted (async), `204` no content, `400` bad request, `401`/`403` auth, `404` unknown key/ID, `409` conflict (already registered, soft-deleted, Fixed/Init violation, dependency cycle), `413` body too large, `429` rate limited, `500` internal.
- **Pagination** on list endpoints: `limit` (default 100, max 1000) and `cursor` (opaque; ULID-based, so cursors are stable and time-ordered). Responses carry `nextCursor` when more pages exist.
- **Versioning**: prefix routes with `/v1` (e.g. `/v1/store/...`) if a stable public contract is needed; omit while the API is internal.

## 1. `/store` endpoints

Backed by the `Store` methods `Register`, `Set`, `Publish`, `Get`, `GetMeta`, `UnRegister`, `GetCurrStoreID`, the `*Deps` family, and the `*Notify` family.

### Objects and data

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/store/keys` | List registered keys. Params: `prefix`, `includeDeleted` (default false), `limit`, `cursor`. **[new support needed]** — no list-all query exists in `internal/database` today. | — |
| `POST` | `/store/objects` | Register an object. Body: [RegisterArgs](../pkg/types/types.go) — `key`, `schemaKey`, `object` (JSON payload), `init`, `check`, `fixed`, `alwaysRun`, `depends`, `notifyTo`. `201` → `MetaData`; `409` if the key is already registered. | `Store.Register` |
| `GET` | `/store/objects/{key}` | Get data. Params: `storeId` (pin a historical version; default = latest via `GetCurrStoreID`). `404` unknown key, `410`-style `deleted` error if soft-deleted. | `Store.Get` |
| `PUT` | `/store/objects/{key}` | Store a new version. Body: `data` (payload), `check` (`none`/`check`/`skip`), `jobId`, optional `fixed`/`alwaysRun` flag flips. → updated `MetaData`. | `Store.Set` |
| `POST` | `/store/objects/{key}/publish` | Publish a version (no validation, triggers notify list). Body: `data`, `jobId`. | `Store.Publish` |
| `DELETE` | `/store/objects/{key}` | Soft-delete (sets `SoftDel`). `204`. Historical versions remain fetchable by `storeId` — see Security. | `Store.UnRegister` |
| `GET` | `/store/objects/{key}/versions` | List version IDs (newest first). Params: `limit`, `cursor`. **[new support needed]** — needs a per-key store-ID history query; `GetMaxDataIDs` only returns the max. | — |

Example — register and write:

```bash
curl -X POST localhost:9090/store/objects -d '{
  "key": "orders-42",
  "object": {"lines": [], "total": 0},
  "init": true, "check": true
}'
curl -X PUT localhost:9090/store/objects/orders-42 -d '{
  "data": {"lines": [{"sku": "espresso", "qty": 1}], "total": 3.5},
  "check": "check"
}'
curl localhost:9090/store/objects/orders-42            # latest
curl "localhost:9090/store/objects/orders-42?storeId=01J..."  # pinned version
```

### Metadata

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/store/objects/{key}/meta` | Full `MetaData`: `init`, `oper`, `fixed`, `check`, `alwaysRun`, `deleted`, `depends`, `notifyTo`, `typeInfo`, current `storeId`. | `Store.GetMeta`, `Store.GetCurrStoreID` |
| `PATCH` | `/store/objects/{key}/meta` | Update mutable flags only (`check`, `alwaysRun`, `fixed` while uninitialized). Body is a partial object; rejected fields → `400`. Prefer this over exposing raw `SetMeta`. | `Store.SetMeta` (guarded) |

### Dependencies

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/store/objects/{key}/deps` | List dependency keys. | `Store.GetDeps` |
| `PUT` | `/store/objects/{key}/deps` | Replace the list. Body: `{"keys": ["a","b"]}`. | `Store.SetDeps` |
| `POST` | `/store/objects/{key}/deps` | Add keys, or union another object's list. Body: `{"keys": [...]}` or `{"merge": ["srcKey"]}`. | `Store.AddDeps`, `Store.MergeDeps` |
| `DELETE` | `/store/objects/{key}/deps` | Remove keys. Body: `{"keys": [...]}`. | `Store.RemoveDeps` |

All mutations validate that targets are registered (existing `ValidateDeps` behavior); unknown keys → `400`, dependency cycles → `409`.

### Notification lists

Same shape as dependencies:

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/store/objects/{key}/notify` | List notify targets. | `Store.GetNotify` |
| `PUT` / `POST` / `DELETE` | `/store/objects/{key}/notify` | Replace / add / remove targets, body `{"keys": [...]}`. | `Store.SetNotify` / `AddNotify` / `RemoveNotify` |

### Iteration (optional, v2)

`GET /store/objects/{key}/iter?storeId=&restock=` streaming elements as NDJSON or Server-Sent Events, mapping `Store.OpenIter`. Needs hard caps: max elements, max stream duration, one stream per client — otherwise this is an easy resource-exhaustion vector (see Security).

## 2. `/job` endpoints

Backed by `JobService`: `DepsGraph`, `ExecutionOrder(LeavesFirst)`, `RunGraph`, `ExecRunGraph`, `Notify`, `WaitIdle`.

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/job/definitions/{key}` | Build the dependency DAG for `key` (transient; fresh `jobId` per call). Response: vertices (`key`, `level`, `fixed`, `alwaysRun`) and edges. | `JobService.DepsGraph` |
| `GET` | `/job/definitions/{key}/order` | Execution order. Param: `direction=root-first` (default) or `leaves-first`. → `{"order": ["leaf", ..., "root"]}`. | `ExecutionOrder` / `ExecutionOrderLeavesFirst` |
| `POST` | `/job/run-graphs` | Build **and persist** a run graph snapshot. Body: `{"rootKey": "orders-42"}`. `201` → `RunGraph` (`rootKey`, `rootStoreId`, `order`, `nodes`). Published under `rootKey + "_job"`; the new `jobId` is the snapshot's store ID. | `JobService.RunGraph` |
| `GET` | `/job/run-graphs/{key}` | Fetch the newest stored run graph for root `key` (reads `key + "_job"` at its current store ID). `404` when none has been built. | `GetCurrStoreID` + `DB.GetData` |
| `POST` | `/job/run-graphs/{key}/exec` | Execute the newest stored run graph. Param: `timeout` (ms, server-capped). → the updated `RunGraph` with refreshed store IDs / dirty flags. Long-running; see Security. | `JobService.ExecRunGraph` |
| `POST` | `/job/notifications` | Trigger an asynchronous update run for a key. Body: `{"key": "orders-42"}`. `202` (runs are serialized per key and coalesced — duplicate triggers collapse into one pending re-run). | `JobService.Notify` |
| `GET` | `/job/status` | In-flight runs and per-key run state. **[new support needed]** — requires exposing `runs`/`inFlight` counters from `JobService`. | — |

Notes:

- `RunGraph`/`DepsGraph` return `409` with a cycle error when dependencies are cyclic (existing `PreventCycles` + explicit cycle detection).
- `ExecRunGraph` never runs `Fixed` nodes; callers can inspect `fixed`/`alwaysRun` on the returned nodes to understand what executed.

## 3. `/perf` endpoints

Backed by [pkg/perf](../pkg/perf/perf.go). Registered only when perf is enabled — but see the gating note below.

| Method | Path | Description | Maps to |
|---|---|---|---|
| `GET` | `/perf/stats` | *(exists today)* All function counters. New params: `sort=total` (default) or `calls`, `top=N` (truncate), `name=<substring>` filter. → array of `{name, calls, total, min, max, last}`. | `perf.Snapshot` / `perf.Top` |
| `GET` | `/perf/config` | Current state: `{"enabled": bool, "reportIntervalMs": n, "reportTopN": n}`. | `perf.Enabled` + env |
| `PUT` | `/perf/enabled` | Toggle recording at runtime. Body: `{"enabled": true}`. Disabled `Track` is a single atomic load, so the off state is cheap. | `perf.SetEnabled` |
| `POST` | `/perf/reset` | Zero all counters (names are kept). `204`. Admin-only (see Security). | `perf.Reset` |

**Gating change**: today the `/perf/stats` route is only registered when `PERF_ENABLED` is on at startup. To support runtime toggling, register the routes unconditionally and check `perf.Enabled()` inside the handlers (`404` or `409 disabled` when off).

## Security risks and mitigations

The current server has **no authentication, wildcard CORS, and plain HTTP**. Exposing these endpoints as-is would give any reachable caller full read/write access to all stored data and the ability to trigger arbitrary recomputation. Risks, ordered by severity:

1. **No authentication / authorization (critical).** Every endpoint is anonymous. `PUT /store/objects/{key}`, `DELETE`, and `/job/.../exec` are remotely exploitable data-integrity and compute-theft holes.
   - *Mitigate*: bearer-token or API-key middleware on all `/store`, `/job`, `/perf` routes; keep `/` and `/health` public for probes. Define roles: `reader` (GET only), `writer` (+ PUT/POST/DELETE on `/store`), `operator` (+ `/job` exec/notify), `admin` (+ `/perf/reset`, `/perf/enabled`). Alternatively bind to `127.0.0.1` / a private interface and front with an authenticating proxy.

2. **Permissive CORS.** `Access-Control-Allow-Origin: *` in [routes.go](../internal/server/routes.go) lets any website issue cross-origin requests from a visitor's browser.
   - *Mitigate*: restrict to an explicit origin allow-list via env config; keep `Allow-Credentials: false` and require the auth header (not cookies) so browsers can't attach ambient authority.

3. **Denial of service.**
   - *Compute*: `/job/run-graphs/{key}/exec`, `POST /job/run-graphs`, and `DepsGraph` walk and rebuild graphs; notify storms can keep the run queue busy. *Mitigate*: per-role rate limits (`429`), a cap on concurrent executions, and a server-side `timeout` cap.
   - *Payload*: unbounded JSON bodies exhaust memory during unmarshal. *Mitigate*: `http.MaxBytesReader` (e.g. 1–10 MB) and `413` on overflow.
   - *Streaming*: the proposed iterator endpoint with `restock=true` can stream forever. *Mitigate*: max elements/duration per stream, or defer the endpoint to v2.
   - *Note*: notification runs are already serialized per key with a single coalesced pending re-run, which bounds the worst case — keep that invariant server-side.

4. **Key handling and injection.** Keys flow into SQL (prepared statements — good, keep it that way) and into logs and URL paths. Arbitrary keys allow log forgery (newlines) and route ambiguity (slashes).
   - *Mitigate*: enforce the key grammar `[A-Za-z0-9._~-]{1,128}` at the router, reject everything else with `400`. Never build SQL with string concatenation.

5. **Information disclosure.** `MetaData.typeInfo` exposes object schemas, `/perf/stats` exposes internal function names and timing (useful for fingerprinting and timing attacks), and verbatim internal errors can leak SQL or filesystem paths.
   - *Mitigate*: sanitize error bodies (stable `code` + generic `message`; log details server-side only), gate `/perf` behind `reader`/`admin` auth, and consider omitting `typeInfo` from metadata responses for non-admin callers.

6. **Soft-delete is not deletion.** `DELETE` only stamps `SoftDel`; every historical version remains retrievable by `storeId` via `GET /store/objects/{key}?storeId=...`. This conflicts with data-retention/GDPR-style erasure expectations.
   - *Mitigate*: document the retention semantics in the endpoint docs; if true erasure is needed, add an admin-only `DELETE /store/objects/{key}/versions/{storeId}` purge **[new support needed]** and never expose it without auth.

7. **No transport security.** Plain HTTP exposes auth tokens and payloads on the wire.
   - *Mitigate*: terminate TLS (in-process `ListenAndServeTLS` or a reverse proxy) before deploying beyond localhost.

8. **Type-consistency caveat for HTTP writes.** Checked writes compare the payload against the `TypeInfo` snapshot taken at `Register` time from a Go value. JSON round-trips change shapes (e.g. `int` → `float64`, field order), so `PUT` with `check=check` on an object registered in-process may fail type comparison or, worse, coerce silently.
   - *Mitigate*: document that HTTP-registered objects are schema-compared as JSON; recommend registering schemas with representative JSON values, or use `check=none`/`skip` deliberately. Also note: HTTP-registered objects have no Go update method, so `Register` forces `fixed=true` and jobs never recompute them — surface this in the register response.

9. **Dangerous capabilities must stay off the wire.** `DBService.AttachFileDB`/`DetachFileDB` accept filesystem paths, and `database.New(reset=true)` drops tables. No endpoint may accept file paths, SQL, or a reset flag.

10. **Auditability.** Mutating calls (register, set, publish, delete, deps/notify changes, exec, perf reset) should emit an audit log line with caller identity, key, and outcome. The existing log file (`logs/go-store.log`) is the natural sink.

## Required codebase additions (summary)

- `internal/database`: list-all-keys query (with prefix/pagination); per-key store-ID history query; hard-delete (optional).
- `pkg/job`: exported run-status snapshot for `GET /job/status`.
- `internal/server`: auth middleware, rate limiter, body-size limit, key-grammar validator, sanitized error encoder, CORS allow-list config; always-register `/perf` routes with handler-time gating.
- Keep handlers thin: the server remains a transport layer over `store`/`job`/`perf`, per the [server user guide](server_user_guide.md).
