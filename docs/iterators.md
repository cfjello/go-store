# Iterators

Iterators live in the `store` package, in [Iterate.go](../pkg/store/Iterate.go). An iterator walks one registered object. It is a pointer you pass between functions. The position and the overall counter live in the iterator, not in the caller.

There is no name registry and no SQLite cursor. `Next` is a shared pull: each call, including a call from another goroutine, consumes the next element.

Walking every store id of a key is not supported. Descending order is not supported.

## Open

```go
it, err := s.OpenIter(store.OpenOpts{
    Key:     "maker-1",
    StoreID: "",          // empty: latest row for Key
    JobID:   "job-1",     // recorded only; not used to select rows
    Restock: true,
})
if err != nil {
    return err
}
defer it.Close()
```

| Field | Rule |
|---|---|
| `Key` | Registered object key. Required when `StoreID` is empty. |
| `StoreID` | One stored row, a ULID from `util.Ulid`. Required when `Key` is empty. `0000` is treated as empty. |
| `JobID` | Stored on the iterator and returned by `JobID`. It does not filter rows. |
| `Restock` | After the current collection is exhausted, call the object's update method for a new store id. |

Provide at least one of `Key` and `StoreID`.

- `StoreID` empty, `Key` set: resolve the latest row with `GetCurrStoreID`, then `Get` that row. That is the highest store id.
- `Key` empty, `StoreID` set: read the row, and use its `meta_key` as the object key. The key must be registered.
- Both set: load that store id. It must belong to `Key`. A newer `Set` does not change this iterator.

Opening fails when the key is missing, not registered, or soft-deleted, when the store id does not exist, or when it belongs to a different key.

`OpenIter` does not keep the pointer. Pass `*store.Iter`. Copying the struct is not safe.

## Read

```go
for {
    el, ok := it.Next(ctx)
    if !ok {
        return it.Err()
    }
    item := el.Value.(Item)
    _ = el.Local   // index in the current store id, resets after restock
    _ = el.Overall // aggregated count, does not reset
    _ = el.StoreID // row this element came from
}
```

`Next` returns one `Element`. `Value` is the element by value. The caller casts it. `Local` is the index inside the collection of that store id, and goes back to 0 when restock binds a new row. `Overall` starts at 0 and counts every element this iterator has yielded. `StoreID` stays with the element, so another caller can restock without changing an element you already took.

`ctx` may be nil. `Next` then uses `context.Background()`. Cancelling `ctx` stops that caller only, and only before a restock call starts. It does not fail the iterator. `Err` stays nil.

A clean end returns `false` and leaves `Err` nil. Check `Err` after `Next` returns false.

| Method | Effect |
|---|---|
| `Next(ctx) (Element, bool)` | Consume one element. `Local` resets per store id. `Overall` does not. |
| `Count()` | How many elements have been yielded. Same as the next overall index. Not reset by restock. |
| `Key()` | Registered key, including one resolved from `StoreID`. |
| `StoreID()` | Store id currently bound. After restock this is the new id. |
| `JobID()` | Job id from open. |
| `Err()` | Sticky restock or load error, or `ErrClosed`. Nil after a clean end. |
| `Close()` | Mark the iterator finished. A second call is a no-op. |

`StoreID()` is the object the iterator is bound to now. Under concurrent `Next` it can move ahead of an element another caller already took. Use `Element.StoreID` for the row an element came from.

## One object, one collection

The stored document is loaded with `Get` into a new value of the registered type, not from the live cache. The cache is only used to know that type and, when restocking, to call the update method.

A slice, array, or map is a collection. Everything else is a collection of one. A struct is one element, not one element per field. A stored graph is one element unless you stored it as a slice or a map.

| Stored value | What `Next` returns |
|---|---|
| `"solo"` or a struct | that value, once |
| `[]string{"a","b"}` | `"a"`, then `"b"` |
| `map[string]string{"b":"2","a":"1"}` | the values, in sorted key order: `"1"`, then `"2"` |
| empty slice | nothing |
| JSON `null` | one nil element |

