package test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

// iterObject is the store object under test. Data holds either a single value
// or a collection. SetData persists the next higher page and returns its store id.
type iterObject[T any] struct {
	Data T `json:"data"`

	store *store.Store
	key   string
	pages []T
	page  int
	calls int
	same  bool
	fail  error
}

func (o *iterObject[T]) SetData() (string, error) {
	o.calls++
	if o.fail != nil {
		return "", o.fail
	}
	if o.same {
		return o.store.GetCurrStoreID(o.key)
	}
	if o.page >= len(o.pages) {
		return "", nil
	}
	o.Data = o.pages[o.page]
	o.page++
	if _, err := o.store.Set(types.SetArgs{Key: o.key, Object: o}); err != nil {
		return "", err
	}
	return o.store.GetCurrStoreID(o.key)
}

func iterKey(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

func registerIterObject[T any](t *testing.T, s *store.Store, key string, pages []T) *iterObject[T] {
	t.Helper()
	obj := &iterObject[T]{store: s, key: key, pages: pages}
	if _, err := s.Register(types.RegisterArgs{Key: key, Object: obj}); err != nil {
		t.Fatalf("register %s: %v", key, err)
	}
	return obj
}

func initIterObject[T any](t *testing.T, s *store.Store, key string, pages []T) *iterObject[T] {
	t.Helper()
	obj := registerIterObject(t, s, key, pages)
	if _, err := obj.SetData(); err != nil {
		t.Fatalf("SetData %s: %v", key, err)
	}
	return obj
}

func drainData[T any](t *testing.T, it *store.Iter) []T {
	t.Helper()
	var out []T
	for {
		el, ok := it.Next(context.Background())
		if !ok {
			if err := it.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		if el.Overall != len(out) || el.Local != 0 {
			t.Fatalf("element %+v, want overall %d local 0", el, len(out))
		}
		obj, ok := el.Value.(iterObject[T])
		if !ok {
			t.Fatalf("value type %T, want iterObject", el.Value)
		}
		out = append(out, obj.Data)
	}
}

func TestIteratorCreateAndTeardown(t *testing.T) {
	s := newValidTestStore(t)
	key := iterKey(t)
	obj := initIterObject(t, s, key, [][]int{{1, 2}})

	it, err := s.OpenIter(store.OpenOpts{Key: key, JobID: "job-1"})
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("OpenIter returned a nil iterator")
	}
	if it.Key() != key || it.JobID() != "job-1" {
		t.Fatalf("identity key=%q job=%q", it.Key(), it.JobID())
	}
	curr, err := s.GetCurrStoreID(key)
	if err != nil {
		t.Fatal(err)
	}
	if it.StoreID() != curr {
		t.Fatalf("store id = %q, want %q", it.StoreID(), curr)
	}
	if it.Count() != 0 {
		t.Fatalf("count before read = %d", it.Count())
	}

	got := drainData[[]int](t, it)
	if !reflect.DeepEqual(got, [][]int{{1, 2}}) {
		t.Fatalf("data = %#v", got)
	}
	if it.Count() != 1 {
		t.Fatalf("count = %d, want 1", it.Count())
	}
	if obj.calls != 1 {
		t.Fatalf("SetData calls = %d, want 1", obj.calls)
	}

	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := it.Next(context.Background()); ok {
		t.Fatal("closed iterator returned a value")
	}
	if !errors.Is(it.Err(), store.ErrClosed) {
		t.Fatalf("err = %v", it.Err())
	}
	if err := it.Close(); err != nil {
		t.Fatal("second Close should be a no-op")
	}
	if it.Count() != 1 {
		t.Fatalf("count after close = %d", it.Count())
	}
	if !s.IsRegistered(key) {
		t.Fatal("closing an iterator unregistered the object")
	}

	again, err := s.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	if again == it {
		t.Fatal("teardown reused the closed iterator")
	}
	againGot := drainData[[]int](t, again)
	if !reflect.DeepEqual(againGot, [][]int{{1, 2}}) {
		t.Fatalf("reopened data = %#v", againGot)
	}
}

func TestIteratorCollectionsAndScalars(t *testing.T) {
	s := newValidTestStore(t)

	t.Run("int slice", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, [][]int{{1, 2}, {8, 9}})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[[]int](t, it)
		if !reflect.DeepEqual(got, [][]int{{1, 2}}) {
			t.Fatalf("without restock got %#v", got)
		}
	})

	t.Run("string slice", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, [][]string{{"a", "b"}})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[[]string](t, it)
		if !reflect.DeepEqual(got, [][]string{{"a", "b"}}) {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("map", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, []map[string]int{{"a": 1, "b": 2}})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[map[string]int](t, it)
		if !reflect.DeepEqual(got, []map[string]int{{"a": 1, "b": 2}}) {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("single int", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, []int{4})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[int](t, it)
		if !reflect.DeepEqual(got, []int{4}) {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("single string", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, []string{"solo"})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[string](t, it)
		if !reflect.DeepEqual(got, []string{"solo"}) {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("single struct", func(t *testing.T) {
		type reading struct {
			Name string `json:"name"`
			N    int    `json:"n"`
		}
		key := iterKey(t)
		initIterObject(t, s, key, []reading{{Name: "low", N: 1}})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		got := drainData[reading](t, it)
		if !reflect.DeepEqual(got, []reading{{Name: "low", N: 1}}) {
			t.Fatalf("got %#v", got)
		}
	})
}

func TestIteratorRestockHigherValues(t *testing.T) {
	s := newValidTestStore(t)
	key := iterKey(t)
	pages := [][]int{{1, 2}, {3, 4}, {10}}
	obj := initIterObject(t, s, key, pages)

	it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true, JobID: "restock"})
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()

	got := drainData[[]int](t, it)
	if !reflect.DeepEqual(got, pages) {
		t.Fatalf("pages = %#v, want %#v", got, pages)
	}
	if it.Count() != len(pages) {
		t.Fatalf("count = %d, want %d", it.Count(), len(pages))
	}
	// init plus one SetData per later page, then a final empty return.
	if obj.calls != len(pages)+1 {
		t.Fatalf("SetData calls = %d, want %d", obj.calls, len(pages)+1)
	}
	prevMax := -1
	for i, page := range got {
		for _, n := range page {
			if n <= prevMax {
				t.Fatalf("page %d value %d is not higher than %d", i, n, prevMax)
			}
			prevMax = n
		}
	}

	scalarKey := key + "_scalar"
	scalarPages := []int{1, 5, 9}
	initIterObject(t, s, scalarKey, scalarPages)
	sit, err := s.OpenIter(store.OpenOpts{Key: scalarKey, Restock: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sit.Close()
	scalars := drainData[int](t, sit)
	if !reflect.DeepEqual(scalars, scalarPages) {
		t.Fatalf("scalars = %#v", scalars)
	}
	for i := 1; i < len(scalars); i++ {
		if scalars[i] <= scalars[i-1] {
			t.Fatalf("scalar page %d (%d) is not higher than %d", i, scalars[i], scalars[i-1])
		}
	}
}

func TestIteratorEdges(t *testing.T) {
	s := newValidTestStore(t)

	t.Run("missing key and store id", func(t *testing.T) {
		if _, err := s.OpenIter(store.OpenOpts{}); err == nil {
			t.Fatal("expected missing key and store id to fail")
		}
	})

	t.Run("not registered", func(t *testing.T) {
		if _, err := s.OpenIter(store.OpenOpts{Key: iterKey(t)}); err == nil {
			t.Fatal("expected an unregistered key to fail")
		}
	})

	t.Run("registered but not initialized", func(t *testing.T) {
		key := iterKey(t)
		registerIterObject(t, s, key, []int{1})
		if _, err := s.OpenIter(store.OpenOpts{Key: key}); err == nil {
			t.Fatal("expected a key with no SetData to fail")
		}
	})

	t.Run("deleted", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, []string{"x"})
		if !s.UnRegister(key) {
			t.Fatal("unregister failed")
		}
		if _, err := s.OpenIter(store.OpenOpts{Key: key}); err == nil {
			t.Fatal("expected a deleted key to fail")
		}
	})

	t.Run("foreign store id", func(t *testing.T) {
		key := iterKey(t)
		initIterObject(t, s, key, []int{1})
		other := key + "_other"
		initIterObject(t, s, other, []int{2})
		otherID, err := s.GetCurrStoreID(other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.OpenIter(store.OpenOpts{Key: key, StoreID: otherID}); err == nil {
			t.Fatal("expected a foreign store id to be rejected")
		}
	})

	t.Run("store id only and pinned older row", func(t *testing.T) {
		key := iterKey(t)
		obj := registerIterObject(t, s, key, [][]int{{1}, {9}})
		firstID, err := obj.SetData()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := obj.SetData(); err != nil {
			t.Fatal(err)
		}
		latest, err := s.OpenIter(store.OpenOpts{Key: key, StoreID: "0000"})
		if err != nil {
			t.Fatal(err)
		}
		defer latest.Close()
		if got := drainData[[]int](t, latest); !reflect.DeepEqual(got, [][]int{{9}}) {
			t.Fatalf("latest = %#v", got)
		}

		pinned, err := s.OpenIter(store.OpenOpts{StoreID: firstID})
		if err != nil {
			t.Fatal(err)
		}
		defer pinned.Close()
		if pinned.Key() != key {
			t.Fatalf("resolved key = %q", pinned.Key())
		}
		if got := drainData[[]int](t, pinned); !reflect.DeepEqual(got, [][]int{{1}}) {
			t.Fatalf("pinned = %#v", got)
		}
	})

	t.Run("restock false ignores a later SetData", func(t *testing.T) {
		key := iterKey(t)
		obj := initIterObject(t, s, key, []int{1, 2, 3})
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		el, ok := it.Next(context.Background())
		if !ok || el.Value.(iterObject[int]).Data != 1 {
			t.Fatalf("first = %#v", el.Value)
		}
		if _, err := obj.SetData(); err != nil {
			t.Fatal(err)
		}
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("iterator followed a SetData without Restock")
		}
		if obj.calls != 2 {
			t.Fatalf("calls = %d, want the manual SetData only", obj.calls)
		}
	})

	t.Run("same store id", func(t *testing.T) {
		key := iterKey(t)
		obj := initIterObject(t, s, key, []int{1, 2})
		obj.same = true
		it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		if _, ok := it.Next(context.Background()); !ok {
			t.Fatal(it.Err())
		}
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("expected restock to stop")
		}
		if !errors.Is(it.Err(), store.ErrRestock) {
			t.Fatalf("err = %v", it.Err())
		}
	})

	t.Run("restock error sticks", func(t *testing.T) {
		key := iterKey(t)
		obj := initIterObject(t, s, key, []string{"a", "b"})
		obj.fail = errors.New("restock failed")
		it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		if _, ok := it.Next(context.Background()); !ok {
			t.Fatal(it.Err())
		}
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("expected the restock error to stop the iterator")
		}
		if !errors.Is(it.Err(), obj.fail) {
			t.Fatalf("err = %v", it.Err())
		}
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("sticky error was cleared")
		}
	})

	t.Run("cancel before restock", func(t *testing.T) {
		key := iterKey(t)
		obj := initIterObject(t, s, key, []int{1, 8})
		it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		if _, ok := it.Next(context.Background()); !ok {
			t.Fatal(it.Err())
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, ok := it.Next(ctx); ok {
			t.Fatal("cancelled next returned a value")
		}
		if err := it.Err(); err != nil {
			t.Fatalf("cancel stuck: %v", err)
		}
		if obj.calls != 1 {
			t.Fatalf("cancelled restock called SetData, calls = %d", obj.calls)
		}
		el, ok := it.Next(context.Background())
		if !ok || el.Overall != 1 || el.Local != 0 || el.Value.(iterObject[int]).Data != 8 {
			t.Fatalf("retry = %+v ok %v err %v", el, ok, it.Err())
		}
	})
}

