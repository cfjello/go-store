package job_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

type setDataObj struct {
	Data  struct{} `json:"data"`
	calls int
	err   error
}

func (o *setDataObj) SetData() error {
	o.calls++
	return o.err
}

func TestExecRunGraphCallsUpdMethodAndStopsOnError(t *testing.T) {
	s := store.New(nil, true)
	js := &job.JobService{Store: s}
	if js.Store.UpdMethod() != "SetData" {
		t.Fatalf("updMethod = %q, want SetData", js.Store.UpdMethod())
	}

	leaf := &setDataObj{}
	root := &setDataObj{}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "upd-leaf", Object: leaf, AlwaysRun: true}); err != nil {
		t.Fatalf("register leaf: %v", err)
	}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "upd-root", Object: root}); err != nil {
		t.Fatalf("register root: %v", err)
	}
	if err := js.Store.SetDeps("upd-root", []string{"upd-leaf"}); err != nil {
		t.Fatalf("SetDeps: %v", err)
	}
	if _, err := js.RunGraph("upd-root"); err != nil {
		t.Fatalf("RunGraph: %v", err)
	}

	rg, err := js.ExecRunGraph("upd-root")
	if err != nil {
		t.Fatalf("ExecRunGraph: %v", err)
	}
	if !rg.Nodes["upd-leaf"].IsDirty || !rg.Nodes["upd-root"].IsDirty {
		t.Fatal("expected nodes with no store ID to be dirty")
	}
	if leaf.calls != 1 || root.calls != 1 {
		t.Fatalf("SetData calls leaf=%d root=%d, want 1 and 1", leaf.calls, root.calls)
	}

	leaf.err = errors.New("leaf update failed")
	next := &setDataObj{}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "upd-next", Object: next, AlwaysRun: true}); err != nil {
		t.Fatalf("register next: %v", err)
	}
	if err := js.Store.SetDeps("upd-next", []string{"upd-leaf"}); err != nil {
		t.Fatalf("SetDeps next: %v", err)
	}
	if _, err := js.RunGraph("upd-next"); err != nil {
		t.Fatalf("RunGraph next: %v", err)
	}
	if _, err := js.ExecRunGraph("upd-next"); err == nil {
		t.Fatal("expected ExecRunGraph to stop when the update method fails")
	}
	if next.calls != 0 {
		t.Fatalf("later dirty node was called after a failure: calls = %d", next.calls)
	}
}

