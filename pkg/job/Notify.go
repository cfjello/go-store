package job

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/cfjello/go-store/pkg/perf"
)

// runState tracks the notification-run state of a single key: at most one
// running run plus one coalesced pending re-run. Guarded by JobService.runsMu.
type runState struct {
	running bool
	pending bool
}

// Notify implements store.Notifier: it validates key and enqueues an update
// run for it, returning immediately while the run executes in the background.
//
// Runs are serialized per key: when a run for key is already in flight, the
// notification is coalesced into a single pending re-run, so an object that is
// notified while its job is running always finishes the current run before the
// same job runs once more. Consecutive notifications never stack beyond one
// pending re-run; convergence of circular notify flows relies on the
// dirty-check invariant (see docs/notifications.md).
func (js *JobService) Notify(key string) error {
	defer perf.Track("job.Notify")()
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	if !js.Store.IsRegistered(key) {
		return fmt.Errorf("key %q is not registered", key)
	}

	js.runsMu.Lock()
	if js.runs == nil {
		js.runs = make(map[string]*runState)
	}
	st := js.runs[key]
	if st == nil {
		st = &runState{}
		js.runs[key] = st
	}
	if st.running {
		st.pending = true
		js.runsMu.Unlock()
		return nil
	}
	st.running = true
	js.inFlight++
	js.runsMu.Unlock()

	go js.runLoop(key, st)
	return nil
}

// WaitIdle blocks until no notification runs are running or queued. It is
// intended for tests and graceful shutdown; calling it while other goroutines
// keep issuing notifications only guarantees that it returns once the queue
// has fully drained.
func (js *JobService) WaitIdle() {
	js.runsMu.Lock()
	defer js.runsMu.Unlock()
	for js.inFlight > 0 {
		js.idleCond().Wait()
	}
}

// runLoop executes runs for key until no notification is pending, then
// retires the key's state. A pending notification is consumed (never stacked)
// before each re-run.
func (js *JobService) runLoop(key string, st *runState) {
	for {
		if err := js.runOnce(key); err != nil {
			js.reportError(key, err)
		}
		js.runsMu.Lock()
		if st.pending {
			st.pending = false
			js.runsMu.Unlock()
			continue
		}
		delete(js.runs, key)
		js.inFlight--
		if js.inFlight == 0 {
			js.idleCond().Broadcast()
		}
		js.runsMu.Unlock()
		return
	}
}

// runOnce rebuilds the dependency graph for key and executes it leaves-first.
// Rebuilding per run keeps triggered runs consistent with the current
// DependsOn metadata and the current store IDs.
func (js *JobService) runOnce(key string) error {
	defer perf.Track("job.runOnce")()
	if _, err := js.RunGraph(key); err != nil {
		return fmt.Errorf("failed to build run graph for %q: %v", key, err)
	}
	if _, err := js.ExecRunGraph(key); err != nil {
		return fmt.Errorf("failed to execute run graph for %q: %v", key, err)
	}
	return nil
}

// reportError surfaces a failed notification run via OnError when set, or via
// log.Printf otherwise.
func (js *JobService) reportError(key string, err error) {
	if js.OnError != nil {
		js.OnError(key, err)
		return
	}
	log.Printf("notification run for %q failed: %v", key, err)
}

// idleCond returns the idle condition variable, binding it to runsMu on first
// use. The caller must hold runsMu. Lazy binding keeps literally constructed
// JobService values (as used in tests) working without a constructor.
func (js *JobService) idleCond() *sync.Cond {
	if js.idle.L == nil {
		js.idle.L = &js.runsMu
	}
	return &js.idle
}

// objLock returns the mutex serializing updMethod calls for a single object
// key across all concurrent runs.
func (js *JobService) objLock(key string) *sync.Mutex {
	mu, _ := js.objLocks.LoadOrStore(key, &sync.Mutex{})
	return mu.(*sync.Mutex)
}