func TestIteratorSharedPull(t *testing.T) {
	s := newValidTestStore(t)
	key := iterKey(t)
	initIterObject(t, s, key, []string{"a", "b", "c", "d"})
	it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true})
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()

	var mu sync.Mutex
	got := map[string]int{}
	indexes := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				el, ok := it.Next(context.Background())
				if !ok {
					return
				}
				mu.Lock()
				got[el.Value.(iterObject[string]).Data]++
				indexes[el.Overall]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || len(indexes) != 4 {
		t.Fatalf("values %#v indexes %#v", got, indexes)
	}
	for _, n := range got {
		if n != 1 {
			t.Fatalf("a value was delivered %d times", n)
		}
	}
	for i := 0; i < 4; i++ {
		if indexes[i] != 1 {
			t.Fatalf("index %d delivered %d times", i, indexes[i])
		}
	}
}

// numberBatch is a collection stored as one row. SetData restocks it with the
// next page and returns that page's store id.
type numberBatch []int

type numberBatchCtl struct {
	store *store.Store
	key   string
	pages [][]int
	page  int
	calls int
	fail  error
}

var (
	numberBatchMu   sync.Mutex
	numberBatchCtls = map[*numberBatch]*numberBatchCtl{}
)

func (b *numberBatch) SetData() (string, error) {
	numberBatchMu.Lock()
	ctl := numberBatchCtls[b]
	numberBatchMu.Unlock()
	if ctl == nil {
		return "", errors.New("missing batch controller")
	}
	ctl.calls++
	if ctl.fail != nil {
		return "", ctl.fail
	}
	if ctl.page >= len(ctl.pages) {
		return "", nil
	}
	page := numberBatch(ctl.pages[ctl.page])
	ctl.page++
	if _, err := ctl.store.Set(types.SetArgs{Key: ctl.key, Object: page}); err != nil {
		return "", err
	}
	return ctl.store.GetCurrStoreID(ctl.key)
}

