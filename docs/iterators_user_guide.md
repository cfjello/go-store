# Iterators

An iterator walks the elements of one stored object. Slices, arrays, and maps yield one element per entry; any other value (including a struct) is a one-element collection. Iterators are the way to consume large stored collections without loading them element by element by hand.

For the precise semantics — binding rules, restock, concurrency — see the in-depth reference: [Iterators](iterators.md).

## Open an iterator

```go
it, err := s.OpenIter(store.OpenOpts{
	Key:     "readings-1", // registered object key
	StoreID: "",           // empty = latest version of Key
	JobID:   "job-42",     // recorded on the iterator; does not filter rows
	Restock: false,
})
if err != nil {
	return err
}
defer it.Close()
```

You may open by `Key` (binds the newest store ID), by `StoreID` (the key is resolved from the row), or both (the row must belong to the key). Opening fails for unknown, unregistered, or soft-deleted keys.

`OpenIter` returns a `*store.Iter` — always pass the pointer; copying the struct is not safe.

## Consume elements

```go
ctx := context.Background()
for {
	el, ok := it.Next(ctx)
	if !ok {
		break
	}
	reading := el.Value.(Reading)
	fmt.Println(el.Local, el.Overall, el.StoreID, reading)
}
if err := it.Err(); err != nil { // nil after a clean end
	return err
}
```

Each `Element` carries:

| Field | Meaning |
|---|---|
| `Value` | The element, by value. The caller casts it. |
| `Local` | Index within the collection of the current store ID. Resets on restock. |
| `Overall` | Running count across everything this iterator has yielded. Never resets. |
| `StoreID` | The row this element came from. |

`Next` is a shared pull: concurrent callers each consume distinct elements — position and counters live in the iterator.

## Restock: follow a live object

With `Restock: true`, exhausting the current collection calls the object's update method (the method named by the store's `UpdMethod`, `SetData` by default) and rebinds the iterator to the store ID it returns:

```go
it, err := s.OpenIter(store.OpenOpts{Key: "feed-1", Restock: true})
```

- An empty string from the update method ends iteration cleanly (`Err()` stays nil).
- Returning the store ID the iterator is already reading is an error (`store.ErrRestock`).
- `Local` resets to 0 on the new row; `Overall` keeps counting.

## Inspect and stop

```go
n  := it.Count()    // elements yielded so far
k  := it.Key()      // registered key (resolved from StoreID when opened by ID)
id := it.StoreID()  // currently bound store ID
j  := it.JobID()    // job ID recorded at open
err := it.Err()     // sticky error, ErrClosed after Close, nil after a clean end
it.Close()          // finish; second call is a no-op
```

Cancelling the `ctx` passed to `Next` stops only that caller and only before a restock starts; it never fails the iterator.

## Limitations

- Walking every store ID of a key is not supported.
- Descending order is not supported.
- The stored document is loaded fresh from the database into a new value of the registered type; the cache only supplies the type and, when restocking, the update method.
