package job_test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

// graphObj is a runnable store object: it has SetData and a data field, so
// Register does not force Fixed.
type graphObj struct {
	Data struct{} `json:"data"`
}

func (o *graphObj) SetData() error { return nil }

// newTestJobService creates a JobService backed by a store. A new struct is
// used instead of NewJobService so tests do not share the process-wide
// JobService singleton.
func newTestJobService(t *testing.T) *job.JobService {
	t.Helper()
	s := store.New(nil, true)
	if s == nil {
		t.Fatalf("failed to create store service")
	}
	return &job.JobService{Store: s}
}

// registerKeys registers the given keys as runnable objects without dependencies.
func registerKeys(t *testing.T, s *store.Store, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := s.Register(types.RegisterArgs{Key: key, Object: &graphObj{}}); err != nil {
			t.Fatalf("failed to register key %q: %v", key, err)
		}
	}
}

func TestDepsGraphLinear(t *testing.T) {
	js := newTestJobService(t)
	registerKeys(t, js.Store, "A", "B", "C")

	// A -> B -> C
	if err := js.Store.SetDeps("A", []string{"B"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"C"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}

	g, err := js.DepsGraph("A")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}

	adj, err := g.AdjacencyMap()
	if err != nil {
		t.Fatalf("AdjacencyMap failed: %v", err)
	}
	if len(adj) != 3 {
		t.Errorf("expected 3 vertices, got %d", len(adj))
	}

	order, err := js.ExecutionOrder(g)
	if err != nil {
		t.Fatalf("ExecutionOrder failed: %v", err)
	}
	// Edges point to dependencies, so the order is root first: A, B, C.
	assertBefore(t, order, "A", "B")
	assertBefore(t, order, "B", "C")
}

func TestDepsGraphDiamond(t *testing.T) {
	js := newTestJobService(t)
	registerKeys(t, js.Store, "A", "B", "C", "D")

	// A -> B, A -> C, B -> D, C -> D (diamond, shared subtree at D)
	if err := js.Store.SetDeps("A", []string{"B", "C"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"D"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}
	if err := js.Store.SetDeps("C", []string{"D"}); err != nil {
		t.Fatalf("SetDeps C: %v", err)
	}

	g, err := js.DepsGraph("A")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}

	adj, err := g.AdjacencyMap()
	if err != nil {
		t.Fatalf("AdjacencyMap failed: %v", err)
	}
	if len(adj) != 4 {
		t.Errorf("expected 4 vertices, got %d", len(adj))
	}

	order, err := js.ExecutionOrder(g)
	if err != nil {
		t.Fatalf("ExecutionOrder failed: %v", err)
	}
	// Root first, deepest dependency (D) last.
	assertBefore(t, order, "A", "B")
	assertBefore(t, order, "A", "C")
	assertBefore(t, order, "B", "D")
	assertBefore(t, order, "C", "D")
}

func TestDepsGraphCycle(t *testing.T) {
	js := newTestJobService(t)
	registerKeys(t, js.Store, "A", "B")

	// A -> B -> A forms a cycle; the Acyclic trait must reject it.
	if err := js.Store.SetDeps("A", []string{"B"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"A"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}

	if _, err := js.DepsGraph("A"); err == nil {
		t.Errorf("expected an error for cyclic dependencies, got nil")
	}
}

func TestDepsGraphVertexFixed(t *testing.T) {
	js := newTestJobService(t)

	if _, err := js.Store.Register(types.RegisterArgs{Key: "F", Object: &struct{}{}, Fixed: true}); err != nil {
		t.Fatalf("failed to register key F: %v", err)
	}
	registerKeys(t, js.Store, "G")
	if err := js.Store.SetDeps("F", []string{"G"}); err != nil {
		t.Fatalf("SetDeps F: %v", err)
	}

	g, err := js.DepsGraph("F")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}

	adj, err := g.AdjacencyMap()
	if err != nil {
		t.Fatalf("AdjacencyMap failed: %v", err)
	}
	found := make(map[string]bool)
	for hash := range adj {
		v, err := g.Vertex(hash)
		if err != nil {
			t.Fatalf("failed to get vertex %q: %v", hash, err)
		}
		switch v.Key {
		case "F":
			if !v.Fixed {
				t.Errorf("expected vertex F to have Fixed=true")
			}
		case "G":
			if v.Fixed {
				t.Errorf("expected vertex G to have Fixed=false")
			}
		}
		found[v.Key] = true
	}
	if !found["F"] || !found["G"] {
		t.Errorf("expected vertices F and G in graph, got %v", found)
	}
}

