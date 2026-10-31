package job_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

var errNotifyUpdate = errors.New("update failed")

// ntfyObj is an updMethod target that records calls. When release is set, the
// first call blocks until release is closed; started (when set) is closed as
// the first call begins. onCall runs at the end of each call.
type ntfyObj struct {
	Data    struct{} `json:"data"`
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	onCall  func()
}

func (o *ntfyObj) SetData() error {
	o.mu.Lock()
	o.calls++
	first := o.calls == 1
	o.mu.Unlock()
	// started and release are set before any run and never mutated, so the
	// test goroutine may read them without a lock.
	if first && o.started != nil {
		close(o.started)
	}
	if o.release != nil {
		<-o.release
	}
	if o.onCall != nil {
		o.onCall()
	}
	return nil
}

func (o *ntfyObj) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

// newNotifyTestService creates a JobService wired as the store's notifier,
// mirroring what NewJobService does for the singleton.
func newNotifyTestService(t *testing.T) (*job.JobService, *store.Store) {
	t.Helper()
	s := store.New(nil, true)
	js := &job.JobService{Store: s}
	s.SetNotifier(js)
	return js, s
}

func TestNotifyTriggersDependentRun(t *testing.T) {
	js, s := newNotifyTestService(t)

	src := &ntfyObj{}
	dst := &ntfyObj{}
	if _, err := s.Register(types.RegisterArgs{Key: "n1-src", Object: src}); err != nil {
		t.Fatalf("register src: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n1-dst", Object: dst}); err != nil {
		t.Fatalf("register dst: %v", err)
	}
	if err := s.SetDeps("n1-dst", []string{"n1-src"}); err != nil {
		t.Fatalf("SetDeps: %v", err)
	}
	if err := s.SetNotify("n1-src", []string{"n1-dst"}); err != nil {
		t.Fatalf("SetNotify: %v", err)
	}

	// New data on the leaf notifies the dependent, which runs its job flow.
	if _, err := s.Set(types.SetArgs{Key: "n1-src", Object: src}); err != nil {
		t.Fatalf("Set src: %v", err)
	}
	js.WaitIdle()
	if dst.callCount() != 1 {
		t.Fatalf("dst SetData calls = %d, want 1", dst.callCount())
	}
	// The leaf itself is newer than the dependent and is not re-run.
	if src.callCount() != 0 {
		t.Fatalf("src SetData calls = %d, want 0", src.callCount())
	}

	// A second change triggers exactly one more run.
	if _, err := s.Set(types.SetArgs{Key: "n1-src", Object: src}); err != nil {
		t.Fatalf("Set src again: %v", err)
	}
	js.WaitIdle()
	if dst.callCount() != 2 {
		t.Fatalf("dst SetData calls = %d, want 2", dst.callCount())
	}
}

func TestNotifyCoalescesDuringRun(t *testing.T) {
	js, s := newNotifyTestService(t)

	src := &ntfyObj{}
	dst := &ntfyObj{started: make(chan struct{}), release: make(chan struct{})}
	if _, err := s.Register(types.RegisterArgs{Key: "n2-src", Object: src}); err != nil {
		t.Fatalf("register src: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n2-dst", Object: dst}); err != nil {
		t.Fatalf("register dst: %v", err)
	}
	if err := s.SetDeps("n2-dst", []string{"n2-src"}); err != nil {
		t.Fatalf("SetDeps: %v", err)
	}
	if err := s.SetNotify("n2-src", []string{"n2-dst"}); err != nil {
		t.Fatalf("SetNotify: %v", err)
	}

	if _, err := s.Set(types.SetArgs{Key: "n2-src", Object: src}); err != nil {
		t.Fatalf("Set src: %v", err)
	}
	// The first run is blocked inside dst's update method.
	waitStarted(t, dst.started, "n2-dst first run")

	// Notifications arriving during the run coalesce into a single re-run.
	for i := 0; i < 3; i++ {
		if _, err := s.Set(types.SetArgs{Key: "n2-src", Object: src}); err != nil {
			t.Fatalf("Set src %d: %v", i, err)
		}
	}
	close(dst.release)
	js.WaitIdle()

	// One running + one pending: exactly 2 calls despite 4 Sets.
	if dst.callCount() != 2 {
		t.Fatalf("dst SetData calls = %d, want 2 (coalesced)", dst.callCount())
	}
}

func TestNotifyCycleConverges(t *testing.T) {
	js, s := newNotifyTestService(t)

	objA := &ntfyObj{}
	objB := &ntfyObj{}
	if _, err := s.Register(types.RegisterArgs{Key: "n3-a", Object: objA}); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n3-b", Object: objB}); err != nil {
		t.Fatalf("register b: %v", err)
	}
	// A notify cycle is allowed: convergence comes from the dirty checks.
	if err := s.SetNotify("n3-a", []string{"n3-b"}); err != nil {
		t.Fatalf("SetNotify a: %v", err)
	}
	if err := s.SetNotify("n3-b", []string{"n3-a"}); err != nil {
		t.Fatalf("SetNotify b: %v", err)
	}
	// B's update method writes unconditionally; even then the cycle must die
	// because A, already stored and with no dependencies, is never dirty.
	objB.onCall = func() {
		_, _ = s.Set(types.SetArgs{Key: "n3-b", Object: objB})
	}

	if _, err := s.Set(types.SetArgs{Key: "n3-a", Object: objA}); err != nil {
		t.Fatalf("Set a: %v", err)
	}

	done := make(chan struct{})
	go func() {
		js.WaitIdle()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notify cycle did not converge")
	}
	if objB.callCount() != 1 {
		t.Fatalf("b SetData calls = %d, want 1", objB.callCount())
	}
	if objA.callCount() != 0 {
		t.Fatalf("a SetData calls = %d, want 0", objA.callCount())
	}
}

