package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cfjello/go-store/pkg/perf"
)

// perfStatsHandler serves GET /perf/stats with optional sort (total|calls),
// top (N busiest) and name (substring) parameters. The route is always
// registered; when recording is disabled it reports 404 perf_disabled.
func (s *Server) perfStatsHandler(w http.ResponseWriter, r *http.Request) {
	if !perf.Enabled() {
		writeError(w, http.StatusNotFound, "perf_disabled", "performance recording is disabled (PERF_ENABLED)")
		return
	}
	q := r.URL.Query()
	stats := perf.Snapshot() // sorted by total time descending

	if name := q.Get("name"); name != "" {
		filtered := stats[:0]
		for _, st := range stats {
			if strings.Contains(st.Name, name) {
				filtered = append(filtered, st)
			}
		}
		stats = filtered
	}

	switch q.Get("sort") {
	case "", "total":
		// Snapshot order.
	case "calls":
		sort.Slice(stats, func(i, j int) bool {
			if stats[i].Calls != stats[j].Calls {
				return stats[i].Calls > stats[j].Calls
			}
			return stats[i].Name < stats[j].Name
		})
	default:
		writeError(w, http.StatusBadRequest, "invalid_sort", "sort must be total or calls")
		return
	}

	if top := q.Get("top"); top != "" {
		n, err := strconv.Atoi(top)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_top", "top must be a positive integer")
			return
		}
		if n < len(stats) {
			stats = stats[:n]
		}
	}

	if stats == nil {
		stats = []perf.Stat{}
	}
	writeJSON(w, http.StatusOK, stats)
}

// perfConfigHandler serves GET /perf/config: enabled flag plus reporter
// settings. Available regardless of the enabled state.
func (s *Server) perfConfigHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, perf.CurrentSettings())
}

// perfEnabledHandler serves PUT /perf/enabled to toggle recording at runtime.
type perfEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) perfEnabledHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	var req perfEnabledRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	perf.SetEnabled(req.Enabled)
	writeJSON(w, http.StatusOK, perf.CurrentSettings())
}

// perfResetHandler serves POST /perf/reset: zero all counters (recorded
// names are kept).
func (s *Server) perfResetHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	perf.Reset()
	w.WriteHeader(http.StatusNoContent)
}
