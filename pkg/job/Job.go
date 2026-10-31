package job

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
	"github.com/dominikbraun/graph"
)

type JobService struct {
	JobGraph graph.Graph[string, types.JobVertex]
	Store    *store.Store
	mu       sync.Mutex // guards JobGraph; ExecRunGraph uses only local state

	// OnError, when set, receives the error of every failed notification run.
	// When nil, failures are reported via log.Printf.
	OnError func(key string, err error)

	runsMu   sync.Mutex           // guards runs and inFlight
	runs     map[string]*runState // per-key notification run state
	inFlight int                  // number of active runLoop goroutines
	idle     sync.Cond            // signalled when inFlight reaches 0; L is runsMu
	objLocks sync.Map             // node key -> *sync.Mutex serializing updMethod calls
}

var js *JobService

func NewJobService(store *store.Store) *JobService {
	g := graph.New(func(jv types.JobVertex) string { return jv.Key + "_" + jv.JobId }, graph.Directed(), graph.Acyclic(), graph.PreventCycles())
	if js != nil {
		return js
	} else {
		js = &JobService{
			JobGraph: g,
			Store:    store,
		}
		// Register as the store's change notifier so successful Set/Publish
		// calls enqueue update runs for the objects named in notify lists.
		store.SetNotifier(js)
	}
	return js
}

// DepsGraph builds a directed acyclic graph of the dependencies of the object
// identified by key, based on the DependsOn entries of the object itself and,
// recursively, of its children. A fresh JobId is generated per call so each
// traversal is identified as a distinct job. The resulting graph is stored on
// the service and returned.
func (js *JobService) DepsGraph(key string) (graph.Graph[string, types.JobVertex], error) {
	defer perf.Track("job.DepsGraph")()
	if key == "" {
		return nil, errors.New("the key cannot be empty")
	}
	if !js.Store.IsRegistered(key) {
		return nil, fmt.Errorf("key %q is not registered", key)
	}

	jobId := util.Ulid()
	g := graph.New(func(jv types.JobVertex) string { return jv.Key + "_" + jv.JobId }, graph.Directed(), graph.Acyclic(), graph.PreventCycles())

	visited := make(map[string]bool)
	inPath := make(map[string]bool)
	if err := js.buildDepsGraph(g, key, jobId, 0, visited, inPath); err != nil {
		return nil, err
	}
	return g, nil
}

// buildDepsGraph recursively adds the vertex for currKey and edges to each of
// its dependencies. Vertices and edges are added before the visited check so
// that shared subtrees (diamonds) are fully connected, while recursion into
// already expanded nodes is skipped. inPath tracks the current recursion path
// so that dependency cycles are detected and reported explicitly; the graph's
// PreventCycles trait acts as a second layer of protection.
func (js *JobService) buildDepsGraph(g graph.Graph[string, types.JobVertex], currKey string, jobId string, level int, visited, inPath map[string]bool) error {
	// GetMeta returns both the Fixed flag and the DependsOn list in one call
	meta, err := js.Store.GetMeta(currKey)
	if err != nil {
		return fmt.Errorf("failed to get metadata for key %q: %v", currKey, err)
	}

	vertex := types.JobVertex{
		JobId:     jobId,
		Key:       currKey,
		NodeName:  currKey,
		Level:     level,
		Fixed:     meta.Fixed,
		AlwaysRun: meta.AlwaysRun,
	}
	if err := g.AddVertex(vertex); err != nil && !errors.Is(err, graph.ErrVertexAlreadyExists) {
		return fmt.Errorf("failed to add vertex for key %q: %v", currKey, err)
	}

	deps := meta.DependsOn.GetDeps()

	for _, dep := range deps {
		if inPath[dep] {
			return fmt.Errorf("dependency cycle detected: %q -> %q", currKey, dep)
		}
		depMeta, err := js.Store.GetMeta(dep)
		if err != nil {
			return fmt.Errorf("dependency %q of key %q is not registered: %v", dep, currKey, err)
		}
		childVertex := types.JobVertex{
			JobId:     jobId,
			Key:       dep,
			NodeName:  dep,
			Level:     level + 1,
			Fixed:     depMeta.Fixed,
			AlwaysRun: depMeta.AlwaysRun,
		}
		if err := g.AddVertex(childVertex); err != nil && !errors.Is(err, graph.ErrVertexAlreadyExists) {
			return fmt.Errorf("failed to add vertex for dependency %q: %v", dep, err)
		}
		// The Acyclic trait makes AddEdge fail if this edge would create a cycle.
		if err := g.AddEdge(currKey+"_"+jobId, dep+"_"+jobId); err != nil {
			if errors.Is(err, graph.ErrEdgeAlreadyExists) {
				continue
			}
			return fmt.Errorf("failed to add edge %q -> %q (dependency cycle?): %v", currKey, dep, err)
		}
	}

	if visited[currKey] {
		return nil
	}
	visited[currKey] = true
	inPath[currKey] = true
	defer delete(inPath, currKey)

	for _, dep := range deps {
		if err := js.buildDepsGraph(g, dep, jobId, level+1, visited, inPath); err != nil {
			return err
		}
	}
	return nil
}

