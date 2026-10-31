package store_test

import (
	"sync"
	"testing"

	"github.com/cfjello/go-store/pkg/types"
)

// fakeNotifier captures notification keys for assertions.
type fakeNotifier struct {
	mu  sync.Mutex
	got []string
}

func (f *fakeNotifier) Notify(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, key)
	return nil
}

func (f *fakeNotifier) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.got))
	copy(out, f.got)
	return out
}

func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, k := range a {
		seen[k]++
	}
	for _, k := range b {
		seen[k]--
		if seen[k] < 0 {
			return false
		}
	}
	return true
}

func TestNotifyListCRUD(t *testing.T) {
	src := keyOf(t) + "_src"
	dstA := keyOf(t) + "_a"
	dstB := keyOf(t) + "_b"
	register(t, src, &struct{}{}, false)
	register(t, dstA, &struct{}{}, false)
	register(t, dstB, &struct{}{}, false)

	if got, err := testStore.GetNotify(src); err != nil || len(got) != 0 {
		t.Fatalf("GetNotify on fresh key = %v, %v; want empty", got, err)
	}
	if err := testStore.SetNotify(src, []string{dstA}); err != nil {
		t.Fatalf("SetNotify: %v", err)
	}
	if got, _ := testStore.GetNotify(src); !sameKeys(got, []string{dstA}) {
		t.Fatalf("GetNotify after SetNotify = %v, want [%s]", got, dstA)
	}
	// AddNotify deduplicates.
	if err := testStore.AddNotify(src, dstA, dstB); err != nil {
		t.Fatalf("AddNotify: %v", err)
	}
	if got, _ := testStore.GetNotify(src); !sameKeys(got, []string{dstA, dstB}) {
		t.Fatalf("GetNotify after AddNotify = %v, want [%s %s]", got, dstA, dstB)
	}
	if err := testStore.RemoveNotify(src, dstA); err != nil {
		t.Fatalf("RemoveNotify: %v", err)
	}
	if got, _ := testStore.GetNotify(src); !sameKeys(got, []string{dstB}) {
		t.Fatalf("GetNotify after RemoveNotify = %v, want [%s]", got, dstB)
	}
	// SetNotify replaces the whole list.
	if err := testStore.SetNotify(src, []string{dstB}); err != nil {
		t.Fatalf("SetNotify replace: %v", err)
	}
	if got, _ := testStore.GetNotify(src); !sameKeys(got, []string{dstB}) {
		t.Fatalf("GetNotify after replace = %v, want [%s]", got, dstB)
	}
}

func TestNotifyListValidation(t *testing.T) {
	src := keyOf(t) + "_src"
	register(t, src, &struct{}{}, false)

	if err := testStore.SetNotify("", []string{src}); err == nil {
		t.Error("SetNotify with empty key: want error")
	}
	if err := testStore.SetNotify(src, []string{""}); err == nil {
		t.Error("SetNotify with empty target: want error")
	}
	if err := testStore.SetNotify(src, []string{src}); err == nil {
		t.Error("SetNotify with self target: want error")
	}
	if err := testStore.AddNotify(src, keyOf(t)+"_missing"); err == nil {
		t.Error("AddNotify with unregistered target: want error")
	}
	if _, err := testStore.GetNotify(""); err == nil {
		t.Error("GetNotify with empty key: want error")
	}
	if err := testStore.RemoveNotify("", src); err == nil {
		t.Error("RemoveNotify with empty key: want error")
	}
}

func TestRegisterNotifyToParentMerge(t *testing.T) {
	schema := keyOf(t) + "_schema"
	monitor := keyOf(t) + "_monitor"
	extra := keyOf(t) + "_extra"
	inst := keyOf(t) + "_inst"
	register(t, monitor, &struct{}{}, false)
	register(t, extra, &struct{}{}, false)
	if _, err := testStore.Register(types.RegisterArgs{Key: schema, Object: &struct{}{}, NotifyTo: types.NotifyList{monitor}}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	// The instance inherits the parent's notify list merged with its own.
	if _, err := testStore.Register(types.RegisterArgs{Key: inst, ParentKey: schema, Object: &struct{}{}, NotifyTo: types.NotifyList{extra}}); err != nil {
		t.Fatalf("register instance: %v", err)
	}
	got, err := testStore.GetNotify(inst)
	if err != nil {
		t.Fatalf("GetNotify: %v", err)
	}
	if !sameKeys(got, []string{monitor, extra}) {
		t.Fatalf("instance notify list = %v, want [%s %s]", got, monitor, extra)
	}
}

func TestSetAndPublishEmitNotifications(t *testing.T) {
	fn := &fakeNotifier{}
	testStore.SetNotifier(fn)
	defer testStore.SetNotifier(nil)

	src := keyOf(t) + "_src"
	dstA := keyOf(t) + "_a"
	dstB := keyOf(t) + "_b"
	plain := keyOf(t) + "_plain"
	register(t, dstA, &struct{}{}, false)
	register(t, dstB, &struct{}{}, false)
	register(t, plain, &struct{}{}, false)
	// Registration with Init writes initial data but must not notify.
	register(t, src, &struct{}{}, true)
	if err := testStore.SetNotify(src, []string{dstA, dstB}); err != nil {
		t.Fatalf("SetNotify: %v", err)
	}
	if got := fn.keys(); len(got) != 0 {
		t.Fatalf("Register emitted notifications: %v", got)
	}

	if _, err := testStore.Set(types.SetArgs{Key: src, Object: &struct{}{}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := fn.keys(); !sameKeys(got, []string{dstA, dstB}) {
		t.Fatalf("Set notified %v, want [%s %s]", got, dstA, dstB)
	}

	// An object without a notify list emits nothing.
	if _, err := testStore.Set(types.SetArgs{Key: plain, Object: &struct{}{}}); err != nil {
		t.Fatalf("Set plain: %v", err)
	}
	if got := fn.keys(); !sameKeys(got, []string{dstA, dstB}) {
		t.Fatalf("Set without notify list emitted %v", got)
	}

	// Publish emits as well.
	if !testStore.Publish(types.SetArgs{Key: src, Object: &struct{}{}}) {
		t.Fatal("Publish failed")
	}
	if got := fn.keys(); !sameKeys(got, []string{dstA, dstB, dstA, dstB}) {
		t.Fatalf("Publish notified %v, want each target twice", got)
	}
}

func TestSetWithoutNotifierIsNoOp(t *testing.T) {
	testStore.SetNotifier(nil)
	src := keyOf(t) + "_src"
	dst := keyOf(t) + "_dst"
	register(t, src, &struct{}{}, false)
	register(t, dst, &struct{}{}, false)
	if err := testStore.SetNotify(src, []string{dst}); err != nil {
		t.Fatalf("SetNotify: %v", err)
	}
	// No notifier configured: Set must succeed and simply skip notification.
	if _, err := testStore.Set(types.SetArgs{Key: src, Object: &struct{}{}}); err != nil {
		t.Fatalf("Set without notifier: %v", err)
	}
}
