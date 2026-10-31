package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

var testStore *store.Store

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-store-iter-*")
	if err != nil {
		panic(err)
	}
	// In-memory main database with the temp file attached as persist layer.
	os.Setenv("SQLITE_DB_URL", ":memory:")
	os.Setenv("SQLITE_ATTACH_URL", "file:"+filepath.Join(dir, "test.db"))
	os.Setenv("FILE_DB_ALIAS", "persist")
	testStore = store.New(nil, true)
	code := m.Run()
	if testStore != nil && testStore.DB != nil {
		_ = testStore.DB.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func keyOf(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

func register(t *testing.T, key string, obj any, init bool) {
	t.Helper()
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Init: init}); err != nil {
		t.Fatalf("register %s: %v", key, err)
	}
}

func set(t *testing.T, key string, obj any) {
	t.Helper()
	if _, err := testStore.Set(types.SetArgs{Key: key, Object: obj}); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

type point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func TestOpenIterRequiresKeyOrStoreID(t *testing.T) {
	if _, err := testStore.OpenIter(store.OpenOpts{}); err == nil {
		t.Fatal("expected missing key and store id to fail")
	}
	if _, err := testStore.OpenIter(store.OpenOpts{Key: "missing"}); err == nil {
		t.Fatal("expected an unregistered key to fail")
	}
}

func TestSingleValueAndCollection(t *testing.T) {
	key := keyOf(t)
	register(t, key, "solo", true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key, JobID: "job-1"})
	if err != nil {
		t.Fatal(err)
	}
	if it.JobID() != "job-1" || it.Key() != key || it.StoreID() == "" {
		t.Fatalf("identity key=%q id=%q job=%q", it.Key(), it.StoreID(), it.JobID())
	}
	el, ok := it.Next(context.Background())
	if !ok || el.Overall != 0 || el.Local != 0 || el.Value.(string) != "solo" {
		t.Fatalf("scalar = %+v ok %v", el, ok)
	}
	if _, ok = it.Next(context.Background()); ok {
		t.Fatal("scalar yielded a second element")
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if it.Count() != 1 {
		t.Fatalf("count = %d", it.Count())
	}

	skey := key + "-slice"
	register(t, skey, []string{"a", "b"}, true)
	sit, err := testStore.OpenIter(store.OpenOpts{Key: skey})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for {
		el, ok := sit.Next(context.Background())
		if !ok {
			break
		}
		got = append(got, el.Value.(string))
	}
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("slice = %v", got)
	}

	mkey := key + "-map"
	register(t, mkey, map[string]string{"b": "2", "a": "1"}, true)
	mit, err := testStore.OpenIter(store.OpenOpts{Key: mkey})
	if err != nil {
		t.Fatal(err)
	}
	first, ok := mit.Next(context.Background())
	if !ok || first.Value.(string) != "1" {
		t.Fatalf("first map value = %v", first.Value)
	}

	pkey := key + "-point"
	register(t, pkey, point{X: 3, Y: 4}, true)
	pit, err := testStore.OpenIter(store.OpenOpts{Key: pkey})
	if err != nil {
		t.Fatal(err)
	}
	pv, ok := pit.Next(context.Background())
	if !ok || pv.Overall != 0 || pv.Local != 0 {
		t.Fatal("expected one struct element")
	}
	if p, ok := pv.Value.(point); !ok || p.X != 3 || p.Y != 4 {
		t.Fatalf("point = %#v", pv.Value)
	}
	if _, ok = pit.Next(context.Background()); ok {
		t.Fatal("struct yielded a second element")
	}
}

