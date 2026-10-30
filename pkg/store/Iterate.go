package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// ErrClosed is returned from Err after Close. Next then reports no element.
var ErrClosed = errors.New("iterator closed")

// ErrRestock is returned when the update method returns the store id the
// iterator is already reading. The iterator stops instead of calling it again.
var ErrRestock = errors.New("update method returned the current store id")

var errRestockDone = errors.New("restock finished")

// Element is one consumed value. Local is its index in the collection of the
// store id that produced it, and resets to 0 when restock binds a new store id.
// Overall counts every element this iterator has yielded and does not reset.
// StoreID is the row the element came from, so a later restock does not change it.
type Element struct {
	Value   any
	Local   int
	Overall int
	StoreID string
}

// OpenOpts opens an iterator over one registered object.
//
// Provide Key, StoreID, or both. An empty StoreID binds the latest row for Key
// (the highest store id). An empty Key is resolved from StoreID. Restock, when
// set, calls the cached object's update method after the current collection is
// exhausted.
type OpenOpts struct {
	Key     string
	StoreID string
	JobID   string
	Restock bool
}

// Iter walks one stored object. A slice, array, or map is a collection of
// elements. Any other value, including a struct, is a one-element collection.
//
// Next is a shared pull. The position and the overall counter live in the
// iterator. Pass the pointer; copying the struct is not safe.
type Iter struct {
	store   *Store
	key     string
	jobID   string
	storeID string
	restock bool

	mu      sync.Mutex
	elems   []any
	pos     int
	yielded int
	closed  bool
	err     error
}

// OpenIter walks the registered object selected by opts. The returned pointer
// is the iterator; it is not stored under a name.
func (s *Store) OpenIter(opts OpenOpts) (*Iter, error) {
	if s == nil {
		return nil, errors.New("store: nil store")
	}
	key, storeID, err := s.resolveIterTarget(opts.Key, opts.StoreID)
	if err != nil {
		return nil, err
	}
	elems, err := s.loadElems(key, storeID)
	if err != nil {
		return nil, err
	}
	return &Iter{
		store:   s,
		key:     key,
		jobID:   opts.JobID,
		storeID: storeID,
		restock: opts.Restock,
		elems:   elems,
	}, nil
}

