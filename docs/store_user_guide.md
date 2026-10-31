# Core Store

The `store` package is the heart of go-store: a versioned, key-addressed object store backed by SQLite. Every stored object is registered under a unique key, described by metadata, and written as immutable, ULID-addressed versions.

## Creating a store

```go
// Singleton: the first call creates the store (and its database), later calls
// return the same instance. reset is honored on the first call only.
s := store.NewStoreService(false)

// Or build one around an existing database service:
db := database.New(false) // nil/":memory:" SQLITE_DB_URL => in-memory
s := store.New(db, false)
```

`reset=true` drops and recreates the schema on first construction — useful in tests, destructive otherwise.

## Registering objects

`Register` puts a key under store management and caches the live object for later use (validation, iterators, job runs):

```go
meta, err := s.Register(types.RegisterArgs{
	Key:       "maker-1",        // required, unique object key
	Object:    &CoffeeMaker{},   // required: pointer to the live object
	Init:      true,             // also write the first data version
	Check:     true,             // run struct-tag validation on every Set
	Fixed:     false,            // Fixed objects are excluded from job re-runs
	AlwaysRun: false,            // AlwaysRun objects re-run on every job execution
	ParentKey: "CoffeeMaker",    // optional schema/parent key; inherits deps + notify list
	DependsOn: types.Dependencies{DependsOn: []string{"water-1", "beans-1"}},
	NotifyTo:  types.NotifyList{"dashboard"},
})
```

Rules:

- `Key` and a non-nil `Object` are required.
- If the object does **not** implement the store's update method (`SetData` by default), it is treated as a standalone static data object and `Fixed` is set to `true` even when the caller passed `false`.
- If it **does** implement the update method, it must also have a `data` field (`Data`, `data`, or `json:"data"`); otherwise `Register` returns an error.
- `Init: true` performs registration **and** the first write atomically from the caller's perspective; a failed write returns an error with `meta.Init` flipped back to `false`.
- When `ParentKey` differs from `Key`, the child merges the parent's dependency list and notify list with its own.
- `Register` records reflection-based type information (`MetaData.TypeInfo`) for the **persisted payload**: the `data` field when present, otherwise the whole object (static data with no `data` field and no update method).

`IsRegistered(key)` (alias `Has(key)`) reports whether a key is registered. `IsPublished(key)` reports whether it has been initialized with data.

## Writing versions: Set

Every `Set` writes a **new version** of the object. Versions are addressed by a fresh [ULID](https://github.com/oklog/ulid) store ID — time-ordered, so the largest ID is the newest:

```go
meta, err := s.Set(types.SetArgs{
	Key:       "maker-1",       // must be registered
	Object:    maker,           // wrapper or payload; the data field is what is stored
	StoreId:   "",              // normally empty; a new ULID is generated per version
	JobId:     "job-42",        // optional run correlation ID; defaults to the store ID
	Check:     types.CheckNone, // CheckOn/CheckSkip override the registered Check flag
	ParentKey: "",              // defaults to Key
	Fixed:     nil,             // nil keeps the registered flag; types.Ptr(true) changes it
	AlwaysRun: nil,
})
```

- `GetCurrStoreID(key)` returns the newest store ID for a key.
- The persisted JSON is the object's **data payload**: an exported field named `Data` / `data`, or tagged `json:"data"` (see `NamesConfig.DataName`). If that field is absent, the whole object is stored — the standalone static case.
- `Fixed` may only be flipped via `SetArgs.Fixed` **before** the object is initialized; afterwards it is frozen.
- `Check: types.CheckOn` type-checks the write payload against `MetaData.TypeInfo` and validates the cached object before writing (see the [Validation guide](validation_user_guide.md)).
- A successful `Set` fires change notifications to every object on the key's notify list (see [Notifications](notifications_user_guide.md)).

## Publishing

`Publish` writes a version like `Set` but additionally copies it to the attached persist layer when one is configured (see [Database modes](database_user_guide.md#the-persist-layer)):

```go
ok := s.Publish(types.SetArgs{Key: "maker-1", Object: maker})
```

Publish skips registration metadata checks and returns a plain `bool`. It also fires change notifications.

## Reading objects

```go
var maker CoffeeMakerModel
obj, err := s.Get("", "maker-1", &maker) // "" resolves the latest version
// obj == &maker; err non-nil on failure

pinned, err := s.Get("01J9X7...", "maker-1", &maker) // exact version by store ID
```

- Pass an empty `storeID` to read the newest version; pass a store ID to pin one.
- `userObj` must be a non-nil pointer. The stored JSON is unmarshaled into `userObj[dataName]` when that field exists, otherwise into `userObj` itself. The pointer that was filled (the data field or the whole object) is returned.
- The special ID `"0000"` is the store's zero value and is rejected.

## Metadata

Each registered key has a `types.MetaData` record:

```go
meta, err := s.GetMeta("maker-1") // optional second arg: schema key fallback
ok := s.SetMeta("maker-1", meta)  // persist a modified record
```

Important fields: `Key`, `Init`, `Fixed`, `Check`, `AlwaysRun`, `ParentKey`, `DependsOn`, `NotifyTo`, `SoftDel`, `TypeInfo`. Prefer the dedicated helpers (`SetDeps`, `SetNotify`, …) over editing metadata by hand — they validate their input.

## Removing objects: soft delete

```go
ok := s.UnRegister("maker-1")
```

`UnRegister` performs a **soft delete**: the metadata's `SoftDel` field is stamped with a new ULID. The key stops resolving (iterators refuse to open it), but historical versions remain in the database.

## The object cache

`Register` caches the live object pointer under its key. The cache drives validation, iterator restock, and job execution:

```go
obj, ok := s.GetCache("maker-1")
s.SetCache("maker-1", obj)   // replace
s.DeleteCache("maker-1")     // remove one
s.ClearCache()               // remove all
```

Keep the cached pointer in sync with the data you `Set` — jobs and iterators call methods on the cached object, not on a freshly loaded copy.

## Error conventions

- `Register`, `Set`, `Get`, `GetMeta` return descriptive errors; empty keys and nil objects are rejected up front.
- `Publish`, `SetMeta`, `UnRegister`, `IsRegistered`, `Has`, `IsPublished` report through `bool`.

## Next steps

- [Dependencies](dependencies_user_guide.md) — declare what an object is built from.
- [Notifications](notifications_user_guide.md) — push change events to consumers.
- [Jobs](jobs_user_guide.md) — recompute objects in dependency order.