Map order is sorted with `fmt.Sprint` of the key so a walk is repeatable. That is not a user-facing order option. There is no descending mode.

JSON is decoded into the registered type, so a `[]string` yields strings and a struct yields that struct. A type that does not match the stored JSON fails at open.

The current collection is the window. There is no separate limit field. `Next` returns every element of this store id, then either stops or restocks.

## Restock

Set `Restock: true` when exhausting the current object should load another one.

The method called is the no-argument method named by `Store.UpdMethod()`. The default name is `SetData`, from configuration. It is invoked on the cached registered object, the same object `Register` stored.

```go
func (w *words) SetData() (string, error) {
    if done {
        return "", nil
    }
    id, err := persistNextPage()
    return id, err
}
```

The string result is the new store id. The iterator then:

1. Drops the current collection.
2. Binds that store id.
3. `Get`s the new object.
4. Starts again at its first element.
5. Resets `Local` to 0 and leaves `Overall` where it is. The next element is overall index 2 if two elements were already yielded.

| Return | Result |
|---|---|
| `("01H...", nil)` | Switch to that row and continue. The id must differ from the current one. |
| `("", nil)` | Clean end. `Err` is nil. |
| a non-nil error | Sticky. Later `Next` calls return false. |
| the current store id | Sticky `ErrRestock`. This stops a loop. |
| no string result | Sticky. The method does not satisfy the restock contract. |

An empty collection after a successful switch is also a clean end. The update produced nothing to yield.

`Restock: false` never calls the method. The iterator ends when the opened collection ends.

The method runs while `Next` holds the iterator lock. It must not call `Next` on the same iterator. `Set` and `Get` are fine. A missing method, a method that requires arguments, or a panic fails the restock.

`JobID` is not passed to the method. Read it from the iterator inside `SetData` if the method needs it, or close over it. The iterator only records the value from open.

## Share the pointer

```go
func take(it *store.Iter) (store.Element, bool) {
    return it.Next(context.Background())
}
```

Several goroutines may call `Next` on the same pointer. Each element is delivered once. This is a work queue, not a view. A second full pass is a second `OpenIter`. There is no `Fork`, no `Reset`, and no bookmark.

Do not `Close` from one worker and expect the others to keep reading. `Close` finishes the iterator for every holder. After `Close`, `Next` returns false and `Err` returns `ErrClosed`.

```go
var wg sync.WaitGroup
for i := 0; i < workers; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        for {
            el, ok := it.Next(ctx)
            if !ok {
                return
            }
            handle(el.Value, el.Local, el.Overall)
        }
    }()
}
wg.Wait()
return it.Err()
```

A sticky restock error makes every `Next` return false, so the workers exit and `it.Err()` reports the cause.

## Errors

| Error | When |
|---|---|
| `ErrClosed` | `Err` after `Close`. |
| `ErrRestock` | The update method returned the store id already bound. Wrapped with the key. Use `errors.Is`. |
| `store: iterator requires a key or a store id` | Both were empty. |
| `store: key "k" is not registered` | Open on an unknown key. |
| `store: key "k" is deleted` | Open after `UnRegister`. |
| `store: store id "..." belongs to "..."` | `StoreID` is a row for a different key. |
| `store: no current value` | `Key` is registered but has no data row. |
| `method SetData not found` | `Restock` is set and the cached object has no such method. |
| `method SetData on T did not return a store id` | The method returned no string. |

A clean end is not an error.

## Limits

- One registered object. Not every store id of a key, and not every instance of a schema.
- No iterator name, bookmark, seek, or reset. Resume by opening the store id you still have.
- No descending order and no mode switch. Slices and maps yield elements; other values yield themselves.
- The window is the current object. There is no numeric page size.
- The update method is no-argument. It must return the new store id as a string.
- Names and bookmarks are not written to the database. Nothing here survives a restart except the rows `SetData` itself stored.