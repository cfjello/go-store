package server

import (
	"encoding/json"
	"log"
	"net/http"
)

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	// Probes
	mux.HandleFunc("GET /{$}", s.HelloWorldHandler)
	mux.HandleFunc("GET /health", s.healthHandler)

	// Store: registered objects, data versions, and metadata
	mux.HandleFunc("GET /store/keys", s.listKeysHandler)
	mux.HandleFunc("POST /store/objects", s.registerObjectHandler)
	mux.HandleFunc("GET /store/objects/{key}", s.getObjectHandler)
	mux.HandleFunc("PUT /store/objects/{key}", s.setObjectHandler)
	mux.HandleFunc("DELETE /store/objects/{key}", s.deleteObjectHandler)
	mux.HandleFunc("POST /store/objects/{key}/publish", s.publishObjectHandler)
	mux.HandleFunc("GET /store/objects/{key}/versions", s.listVersionsHandler)
	mux.HandleFunc("GET /store/objects/{key}/meta", s.getMetaHandler)
	mux.HandleFunc("PATCH /store/objects/{key}/meta", s.patchMetaHandler)
	mux.HandleFunc("GET /store/objects/{key}/deps", s.getDepsHandler)
	mux.HandleFunc("PUT /store/objects/{key}/deps", s.putDepsHandler)
	mux.HandleFunc("POST /store/objects/{key}/deps", s.postDepsHandler)
	mux.HandleFunc("DELETE /store/objects/{key}/deps", s.deleteDepsHandler)
	mux.HandleFunc("GET /store/objects/{key}/notify", s.getNotifyHandler)
	mux.HandleFunc("PUT /store/objects/{key}/notify", s.putNotifyHandler)
	mux.HandleFunc("POST /store/objects/{key}/notify", s.postNotifyHandler)
	mux.HandleFunc("DELETE /store/objects/{key}/notify", s.deleteNotifyHandler)

	// Jobs: dependency definitions, run graphs, executions, notifications
	mux.HandleFunc("GET /job/definitions/{key}", s.getDefinitionHandler)
	mux.HandleFunc("GET /job/definitions/{key}/order", s.getOrderHandler)
	mux.HandleFunc("POST /job/run-graphs", s.createRunGraphHandler)
	mux.HandleFunc("GET /job/run-graphs/{key}", s.getRunGraphHandler)
	mux.HandleFunc("POST /job/run-graphs/{key}/exec", s.execRunGraphHandler)
	mux.HandleFunc("POST /job/notifications", s.notifyHandler)
	mux.HandleFunc("GET /job/status", s.statusHandler)

	// Performance: always registered, gated at handler time by perf.Enabled
	mux.HandleFunc("GET /perf/stats", s.perfStatsHandler)
	mux.HandleFunc("GET /perf/config", s.perfConfigHandler)
	mux.HandleFunc("PUT /perf/enabled", s.perfEnabledHandler)
	mux.HandleFunc("POST /perf/reset", s.perfResetHandler)

	// Wrap the mux with CORS middleware
	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	origin := s.rest.AllowedOrigins
	if origin == "" {
		origin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "false")

		// Handle preflight OPTIONS requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Proceed with the next handler
		next.ServeHTTP(w, r)
	})
}

func (s *Server) HelloWorldHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"message": "Hello World"}
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(jsonResp); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	resp, err := json.Marshal(s.db.Health())
	if err != nil {
		http.Error(w, "Failed to marshal health check response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}
