package server

import (
	"net/http"
	"sort"

	"github.com/cfjello/go-store/pkg/types"
	"github.com/dominikbraun/graph"
)

// definitionResponse is the serialized dependency DAG built by DepsGraph.
// Edges point from a node to its dependency.
type definitionResponse struct {
	RootKey  string       `json:"rootKey"`
	Vertices []vertexJSON `json:"vertices"`
	Edges    []edgeJSON   `json:"edges"`
}

type vertexJSON struct {
	Key       string `json:"key"`
	Level     int    `json:"level"`
	Fixed     bool   `json:"fixed"`
	AlwaysRun bool   `json:"alwaysRun"`
}

type edgeJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// runGraphRequest is the POST /job/run-graphs body.
type runGraphRequest struct {
	RootKey string `json:"rootKey"`
}

// notifyRequest is the POST /job/notifications body.
type notifyRequest struct {
	Key string `json:"key"`
}

// getDefinitionHandler serves GET /job/definitions/{key}: the transient
// dependency DAG for a registered object.
func (s *Server) getDefinitionHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	g, err := s.jobs.DepsGraph(key)
	if err != nil {
		storeError(w, err)
		return
	}
	resp, err := serializeGraph(key, g)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to read dependency graph")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// getOrderHandler serves GET /job/definitions/{key}/order with
// ?direction=root-first (default) or leaves-first. Fixed vertices and their
// descendants are excluded by the store layer.
func (s *Server) getOrderHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	direction := r.URL.Query().Get("direction")
	if direction != "" && direction != "root-first" && direction != "leaves-first" {
		writeError(w, http.StatusBadRequest, "invalid_direction", "direction must be root-first or leaves-first")
		return
	}
	g, err := s.jobs.DepsGraph(key)
	if err != nil {
		storeError(w, err)
		return
	}
	var order []string
	if direction == "leaves-first" {
		order, err = s.jobs.ExecutionOrderLeavesFirst(g)
	} else {
		order, err = s.jobs.ExecutionOrder(g)
	}
	if err != nil {
		storeError(w, err)
		return
	}
	if order == nil {
		order = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"order": order})
}

// createRunGraphHandler serves POST /job/run-graphs: build the dependency
// DAG for rootKey, compute the leaves-first execution order, and persist the
// snapshot under rootKey+"_job".
func (s *Server) createRunGraphHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	var req runGraphRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if !keyRe.MatchString(req.RootKey) {
		writeError(w, http.StatusBadRequest, "invalid_key", "rootKey must match [A-Za-z0-9._~-]{1,128}")
		return
	}
	runGraph, err := s.jobs.RunGraph(req.RootKey)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, runGraph)
}

// getRunGraphHandler serves GET /job/run-graphs/{key}: the newest stored run
// graph snapshot for root key.
func (s *Server) getRunGraphHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	runGraph, err := s.jobs.LatestRunGraph(key)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runGraph)
}

// execRunGraphHandler serves POST /job/run-graphs/{key}/exec. Execution runs
// synchronously; the server's write timeout bounds the response.
func (s *Server) execRunGraphHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	runGraph, err := s.jobs.ExecRunGraph(key)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runGraph)
}

// notifyHandler serves POST /job/notifications: enqueue an asynchronous
// update run for a key. Runs are serialized per key and consecutive
// notifications coalesce into a single pending re-run, so this is safe to
// call repeatedly.
func (s *Server) notifyHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	var req notifyRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if !keyRe.MatchString(req.Key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key must match [A-Za-z0-9._~-]{1,128}")
		return
	}
	if err := s.jobs.Notify(req.Key); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"queued": req.Key})
}

// statusHandler serves GET /job/status: in-flight and pending notification
// runs.
func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.jobs.Status())
}

// serializeGraph converts the dependency DAG into its JSON representation
// with deterministic vertex/edge ordering.
func serializeGraph(rootKey string, g graph.Graph[string, types.JobVertex]) (definitionResponse, error) {
	adj, err := g.AdjacencyMap()
	if err != nil {
		return definitionResponse{}, err
	}
	resp := definitionResponse{
		RootKey:  rootKey,
		Vertices: make([]vertexJSON, 0, len(adj)),
		Edges:    make([]edgeJSON, 0, len(adj)),
	}
	for hash, succ := range adj {
		v, err := g.Vertex(hash)
		if err != nil {
			return definitionResponse{}, err
		}
		resp.Vertices = append(resp.Vertices, vertexJSON{
			Key:       v.Key,
			Level:     v.Level,
			Fixed:     v.Fixed,
			AlwaysRun: v.AlwaysRun,
		})
		for depHash := range succ {
			dep, err := g.Vertex(depHash)
			if err != nil {
				return definitionResponse{}, err
			}
			resp.Edges = append(resp.Edges, edgeJSON{From: v.Key, To: dep.Key})
		}
	}
	sort.Slice(resp.Vertices, func(i, j int) bool {
		if resp.Vertices[i].Level != resp.Vertices[j].Level {
			return resp.Vertices[i].Level < resp.Vertices[j].Level
		}
		return resp.Vertices[i].Key < resp.Vertices[j].Key
	})
	sort.Slice(resp.Edges, func(i, j int) bool {
		if resp.Edges[i].From != resp.Edges[j].From {
			return resp.Edges[i].From < resp.Edges[j].From
		}
		return resp.Edges[i].To < resp.Edges[j].To
	})
	return resp, nil
}
