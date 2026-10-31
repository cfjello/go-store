package job

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cfjello/go-store/pkg/types"
)

// RunStatus reports the notification-run state of a single key: a run is in
// flight for the key, and Pending marks a coalesced re-run waiting behind it.
type RunStatus struct {
	Key     string `json:"key"`
	Pending bool   `json:"pending"`
}

// Status is a point-in-time snapshot of the notification runner: how many
// run loops are in flight and the per-key run state, sorted by key.
type Status struct {
	InFlight int         `json:"inFlight"`
	Runs     []RunStatus `json:"runs"`
}

// Status returns a consistent snapshot of the notification runner state.
// Intended for monitoring and the GET /job/status endpoint.
func (js *JobService) Status() Status {
	js.runsMu.Lock()
	defer js.runsMu.Unlock()
	out := Status{
		InFlight: js.inFlight,
		Runs:     make([]RunStatus, 0, len(js.runs)),
	}
	for key, st := range js.runs {
		out.Runs = append(out.Runs, RunStatus{Key: key, Pending: st.pending})
	}
	sort.Slice(out.Runs, func(i, j int) bool { return out.Runs[i].Key < out.Runs[j].Key })
	return out
}

// LatestRunGraph loads the newest run graph stored under key+"_job". ULIDs
// are time-ordered, so the current store ID of the job key is the newest
// snapshot. It returns an error when no run graph has been published for key.
func (js *JobService) LatestRunGraph(key string) (types.RunGraph, error) {
	if key == "" {
		return types.RunGraph{}, errors.New("the key cannot be empty")
	}
	storeID, err := js.Store.GetCurrStoreID(jobKey(key))
	if err != nil {
		return types.RunGraph{}, fmt.Errorf("no stored job graph for %q: %v", key, err)
	}
	// GetData unmarshals directly into the typed RunGraph.
	var runGraph types.RunGraph
	if _, err := js.Store.DB.GetData(storeID, &runGraph); err != nil {
		return types.RunGraph{}, fmt.Errorf("failed to load job graph %q: %v", key, err)
	}
	return runGraph, nil
}