func TestExecRunGraphStoreIDRules(t *testing.T) {
	s := store.New(nil, true)
	js := &job.JobService{Store: s}

	missing := &setDataObj{}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "id-missing", Object: missing}); err != nil {
		t.Fatalf("register missing: %v", err)
	}
	if _, err := js.RunGraph("id-missing"); err != nil {
		t.Fatalf("RunGraph missing: %v", err)
	}
	rg, err := js.ExecRunGraph("id-missing")
	if err != nil {
		t.Fatalf("ExecRunGraph missing: %v", err)
	}
	if !rg.Nodes["id-missing"].IsDirty || missing.calls != 1 {
		t.Fatalf("missing store ID: dirty=%v calls=%d", rg.Nodes["id-missing"].IsDirty, missing.calls)
	}

	stable := &setDataObj{}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "id-stable", Object: stable}); err != nil {
		t.Fatalf("register stable: %v", err)
	}
	if _, err := js.Store.Set(types.SetArgs{Key: "id-stable", Object: stable}); err != nil {
		t.Fatalf("set stable: %v", err)
	}
	if _, err := js.RunGraph("id-stable"); err != nil {
		t.Fatalf("RunGraph stable: %v", err)
	}
	rg, err = js.ExecRunGraph("id-stable")
	if err != nil {
		t.Fatalf("ExecRunGraph stable: %v", err)
	}
	if rg.Nodes["id-stable"].IsDirty || stable.calls != 0 {
		t.Fatalf("stable node: dirty=%v calls=%d", rg.Nodes["id-stable"].IsDirty, stable.calls)
	}

	fixed := &setDataObj{}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "id-fixed", Object: fixed}); err != nil {
		t.Fatalf("register fixed: %v", err)
	}
	if _, err := js.Store.Set(types.SetArgs{Key: "id-fixed", Object: fixed}); err != nil {
		t.Fatalf("set fixed: %v", err)
	}
	if _, err := js.RunGraph("id-fixed"); err != nil {
		t.Fatalf("RunGraph fixed: %v", err)
	}
	// Stamp Fixed on the stored node. RunGraph omits Fixed vertices from the
	// order, so the flag is set on the published snapshot instead.
	jKey := "id-fixed_job"
	graphID, err := js.Store.GetCurrStoreID(jKey)
	if err != nil {
		t.Fatalf("job store ID: %v", err)
	}
	var stored types.RunGraph
	if _, err := js.Store.DB.GetData(graphID, &stored); err != nil {
		t.Fatalf("load job graph: %v", err)
	}
	stored.Nodes["id-fixed"].Fixed = true
	jobID := util.Ulid()
	if ok := js.Store.DB.UpsertData(jKey, types.SetArgs{
		Key:     jKey,
		StoreId: jobID,
		JobId:   jobID,
		Object:  stored,
	}); !ok {
		t.Fatal("failed to publish fixed node snapshot")
	}
	rg, err = js.ExecRunGraph("id-fixed")
	if err != nil {
		t.Fatalf("ExecRunGraph fixed: %v", err)
	}
	if rg.Nodes["id-fixed"].IsDirty || fixed.calls != 0 {
		t.Fatalf("fixed node: dirty=%v calls=%d, want clean and uncalled", rg.Nodes["id-fixed"].IsDirty, fixed.calls)
	}
}

// levelObj blocks inside SetData so a test can observe same-level overlap and
// the barrier before the next level starts.
type levelObj struct {
	Data    struct{} `json:"data"`
	started chan struct{}
	release chan struct{}
	err     error
}

func (o *levelObj) SetData() error {
	close(o.started)
	<-o.release
	return o.err
}

func waitStarted(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s to start", what)
	}
}

func TestExecRunGraphLevelParallelism(t *testing.T) {
	s := store.New(nil, true)
	js := &job.JobService{Store: s}

	leafRelease := make(chan struct{})
	leafA := &levelObj{started: make(chan struct{}), release: leafRelease}
	leafB := &levelObj{started: make(chan struct{}), release: leafRelease}
	mid := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	root := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	for key, obj := range map[string]*levelObj{
		"lvl-a": leafA, "lvl-b": leafB, "lvl-mid": mid, "lvl-root": root,
	} {
		if _, err := js.Store.Register(types.RegisterArgs{Key: key, Object: obj}); err != nil {
			t.Fatalf("register %s: %v", key, err)
		}
	}
	if err := js.Store.SetDeps("lvl-mid", []string{"lvl-a", "lvl-b"}); err != nil {
		t.Fatalf("SetDeps mid: %v", err)
	}
	if err := js.Store.SetDeps("lvl-root", []string{"lvl-mid"}); err != nil {
		t.Fatalf("SetDeps root: %v", err)
	}
	if _, err := js.RunGraph("lvl-root"); err != nil {
		t.Fatalf("RunGraph: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := js.ExecRunGraph("lvl-root")
		done <- err
	}()

	// Leaves share a level and must both be in SetData before either is released.
	waitStarted(t, leafA.started, "lvl-a")
	waitStarted(t, leafB.started, "lvl-b")
	select {
	case <-mid.started:
		t.Fatal("next level started before the previous level finished")
	default:
	}
	close(leafRelease)

	waitStarted(t, mid.started, "lvl-mid")
	select {
	case <-root.started:
		t.Fatal("root started before lvl-mid finished")
	default:
	}
	close(mid.release)
	waitStarted(t, root.started, "lvl-root")
	close(root.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecRunGraph: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecRunGraph")
	}
}

func TestExecRunGraphLevelErrorStopsNextLevel(t *testing.T) {
	s := store.New(nil, true)
	js := &job.JobService{Store: s}

	leafRelease := make(chan struct{})
	leaf := &levelObj{started: make(chan struct{}), release: leafRelease, err: errors.New("leaf failed")}
	root := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "err-leaf", Object: leaf}); err != nil {
		t.Fatalf("register leaf: %v", err)
	}
	if _, err := js.Store.Register(types.RegisterArgs{Key: "err-root", Object: root}); err != nil {
		t.Fatalf("register root: %v", err)
	}
	if err := js.Store.SetDeps("err-root", []string{"err-leaf"}); err != nil {
		t.Fatalf("SetDeps: %v", err)
	}
	if _, err := js.RunGraph("err-root"); err != nil {
		t.Fatalf("RunGraph: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := js.ExecRunGraph("err-root")
		done <- err
	}()

	waitStarted(t, leaf.started, "err-leaf")
	close(leafRelease)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ExecRunGraph to stop after the level failed")
		}
	case <-root.started:
		t.Fatal("next level started after the previous level failed")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecRunGraph to stop")
	}
}