func TestNotifySharedDependencySerialized(t *testing.T) {
	js, s := newNotifyTestService(t)

	var cur, max atomic.Int32
	leaf := &ntfyObj{release: make(chan struct{})}
	root1 := &ntfyObj{}
	root2 := &ntfyObj{}
	for key, obj := range map[string]*ntfyObj{"n4-leaf": leaf, "n4-r1": root1, "n4-r2": root2} {
		if _, err := s.Register(types.RegisterArgs{Key: key, Object: obj, AlwaysRun: key == "n4-leaf"}); err != nil {
			t.Fatalf("register %s: %v", key, err)
		}
	}
	leaf.onCall = func() {
		c := cur.Add(1)
		for {
			m := max.Load()
			if c <= m || max.CompareAndSwap(m, c) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		cur.Add(-1)
	}
	close(leaf.release)

	if err := s.SetDeps("n4-r1", []string{"n4-leaf"}); err != nil {
		t.Fatalf("SetDeps r1: %v", err)
	}
	if err := s.SetDeps("n4-r2", []string{"n4-leaf"}); err != nil {
		t.Fatalf("SetDeps r2: %v", err)
	}
	if err := s.SetNotify("n4-leaf", []string{"n4-r1", "n4-r2"}); err != nil {
		t.Fatalf("SetNotify leaf: %v", err)
	}

	// One change notifies both roots; their runs overlap but the shared
	// leaf's update method must never run concurrently with itself.
	if _, err := s.Set(types.SetArgs{Key: "n4-leaf", Object: leaf}); err != nil {
		t.Fatalf("Set leaf: %v", err)
	}
	js.WaitIdle()

	if max.Load() != 1 {
		t.Fatalf("max concurrent leaf updates = %d, want 1", max.Load())
	}
	if leaf.callCount() != 2 {
		t.Fatalf("leaf SetData calls = %d, want 2 (one per root run)", leaf.callCount())
	}
	if root1.callCount() != 1 || root2.callCount() != 1 {
		t.Fatalf("root calls r1=%d r2=%d, want 1 and 1", root1.callCount(), root2.callCount())
	}
}

func TestNotifyValidation(t *testing.T) {
	js, _ := newNotifyTestService(t)
	if err := js.Notify(""); err == nil {
		t.Error("Notify with empty key: want error")
	}
	if err := js.Notify("n5-missing"); err == nil {
		t.Error("Notify with unregistered key: want error")
	}
}

func TestNotifyRunErrorReported(t *testing.T) {
	js, s := newNotifyTestService(t)

	errCh := make(chan error, 1)
	js.OnError = func(key string, err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	// The cached object implements SetData but the call fails, so the run errors.
	if _, err := s.Register(types.RegisterArgs{Key: "n6-bad", Object: &setDataObj{err: errNotifyUpdate}}); err != nil {
		t.Fatalf("register bad: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n6-src", Object: &ntfyObj{}, NotifyTo: types.NotifyList{"n6-bad"}}); err != nil {
		t.Fatalf("register src: %v", err)
	}
	if _, err := s.Set(types.SetArgs{Key: "n6-src", Object: &ntfyObj{}}); err != nil {
		t.Fatalf("Set src: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("OnError received nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnError was not called for the failed run")
	}
	js.WaitIdle()
}

func TestNotifyRunFailureDoesNotFailSet(t *testing.T) {
	js, s := newNotifyTestService(t)
	var reported atomic.Int32
	js.OnError = func(key string, err error) { reported.Add(1) }

	if _, err := s.Register(types.RegisterArgs{Key: "n7-bad", Object: &setDataObj{err: errNotifyUpdate}}); err != nil {
		t.Fatalf("register bad: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n7-src", Object: &ntfyObj{}, NotifyTo: types.NotifyList{"n7-bad"}}); err != nil {
		t.Fatalf("register src: %v", err)
	}
	// The async run fails (SetData returns an error), but Set itself
	// must succeed: the data write already happened.
	if _, err := s.Set(types.SetArgs{Key: "n7-src", Object: &ntfyObj{}}); err != nil {
		t.Fatalf("Set must not fail on async run errors: %v", err)
	}
	js.WaitIdle()
	if reported.Load() == 0 {
		t.Fatal("expected the run failure to be reported via OnError")
	}
	if err := js.Notify("n7-src"); err != nil {
		t.Fatalf("manual Notify of valid key: %v", err)
	}
	js.WaitIdle()
}

func TestNotifyStaticTargetIsSkipped(t *testing.T) {
	js, s := newNotifyTestService(t)
	var reported atomic.Int32
	js.OnError = func(key string, err error) { reported.Add(1) }

	// No SetData method → registered as Fixed static data and omitted from the
	// run graph, so the notify-driven run must not fail.
	if _, err := s.Register(types.RegisterArgs{Key: "n8-static", Object: &struct{}{}}); err != nil {
		t.Fatalf("register static: %v", err)
	}
	if _, err := s.Register(types.RegisterArgs{Key: "n8-src", Object: &ntfyObj{}, NotifyTo: types.NotifyList{"n8-static"}}); err != nil {
		t.Fatalf("register src: %v", err)
	}
	if _, err := s.Set(types.SetArgs{Key: "n8-src", Object: &ntfyObj{}}); err != nil {
		t.Fatalf("Set src: %v", err)
	}
	js.WaitIdle()
	if reported.Load() != 0 {
		t.Fatal("static Fixed target should not fail the run")
	}
}