func TestExecutionOrderSkipsFixed(t *testing.T) {
	js := newTestJobService(t)

	// A -> B (fixed) -> D, and A -> C -> D.
	// B and its child D must be skipped; A and C remain.
	if _, err := js.Store.Register(types.RegisterArgs{Key: "B", Object: &struct{}{}, Fixed: true}); err != nil {
		t.Fatalf("failed to register key B: %v", err)
	}
	registerKeys(t, js.Store, "A", "C", "D")

	if err := js.Store.SetDeps("A", []string{"B", "C"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"D"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}
	if err := js.Store.SetDeps("C", []string{"D"}); err != nil {
		t.Fatalf("SetDeps C: %v", err)
	}

	g, err := js.DepsGraph("A")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}
	order, err := js.ExecutionOrder(g)
	if err != nil {
		t.Fatalf("ExecutionOrder failed: %v", err)
	}

	for _, key := range order {
		if key == "B" || key == "D" {
			t.Errorf("expected Fixed vertex B and its child D to be skipped, got order %v", order)
		}
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 vertices in order (A and C), got %v", order)
	}
	assertBefore(t, order, "A", "C")
}

func TestExecutionOrderLeavesFirst(t *testing.T) {
	js := newTestJobService(t)
	registerKeys(t, js.Store, "A", "B", "C")

	// A -> B -> C; leaves first means C, B, A.
	if err := js.Store.SetDeps("A", []string{"B"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"C"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}

	g, err := js.DepsGraph("A")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}
	order, err := js.ExecutionOrderLeavesFirst(g)
	if err != nil {
		t.Fatalf("ExecutionOrderLeavesFirst failed: %v", err)
	}
	assertBefore(t, order, "C", "B")
	assertBefore(t, order, "B", "A")
}

func TestExecutionOrderLeavesFirstSkipsFixed(t *testing.T) {
	js := newTestJobService(t)

	// A -> B (fixed) -> D, and A -> C -> D: leaves first still skips B and D.
	if _, err := js.Store.Register(types.RegisterArgs{Key: "B", Object: &struct{}{}, Fixed: true}); err != nil {
		t.Fatalf("failed to register key B: %v", err)
	}
	registerKeys(t, js.Store, "A", "C", "D")

	if err := js.Store.SetDeps("A", []string{"B", "C"}); err != nil {
		t.Fatalf("SetDeps A: %v", err)
	}
	if err := js.Store.SetDeps("B", []string{"D"}); err != nil {
		t.Fatalf("SetDeps B: %v", err)
	}
	if err := js.Store.SetDeps("C", []string{"D"}); err != nil {
		t.Fatalf("SetDeps C: %v", err)
	}

	g, err := js.DepsGraph("A")
	if err != nil {
		t.Fatalf("DepsGraph failed: %v", err)
	}
	order, err := js.ExecutionOrderLeavesFirst(g)
	if err != nil {
		t.Fatalf("ExecutionOrderLeavesFirst failed: %v", err)
	}

	for _, key := range order {
		if key == "B" || key == "D" {
			t.Errorf("expected Fixed vertex B and its child D to be skipped, got order %v", order)
		}
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 vertices in order (C and A), got %v", order)
	}
	// Leaves first: C before the root A.
	assertBefore(t, order, "C", "A")
}

func TestDepsGraphErrors(t *testing.T) {
	js := newTestJobService(t)

	if _, err := js.DepsGraph(""); err == nil {
		t.Errorf("expected an error for empty key, got nil")
	}
	if _, err := js.DepsGraph("missing"); err == nil {
		t.Errorf("expected an error for unregistered key, got nil")
	}
}

func assertBefore(t *testing.T, order []string, first, second string) {
	t.Helper()
	idxFirst, idxSecond := -1, -1
	for i, key := range order {
		if key == first {
			idxFirst = i
		}
		if key == second {
			idxSecond = i
		}
	}
	if idxFirst == -1 || idxSecond == -1 {
		t.Fatalf("keys %q and %q not both present in order %v", first, second, order)
	}
	if idxFirst >= idxSecond {
		t.Errorf("expected %q before %q in order %v", first, second, order)
	}
}