func TestExecRunGraphLevelIncreaseStartsNewBranch(t *testing.T) {
	s := store.New(nil, true)
	js := &job.JobService{Store: s}

	leafA := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	midA := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	deepB := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	leafB := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	midB := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	root := &levelObj{started: make(chan struct{}), release: make(chan struct{})}
	objs := map[string]*levelObj{
		"br-leafA": leafA, "br-midA": midA, "br-deepB": deepB,
		"br-leafB": leafB, "br-midB": midB, "br-root": root,
	}
	for key, obj := range objs {
		if _, err := js.Store.Register(types.RegisterArgs{Key: key, Object: obj}); err != nil {
			t.Fatalf("register %s: %v", key, err)
		}
	}
	if _, err := js.RunGraph("br-root"); err != nil {
		t.Fatalf("RunGraph: %v", err)
	}

	// Leaves-first order that climbs branch A, then jumps to the deeper branch B.
	nodes := make(map[string]*types.RunNode, len(objs))
	order := []string{"br-leafA", "br-midA", "br-deepB", "br-leafB", "br-midB", "br-root"}
	levels := map[string]int{
		"br-leafA": 2, "br-midA": 1, "br-deepB": 3,
		"br-leafB": 2, "br-midB": 1, "br-root": 0,
	}
	for key, level := range levels {
		nodes[key] = &types.RunNode{Key: key, Level: level}
	}
	jobID := util.Ulid()
	if ok := js.Store.DB.UpsertData("br-root_job", types.SetArgs{
		Key:     "br-root_job",
		StoreId: jobID,
		JobId:   jobID,
		Object: types.RunGraph{
			RootKey: "br-root",
			Order:   order,
			Nodes:   nodes,
		},
	}); !ok {
		t.Fatal("failed to publish branched run graph")
	}

	done := make(chan error, 1)
	go func() {
		_, err := js.ExecRunGraph("br-root")
		done <- err
	}()

	waitStarted(t, leafA.started, "br-leafA")
	select {
	case <-midA.started:
		t.Fatal("midA started before leafA finished")
	default:
	}
	close(leafA.release)
	waitStarted(t, midA.started, "br-midA")

	// Level rose from 1 to 3: branch B must start without waiting for midA.
	waitStarted(t, deepB.started, "br-deepB")
	select {
	case <-leafB.started:
		t.Fatal("leafB started before deepB finished")
	default:
	}
	close(deepB.release)
	waitStarted(t, leafB.started, "br-leafB")
	close(leafB.release)
	waitStarted(t, midB.started, "br-midB")
	select {
	case <-root.started:
		t.Fatal("root started before both branches finished")
	default:
	}
	close(midA.release)
	close(midB.release)
	waitStarted(t, root.started, "br-root")
	close(root.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecRunGraph: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecRunGraph")
	}
}