type batchPull struct {
	el store.Element
	ok bool
}

// sumBatch is one caller. It adds the values it is given.
func sumBatch(it *store.Iter, goPull <-chan struct{}, result chan<- batchPull) {
	for range goPull {
		el, ok := it.Next(context.Background())
		result <- batchPull{el, ok}
		if !ok {
			return
		}
	}
}

// auditBatch is a second caller. It only records the position it was given.
func auditBatch(it *store.Iter, goPull <-chan struct{}, result chan<- batchPull) {
	for range goPull {
		el, ok := it.Next(context.Background())
		if ok && el.StoreID == "" {
			result <- batchPull{ok: false}
			return
		}
		result <- batchPull{el, ok}
		if !ok {
			return
		}
	}
}

func TestIteratorSharedRestockCounters(t *testing.T) {
	s := newValidTestStore(t)
	key := iterKey(t)
	pages := [][]int{{1, 2}, {10, 11, 12}}
	start := numberBatch(pages[0])
	ctl := &numberBatchCtl{store: s, key: key, pages: pages}
	numberBatchMu.Lock()
	numberBatchCtls[&start] = ctl
	numberBatchMu.Unlock()
	t.Cleanup(func() {
		numberBatchMu.Lock()
		delete(numberBatchCtls, &start)
		numberBatchMu.Unlock()
	})
	if _, err := s.Register(types.RegisterArgs{Key: key, Object: &start}); err != nil {
		t.Fatal(err)
	}
	firstID, err := start.SetData()
	if err != nil {
		t.Fatal(err)
	}

	it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true, JobID: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	if it.StoreID() != firstID {
		t.Fatalf("open store id = %q, want %q", it.StoreID(), firstID)
	}

	sumGo := make(chan struct{})
	auditGo := make(chan struct{})
	sumOut := make(chan batchPull)
	auditOut := make(chan batchPull)
	go sumBatch(it, sumGo, sumOut)
	go auditBatch(it, auditGo, auditOut)

	var got []store.Element
	pull := func(ch chan struct{}, out <-chan batchPull, who string) store.Element {
		t.Helper()
		ch <- struct{}{}
		res := <-out
		if !res.ok {
			t.Fatalf("%s ended early: %v", who, it.Err())
		}
		got = append(got, res.el)
		return res.el
	}

	// Alternate two functions across the restock. sum takes 1 and 10 and 12.
	// audit takes 2 and 11, including the first element of the new store id.
	pull(sumGo, sumOut, "sum")
	pull(auditGo, auditOut, "audit")
	pull(auditGo, auditOut, "audit")
	pull(sumGo, sumOut, "sum")
	pull(sumGo, sumOut, "sum")

	sumGo <- struct{}{}
	if res := <-sumOut; res.ok {
		t.Fatal("expected the shared iterator to be exhausted")
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	close(sumGo)
	close(auditGo)

	if len(got) != 5 {
		t.Fatalf("got %d elements", len(got))
	}
	secondID := got[2].StoreID
	if secondID == "" || secondID == firstID {
		t.Fatalf("restock store id = %q, first = %q", secondID, firstID)
	}
	if it.StoreID() != secondID {
		t.Fatalf("iterator store id = %q, want %q", it.StoreID(), secondID)
	}
	if it.Count() != 5 {
		t.Fatalf("overall count = %d, want 5", it.Count())
	}

	wantLocal := []int{0, 1, 0, 1, 2}
	wantValue := []int{1, 2, 10, 11, 12}
	for i, el := range got {
		if el.Overall != i {
			t.Fatalf("element %d overall = %d", i, el.Overall)
		}
		if el.Local != wantLocal[i] {
			t.Fatalf("element %d local = %d, want %d", i, el.Local, wantLocal[i])
		}
		if el.Value.(int) != wantValue[i] {
			t.Fatalf("element %d value = %v, want %d", i, el.Value, wantValue[i])
		}
		wantID := firstID
		if i >= 2 {
			wantID = secondID
		}
		if el.StoreID != wantID {
			t.Fatalf("element %d store id = %q, want %q", i, el.StoreID, wantID)
		}
	}
}

func openNumberBatch(t *testing.T, s *store.Store, key string, pages [][]int, restock bool) (*store.Iter, *numberBatchCtl) {
	t.Helper()
	start := numberBatch{}
	ctl := &numberBatchCtl{store: s, key: key, pages: pages}
	numberBatchMu.Lock()
	numberBatchCtls[&start] = ctl
	numberBatchMu.Unlock()
	t.Cleanup(func() {
		numberBatchMu.Lock()
		delete(numberBatchCtls, &start)
		numberBatchMu.Unlock()
	})
	if _, err := s.Register(types.RegisterArgs{Key: key, Object: &start, Init: true}); err != nil {
		t.Fatal(err)
	}
	it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: restock})
	if err != nil {
		t.Fatal(err)
	}
	return it, ctl
}

