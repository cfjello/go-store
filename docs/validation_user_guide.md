# Validation

When `Check` is on, a `Set` first compares the **persisted payload type** against `MetaData.TypeInfo`, then validates struct tags on the **cached live object** with [`github.com/go-playground/validator/v10`](https://pkg.go.dev/github.com/go-playground/validator/v10).

## Payload type check

`Register` records `TypeInfo` of:

1. the `data` field (`Data` / `data` / `json:"data"`, see `NamesConfig.DataName`) when that field exists, or
2. the whole object when it is static — no `data` field and no update method (`SetData` by default).

A later checked `Set` builds `TypeInfo` of `storedPayload(args.Object)` the same way and rejects a mismatch. Passing the wrapper or the payload value both succeed when they share that payload type.

```go
// Registered as *updatableObj with a dataPayload field named Data.
s.Set(types.SetArgs{Key: "maker-1", Object: dataPayload{N: 3}, Check: types.CheckOn}) // ok
s.Set(types.SetArgs{Key: "maker-1", Object: &otherStruct{}, Check: types.CheckOn})     // type mismatch
```

Tag validation still runs against the **cached live object** — the pointer you passed to `Register` — not against the JSON being written.

## Tag your structs

```go
type CoffeeMaker struct {
	Brand         string `json:"brand"         validate:"required"`
	WaterLevel    int    `json:"water_level"   validate:"gte=0,lte=1000"`
	CoffeeLevel   int    `json:"coffee_level"  validate:"gte=0"`
	Temperature   int    `json:"temperature"   validate:"gte=0,lte=100"`
}
```

Any tag supported by go-playground/validator works (`required`, `gte`, `lte`, `email`, `oneof`, nested structs, …).

## Turn validation on

Validation is controlled per object by the `Check` flag at registration:

```go
s.Register(types.RegisterArgs{
	Key:    "maker-1",
	Object: &maker,
	Check:  true, // validate before every Set
})
```

## Per-write override with CheckMode

`SetArgs.Check` overrides the registered flag for a single write:

| Value | Effect |
|---|---|
| `types.CheckNone` (zero value, `""`) | Defer to the registered `Check` flag. |
| `types.CheckOn` | Force validation for this write. |
| `types.CheckSkip` | Skip validation for this write, even when `Check` is registered. |

```go
// Force validation for one write:
s.Set(types.SetArgs{Key: "maker-1", Object: maker, Check: types.CheckOn})

// Skip validation for a trusted batch write:
s.Set(types.SetArgs{Key: "maker-1", Object: maker, Check: types.CheckSkip})
```

A failed validation aborts the `Set` — nothing is written and no notifications fire:

```go
maker.WaterLevel = -5
_, err := s.Set(types.SetArgs{Key: "maker-1", Object: maker})
// err: validation failed for key "maker-1": ...
```

## Validate on demand

```go
err := s.ValidateCached("maker-1")
```

`ValidateCached` dereferences pointers/interfaces down to the underlying struct and validates it. It fails when:

- the key has no cached object,
- the cached object is nil, or
- the cached object is not (a pointer to) a struct.

## Keep the cache truthful

Because validation targets the cached object, a `Set` that stores a *different* value than the cache holds validates the wrong thing. The idiomatic pattern is to mutate the registered pointer and then store it:

```go
maker.WaterLevel = 750            // mutate the registered (cached) object
_, err := s.Set(types.SetArgs{Key: "maker-1", Object: maker})
```