// Next returns the next element. The caller casts Element.Value. Local is the
// index inside the current store id. Overall starts at 0 and keeps counting
// after restock. Clean exhaustion returns false and leaves Err nil.
func (it *Iter) Next(ctx context.Context) (Element, bool) {
	if it == nil {
		return Element{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	for {
		if it.closed || it.err != nil {
			return Element{}, false
		}
		if it.pos < len(it.elems) {
			el := Element{
				Value:   it.elems[it.pos],
				Local:   it.pos,
				Overall: it.yielded,
				StoreID: it.storeID,
			}
			it.pos++
			it.yielded++
			return el, true
		}
		if !it.restock {
			return Element{}, false
		}
		if err := ctx.Err(); err != nil {
			return Element{}, false
		}
		if err := it.restockLocked(); err != nil {
			if errors.Is(err, errRestockDone) {
				it.restock = false
				return Element{}, false
			}
			it.err = err
			return Element{}, false
		}
	}
}

// Count returns how many elements Next has yielded. It does not reset on restock.
func (it *Iter) Count() int {
	if it == nil {
		return 0
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.yielded
}

// Key returns the registered object key. It is resolved from StoreID when Open
// was given only a store id.
func (it *Iter) Key() string {
	if it == nil {
		return ""
	}
	return it.key
}

// StoreID returns the store id currently bound. After a restock this is the id
// returned by the update method, which may be ahead of an element already taken
// by another caller.
func (it *Iter) StoreID() string {
	if it == nil {
		return ""
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.storeID
}

// JobID returns the job id recorded at open. It is not used to select rows.
func (it *Iter) JobID() string {
	if it == nil {
		return ""
	}
	return it.jobID
}

// Err returns a sticky restock or load error, or ErrClosed after Close. A clean
// end leaves Err nil. A caller stopped by its own context also leaves Err nil.
func (it *Iter) Err() error {
	if it == nil {
		return errors.New("store: nil iterator")
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.closed {
		return ErrClosed
	}
	return it.err
}

// Close marks the iterator finished. A Next blocked on the mutex observes
// ErrClosed once it runs. A second call is a no-op.
func (it *Iter) Close() error {
	if it == nil {
		return errors.New("store: nil iterator")
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.closed {
		return nil
	}
	it.closed = true
	it.err = ErrClosed
	return nil
}

func (s *Store) resolveIterTarget(key, storeID string) (string, string, error) {
	if storeID == s.InitStoreId {
		storeID = ""
	}
	if key == "" && storeID == "" {
		return "", "", errors.New("store: iterator requires a key or a store id")
	}
	if storeID != "" {
		metaKey, _, _, err := s.DB.GetDataRow(context.Background(), storeID)
		if err != nil {
			return "", "", fmt.Errorf("store: store id %q: %w", storeID, err)
		}
		if key == "" {
			key = metaKey
		} else if key != metaKey {
			return "", "", fmt.Errorf("store: store id %q belongs to %q, not %q", storeID, metaKey, key)
		}
	}
	if err := s.requireLive(key); err != nil {
		return "", "", err
	}
	if storeID == "" {
		id, err := s.GetCurrStoreID(key)
		if err != nil || id == "" || id == s.InitStoreId {
			return "", "", fmt.Errorf("store: no current value for %q", key)
		}
		storeID = id
	}
	return key, storeID, nil
}

func (s *Store) requireLive(key string) error {
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("store: key %q is not registered: %w", key, err)
	}
	if meta.SoftDel != "" && meta.SoftDel != s.SoftDel {
		return fmt.Errorf("store: key %q is deleted", key)
	}
	return nil
}

func (s *Store) loadElems(key, storeID string) ([]any, error) {
	cached, ok := s.GetCache(key)
	if !ok || cached == nil {
		return nil, fmt.Errorf("store: no cached object for %q", key)
	}
	dest := allocLike(cached)
	if _, err := s.Get(storeID, key, dest); err != nil {
		return nil, fmt.Errorf("store: value %q store id %q: %w", key, storeID, err)
	}
	return elementsOf(unwrap(dest)), nil
}

// restockLocked replaces the bound object with the store id returned by the
// cached object's update method. yielded is left unchanged. The method must not
// call Next on this iterator: Next holds the iterator lock across the call.
func (it *Iter) restockLocked() error {
	if it.store == nil {
		return errors.New("store: nil store")
	}
	cached, ok := it.store.GetCache(it.key)
	if !ok || cached == nil {
		return fmt.Errorf("store: no cached object for %q", it.key)
	}
	newID, err := callUpdate(cached, it.store.UpdMethod())
	if err != nil {
		return err
	}
	if newID == "" {
		return errRestockDone
	}
	if newID == it.storeID {
		return fmt.Errorf("store: iterator %q: %w", it.key, ErrRestock)
	}
	elems, err := it.store.loadElems(it.key, newID)
	if err != nil {
		return err
	}
	it.storeID = newID
	it.elems = elems
	it.pos = 0
	if len(elems) == 0 {
		return errRestockDone
	}
	return nil
}

// callUpdate invokes the exported no-argument method name on obj. A string
// result is the new store id. An empty string means there is nothing further
// to read. A non-nil error result fails the call. A method that returns neither
// a string nor an error does not satisfy the restock contract.
func callUpdate(obj any, name string) (storeID string, err error) {
	if obj == nil {
		return "", errors.New("store: no object to update")
	}
	if name == "" {
		return "", errors.New("store: update method name is empty")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s panicked: %v", name, r)
		}
	}()

	method := reflect.ValueOf(obj).MethodByName(name)
	if !method.IsValid() {
		return "", fmt.Errorf("method %s not found on %T", name, obj)
	}
	if method.Type().NumIn() != 0 {
		return "", fmt.Errorf("method %s on %T requires %d arguments", name, obj, method.Type().NumIn())
	}
	var found bool
	for _, result := range method.Call(nil) {
		if !result.IsValid() || !result.CanInterface() {
			continue
		}
		if result.Kind() == reflect.Interface && result.IsNil() {
			continue
		}
		switch v := result.Interface().(type) {
		case error:
			if v != nil {
				return "", v
			}
		case string:
			if !found {
				storeID = v
				found = true
			}
		}
	}
	if !found {
		return "", fmt.Errorf("method %s on %T did not return a store id", name, obj)
	}
	return storeID, nil
}

func allocLike(sample any) any {
	t := reflect.TypeOf(sample)
	if t == nil {
		return new(any)
	}
	if t.Kind() == reflect.Pointer {
		return reflect.New(t.Elem()).Interface()
	}
	return reflect.New(t).Interface()
}

func unwrap(loaded any) any {
	rv := reflect.ValueOf(loaded)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || !rv.CanInterface() {
		return nil
	}
	return rv.Interface()
}

// elementsOf treats a slice, array, or map as a collection and every other
// value as a one-element collection. Map values are yielded in sorted key order
// so a walk is repeatable. A nil value is one nil element.
func elementsOf(v any) []any {
	if v == nil {
		return []any{nil}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
		})
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = rv.MapIndex(k).Interface()
		}
		return out
	default:
		return []any{v}
	}
}