// ExecutionOrder returns the keys of the given dependency graph in topological
// order, after applying a transitive reduction to remove redundant edges.
// Edges point from a node to its dependencies, so the order is root first and
// deepest dependencies last. Vertices marked Fixed (and all of their
// descendants) are excluded from the returned order.
func (js *JobService) ExecutionOrder(g graph.Graph[string, types.JobVertex]) ([]string, error) {
	trans, err := graph.TransitiveReduction(g)
	if err != nil {
		return nil, fmt.Errorf("failed to compute transitive reduction: %v", err)
	}
	hashes, err := graph.TopologicalSort(trans)
	if err != nil {
		return nil, fmt.Errorf("failed to compute topological order: %v", err)
	}

	adj, err := trans.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("failed to get adjacency map: %v", err)
	}

	// skip marks a vertex and, recursively, all of its children (successors).
	skipped := make(map[string]bool)
	var skip func(hash string)
	skip = func(hash string) {
		if skipped[hash] {
			return
		}
		skipped[hash] = true
		for succ := range adj[hash] {
			skip(succ)
		}
	}

	order := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if skipped[hash] {
			continue
		}
		vertex, err := trans.Vertex(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get vertex %q: %v", hash, err)
		}
		// A Fixed vertex and its children are excluded from the order.
		if vertex.Fixed {
			skip(hash)
			continue
		}
		order = append(order, vertex.Key)
	}
	return order, nil
}

// ExecutionOrderLeavesFirst returns the keys of the given dependency graph in
// reverse topological order: leaf dependencies first and the root last. This is
// the order in which jobs can be executed so that every dependency is processed
// before the node that depends on it. Vertices marked Fixed (and all of their
// descendants) are excluded from the returned order.
func (js *JobService) ExecutionOrderLeavesFirst(g graph.Graph[string, types.JobVertex]) ([]string, error) {
	order, err := js.ExecutionOrder(g)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order, nil
}

// jobKey returns the store key under which the run graph for a root element is
// registered and published.
func jobKey(rootKey string) string {
	return rootKey + "_job"
}