func TestIteratorBoundaries(t *testing.T) {
	s := newValidTestStore(t)

	t.Run("empty collection", func(t *testing.T) {
		key := iterKey(t)
		if _, err := s.Register(types.RegisterArgs{Key: key, Object: []int{}, Init: true}); err != nil {
			t.Fatal(err)
		}
		it, err := s.OpenIter(store.OpenOpts{Key: key})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("empty collection yielded an element")
		}
		if err := it.Err(); err != nil {
			t.Fatal(err)
		}
		if it.Count() != 0 {
			t.Fatalf("count = %d", it.Count())
		}
	})

	t.Run("empty collection restock fills", func(t *testing.T) {
		key := iterKey(t)
		it, ctl := openNumberBatch(t, s, key, [][]int{{7, 8}}, true)
		defer it.Close()
		opened := it.StoreID()
		el, ok := it.Next(context.Background())
		if !ok {
			t.Fatal(it.Err())
		}
		if el.Value.(int) != 7 || el.Local != 0 || el.Overall != 0 {
			t.Fatalf("first = %+v", el)
		}
		if el.StoreID == opened {
			t.Fatal("restock did not switch store id")
		}
		el, ok = it.Next(context.Background())
		if !ok || el.Value.(int) != 8 || el.Local != 1 || el.Overall != 1 || el.StoreID != it.StoreID() {
			t.Fatalf("second = %+v bound %s", el, it.StoreID())
		}
		if _, ok = it.Next(context.Background()); ok {
			t.Fatal("expected clean end after the restocked page")
		}
		if err := it.Err(); err != nil {
			t.Fatal(err)
		}
		if ctl.calls != 2 {
			t.Fatalf("SetData calls = %d, want fill then empty", ctl.calls)
		}
	})

	t.Run("empty restock", func(t *testing.T) {
		key := iterKey(t)
		it, ctl := openNumberBatch(t, s, key, nil, true)
		defer it.Close()
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("empty restock yielded an element")
		}
		if err := it.Err(); err != nil {
			t.Fatalf("empty restock stuck: %v", err)
		}
		if ctl.calls != 1 || it.Count() != 0 {
			t.Fatalf("calls = %d count = %d", ctl.calls, it.Count())
		}
	})

	t.Run("restock empty collection", func(t *testing.T) {
		key := iterKey(t)
		it, ctl := openNumberBatch(t, s, key, [][]int{{}}, true)
		defer it.Close()
		opened := it.StoreID()
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("empty restocked collection yielded an element")
		}
		if err := it.Err(); err != nil {
			t.Fatal(err)
		}
		if it.StoreID() == opened {
			t.Fatal("empty restock did not bind the new store id")
		}
		if ctl.calls != 1 || it.Count() != 0 {
			t.Fatalf("calls = %d count = %d", ctl.calls, it.Count())
		}
	})

	t.Run("restock failed", func(t *testing.T) {
		key := iterKey(t)
		it, ctl := openNumberBatch(t, s, key, [][]int{{1}}, true)
		ctl.fail = errors.New("restock failed")
		defer it.Close()
		if _, ok := it.Next(context.Background()); ok {
			t.Fatal("failed restock yielded an element")
		}
		if !errors.Is(it.Err(), ctl.fail) {
			t.Fatalf("err = %v", it.Err())
		}
		if _, ok := it.Next(context.Background()); ok || !errors.Is(it.Err(), ctl.fail) {
			t.Fatal("restock error was not sticky")
		}
		if it.Count() != 0 {
			t.Fatalf("count = %d", it.Count())
		}
	})

	t.Run("single value restock", func(t *testing.T) {
		key := iterKey(t)
		obj := initIterObject(t, s, key, []int{4, 9})
		it, err := s.OpenIter(store.OpenOpts{Key: key, Restock: true})
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		first, ok := it.Next(context.Background())
		if !ok || first.Local != 0 || first.Overall != 0 || first.Value.(iterObject[int]).Data != 4 {
			t.Fatalf("first = %+v", first)
		}
		second, ok := it.Next(context.Background())
		if !ok || second.Local != 0 || second.Overall != 1 || second.Value.(iterObject[int]).Data != 9 {
			t.Fatalf("second = %+v", second)
		}
		if second.StoreID == first.StoreID {
			t.Fatal("single value restock kept the old store id")
		}
		if _, ok = it.Next(context.Background()); ok || it.Err() != nil {
			t.Fatalf("end ok %v err %v", ok, it.Err())
		}
		if obj.calls != 3 || it.Count() != 2 {
			t.Fatalf("calls = %d count = %d", obj.calls, it.Count())
		}
	})
}