func TestLatestAndExplicitStoreID(t *testing.T) {
	key := keyOf(t)
	register(t, key, []string{"old"}, true)
	firstID, err := testStore.GetCurrStoreID(key)
	if err != nil {
		t.Fatal(err)
	}
	set(t, key, []string{"new"})

	latest, err := testStore.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := latest.Next(context.Background())
	if !ok || v.Value.(string) != "new" {
		t.Fatalf("latest = %v", v.Value)
	}
	if latest.StoreID() == firstID {
		t.Fatal("empty store id did not bind the latest row")
	}

	pinned, err := testStore.OpenIter(store.OpenOpts{StoreID: firstID})
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Key() != key {
		t.Fatalf("resolved key = %q", pinned.Key())
	}
	v, ok = pinned.Next(context.Background())
	if !ok || v.Value.(string) != "old" {
		t.Fatalf("pinned = %v", v.Value)
	}

	other := key + "-other"
	register(t, other, "two", true)
	otherID, err := testStore.GetCurrStoreID(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.OpenIter(store.OpenOpts{Key: key, StoreID: otherID}); err == nil {
		t.Fatal("expected a foreign store id to be rejected")
	}
}

func TestDeletedKey(t *testing.T) {
	key := keyOf(t)
	register(t, key, "one", true)
	if !testStore.UnRegister(key) {
		t.Fatal("unregister failed")
	}
	if _, err := testStore.OpenIter(store.OpenOpts{Key: key}); err == nil {
		t.Fatal("expected a deleted key to be rejected")
	}
}

type words struct {
	Data []string `json:"data"`
}

var (
	wordStore *store.Store
	wordKey   string
	wordPages [][]string
	wordPage  int
	wordSame  bool
)

func (w *words) SetData() (string, error) {
	if wordSame {
		return wordStore.GetCurrStoreID(wordKey)
	}
	if wordPage >= len(wordPages) {
		return "", nil
	}
	page := words{Data: wordPages[wordPage]}
	wordPage++
	if _, err := wordStore.Set(types.SetArgs{Key: wordKey, Object: page}); err != nil {
		return "", err
	}
	return wordStore.GetCurrStoreID(wordKey)
}

func TestRestockSwitchesObjectAndKeepsCounter(t *testing.T) {
	key := keyOf(t)
	wordStore = testStore
	wordKey = key
	wordPages = [][]string{{"c", "d"}}
	wordPage = 0
	wordSame = false
	t.Cleanup(func() { wordPages = nil })

	start := words{Data: []string{"a", "b"}}
	register(t, key, &start, true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key, Restock: true, JobID: "j"})
	if err != nil {
		t.Fatal(err)
	}
	firstID := it.StoreID()

	var got []string
	var idxs []int
	for {
		el, ok := it.Next(context.Background())
		if !ok {
			break
		}
		got = append(got, el.Value.(string))
		idxs = append(idxs, el.Overall)
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a,b,c,d" {
		t.Fatalf("values = %v", got)
	}
	if ints(idxs) != "0,1,2,3" {
		t.Fatalf("indexes = %v", idxs)
	}
	if it.Count() != 4 {
		t.Fatalf("count = %d", it.Count())
	}
	if it.StoreID() == firstID {
		t.Fatal("store id did not change after restock")
	}
}

func TestRestockFalseStopsAtCollectionEnd(t *testing.T) {
	key := keyOf(t)
	wordStore = testStore
	wordKey = key
	wordPages = [][]string{{"c"}}
	wordPage = 0
	wordSame = false

	start := words{Data: []string{"a"}}
	register(t, key, &start, true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := it.Next(context.Background()); !ok {
		t.Fatal("missing first element")
	}
	if _, ok := it.Next(context.Background()); ok {
		t.Fatal("restock ran without Restock")
	}
	if wordPage != 0 {
		t.Fatalf("SetData calls advanced page to %d", wordPage)
	}
}

func TestRestockSameStoreIDStops(t *testing.T) {
	key := keyOf(t)
	wordStore = testStore
	wordKey = key
	wordSame = true
	wordPage = 0
	t.Cleanup(func() { wordSame = false })

	start := words{Data: []string{"a"}}
	register(t, key, &start, true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key, Restock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := it.Next(context.Background()); !ok {
		t.Fatal("missing first element")
	}
	if _, ok := it.Next(context.Background()); ok {
		t.Fatal("expected restock to stop")
	}
	if !errors.Is(it.Err(), store.ErrRestock) {
		t.Fatalf("err = %v", it.Err())
	}
}

func TestCancelledRestockDoesNotStick(t *testing.T) {
	key := keyOf(t)
	wordStore = testStore
	wordKey = key
	wordPages = [][]string{{"z"}}
	wordPage = 0
	wordSame = false

	start := words{Data: []string{}}
	register(t, key, &start, true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key, Restock: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := it.Next(ctx); ok {
		t.Fatal("cancelled next returned an element")
	}
	if err := it.Err(); err != nil {
		t.Fatalf("cancel stuck: %v", err)
	}
	el, ok := it.Next(context.Background())
	if !ok || el.Overall != 0 || el.Local != 0 || el.Value.(string) != "z" {
		t.Fatalf("retry = %+v ok %v err %v", el, ok, it.Err())
	}
}

func TestSharedPull(t *testing.T) {
	key := keyOf(t)
	register(t, key, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	got := map[string]int{}
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
				got[el.Value.(string)]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("unique = %d", len(got))
	}
	for v, n := range got {
		if n != 1 {
			t.Fatalf("%s delivered %d times", v, n)
		}
	}
}

func TestClose(t *testing.T) {
	key := keyOf(t)
	register(t, key, "scalar", true)
	it, err := testStore.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := it.Next(context.Background()); ok {
		t.Fatal("closed iterator returned an element")
	}
	if !errors.Is(it.Err(), store.ErrClosed) {
		t.Fatalf("err = %v", it.Err())
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
}

func ints(vs []int) string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strconv.Itoa(v)
	}
	return strings.Join(out, ",")
}