// RunGraph builds the dependency DAG for rootKey, computes its leaves-first
// execution order, registers the run graph under rootKey+"_job" (DataGroup
// "job") when not already registered, and publishes the resulting RunGraph
// snapshot to the store.
func (js *JobService) RunGraph(rootKey string) (types.RunGraph, error) {
	defer perf.Track("job.RunGraph")()
	g, err := js.DepsGraph(rootKey)
	if err != nil {
		return types.RunGraph{}, fmt.Errorf("failed to build dependency graph for %q: %v", rootKey, err)
	}

	order, err := js.ExecutionOrderLeavesFirst(g)
	if err != nil {
		return types.RunGraph{}, fmt.Errorf("failed to compute execution order for %q: %v", rootKey, err)
	}

	// The adjacency map holds every vertex hash (including leaves with no
	// successors) and each vertex's outgoing edges to its dependencies.
	adj, err := g.AdjacencyMap()
	if err != nil {
		return types.RunGraph{}, fmt.Errorf("failed to get adjacency map for %q: %v", rootKey, err)
	}

	// Collect the full vertex set (the leaves-first order may exclude Fixed
	// nodes) and capture per-node metadata.
	nodes := make(map[string]*types.RunNode, len(adj))
	for hash := range adj {
		v, err := g.Vertex(hash)
		if err != nil {
			return types.RunGraph{}, fmt.Errorf("failed to get vertex %q: %v", hash, err)
		}
		storeID, _ := js.Store.GetCurrStoreID(v.Key)
		deps := make([]string, 0, len(adj[hash]))
		for succ := range adj[hash] {
			sv, err := g.Vertex(succ)
			if err != nil {
				return types.RunGraph{}, fmt.Errorf("failed to get successor vertex %q: %v", succ, err)
			}
			deps = append(deps, sv.Key)
		}
		nodes[v.Key] = &types.RunNode{
			Key:       v.Key,
			StoreId:   storeID,
			Level:     v.Level,
			Fixed:     v.Fixed,
			AlwaysRun: v.AlwaysRun,
			DependsOn: deps,
		}
	}

	rootStoreID, _ := js.Store.GetCurrStoreID(rootKey)
	runGraph := types.RunGraph{
		RootKey:     rootKey,
		RootStoreId: rootStoreID,
		Order:       order,
		Nodes:       nodes,
	}

	// Register the run graph under rootKey+"_job" if not already registered.
	jKey := jobKey(rootKey)
	if !js.Store.IsRegistered(jKey) {
		if _, err := js.Store.Register(types.RegisterArgs{Key: jKey, Object: &runGraph}); err != nil {
			return types.RunGraph{}, fmt.Errorf("failed to register job graph %q: %v", jKey, err)
		}
	}

	// Publish the run graph snapshot to the store. UpsertData indexes the row
	// under jKey (meta_key). The storeId used for later retrieval is the jobId.
	jobId := util.Ulid()
	if ok := js.Store.DB.UpsertData(jKey, types.SetArgs{
		Key:     jKey,
		StoreId: jobId,
		JobId:   jobId,
		Object:  runGraph,
	}); !ok {
		return types.RunGraph{}, fmt.Errorf("failed to publish job graph %q", jKey)
	}

	return runGraph, nil
}

