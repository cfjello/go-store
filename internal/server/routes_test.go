package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cfjello/go-store/pkg/perf"
)

func TestHandler(t *testing.T) {
	s := &Server{}
	server := httptest.NewServer(http.HandlerFunc(s.HelloWorldHandler))
	defer server.Close()
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("error making request to server. Err: %v", err)
	}
	defer resp.Body.Close()
	// Assertions
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK; got %v", resp.Status)
	}
	expected := "{\"message\":\"Hello World\"}"
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading response body. Err: %v", err)
	}
	if expected != string(body) {
		t.Errorf("expected response body to be %v; got %v", expected, string(body))
	}
}

func TestPerfStatsRoute(t *testing.T) {
	perf.SetEnabled(true)
	t.Cleanup(func() { perf.SetEnabled(false) })

	handler := (&Server{}).RegisterRoutes()
	req := httptest.NewRequest(http.MethodGet, "/perf/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var stats []perf.Stat
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}

func TestPerfStatsRouteReportsDisabled(t *testing.T) {
	perf.SetEnabled(false)
	handler := (&Server{}).RegisterRoutes()
	req := httptest.NewRequest(http.MethodGet, "/perf/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// The route is always registered; when perf is disabled it reports 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when perf is disabled, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "perf_disabled") {
		t.Fatalf("expected perf_disabled error body, got %s", rec.Body.String())
	}
}