// ExecRunGraph loads the newest run graph stored under key+"_job" and refreshes
// each node's store ID. A node is dirty when that store ID is missing, when the
// node is Fixed, or when isDirty reports that dependencies or AlwaysRun
// require a re-run. A dirty node is updated by calling the no-argument method
// named by the store's updMethod on the object cached under that node's key.
// Calls at the same node.Level run concurrently. Walking up a branch (the level
// decreases) waits for every deeper in-flight call. A level increase is another
// branch: it does not wait for the shallower branch already running, and
// prevLevel moves to the new branch. The first call (prevLevel == -1) does not
// wait. A failed lookup or call stops later launches and is returned.
func (js *JobService) ExecRunGraph(key string) (types.RunGraph, error) {
	defer perf.Track("job.ExecRunGraph")()
	runGraph, err := js.LatestRunGraph(key)
	if err != nil {
		return types.RunGraph{}, err
	}

	// prevLevel is the level of the previous node. -1 means the walk has not
	// started, so the first call does not wait. Each level keeps its own
	// in-flight batch so a new branch does not join a shallower one.
	prevLevel := -1
	method := js.Store.UpdMethod()
	type levelBatch struct {
		wg  sync.WaitGroup
		mu  sync.Mutex
		err error
	}
	batches := make(map[int]*levelBatch)
	// waitDeeperThan joins every in-flight batch deeper than level (Promise.all
	// for those batches only) and removes them.
	waitDeeperThan := func(level int) error {
		var errs []error
		for l, batch := range batches {
			if l <= level {
				continue
			}
			batch.wg.Wait()
			if batch.err != nil {
				errs = append(errs, batch.err)
			}
			delete(batches, l)
		}
		return errors.Join(errs...)
	}
	abort := func(err error) (types.RunGraph, error) {
		if rest := waitDeeperThan(-1); rest != nil {
			err = errors.Join(err, rest)
		}
		return types.RunGraph{}, err
	}

	// Refresh each node's current store ID and compute IsDirty.
	for _, nodeKey := range runGraph.Order {
		node := runGraph.Nodes[nodeKey]
		if node == nil {
			continue
		}
		if prevLevel != -1 && node.Level != prevLevel {
			// Level decrease: moving up this branch, so deeper calls must finish.
			// Level increase: another branch. Join only calls deeper than the new
			// branch and leave the previous, shallower branch running.
			if err := waitDeeperThan(node.Level); err != nil {
				return abort(err)
			}
		}
		prevLevel = node.Level

		currStoreID, err := js.Store.GetCurrStoreID(nodeKey)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return abort(fmt.Errorf("failed to get current store ID for %q: %v", nodeKey, err))
			}
			// No stored version for this node. It must run; skip isDirty.
			node.StoreId = ""
			node.IsDirty = true
		} else {
			node.StoreId = currStoreID
			if node.Fixed {
				// Fixed nodes are not re-run. Skip isDirty.
				node.IsDirty = false
			} else {
				node.IsDirty = js.isDirty(node, runGraph)
			}
		}
		if !node.IsDirty {
			continue
		}
		obj, ok := js.Store.GetCache(nodeKey)
		if !ok || obj == nil {
			return abort(fmt.Errorf("no cached object for dirty node %q", nodeKey))
		}
		batch := batches[node.Level]
		if batch == nil {
			batch = &levelBatch{}
			batches[node.Level] = batch
		}
		batch.wg.Add(1)
		go func(batch *levelBatch, obj any, nodeKey string) {
			defer batch.wg.Done()
			// Serialize updMethod calls per object: concurrent runs of
			// different roots may share this node. One lock is held at a
			// time and never nested, so this cannot deadlock.
			mu := js.objLock(nodeKey)
			mu.Lock()
			defer mu.Unlock()
			if err := callNoArgMethod(obj, method); err != nil {
				batch.mu.Lock()
				if batch.err == nil {
					batch.err = fmt.Errorf("failed to call %s on dirty node %q: %v", method, nodeKey, err)
				}
				batch.mu.Unlock()
			}
		}(batch, obj, nodeKey)
	}
	if err := waitDeeperThan(-1); err != nil {
		return types.RunGraph{}, err
	}
	return runGraph, nil
}

// callNoArgMethod invokes the exported method name on obj with no arguments.
// A missing method, a method that requires arguments, a panic, or a non-nil
// error result is a failed call.
func callNoArgMethod(obj any, name string) (err error) {
	if name == "" {
		return errors.New("update method name is empty")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s panicked: %v", name, r)
		}
	}()

	method := reflect.ValueOf(obj).MethodByName(name)
	if !method.IsValid() {
		return fmt.Errorf("method %s not found on %T", name, obj)
	}
	if method.Type().NumIn() != 0 {
		return fmt.Errorf("method %s on %T requires %d arguments", name, obj, method.Type().NumIn())
	}
	for _, result := range method.Call(nil) {
		if result.Kind() == reflect.Interface && result.IsNil() {
			continue
		}
		if !result.CanInterface() {
			continue
		}
		if callErr, ok := result.Interface().(error); ok && callErr != nil {
			return callErr
		}
	}
	return nil
}

// isDirty reports whether a node must re-run. A leaf is dirty when it is older
// than the root element's store ID or when AlwaysRun is set. A non-leaf is
// dirty when one or more of its children have a newer (larger) store ID.
func (js *JobService) isDirty(node *types.RunNode, rg types.RunGraph) bool {
	if node.AlwaysRun {
		return true
	}
	if len(node.DependsOn) == 0 {
		// Leaf node: dirty when older than the root element.
		return node.StoreId < rg.RootStoreId
	}
	// Non-leaf: dirty when any child is newer.
	for _, childKey := range node.DependsOn {
		child := rg.Nodes[childKey]
		childStoreId := node.StoreId
		if child != nil {
			childStoreId = child.StoreId
		} else if id, err := js.Store.GetCurrStoreID(childKey); err == nil {
			childStoreId = id
		}
		if node.StoreId < childStoreId {
			return true
		}
	}
	return false
}
