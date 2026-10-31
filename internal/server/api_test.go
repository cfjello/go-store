package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/config"
	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

// apiTestEnv holds the shared test servers. database.New and
// job.NewJobService are process-wide singletons, so all tests in this
// package share one in-memory database, one store, and one job service;
// every test uses its own key prefix to stay independent (and the package
// must not be re-run with -count>1).
type apiTestEnv struct {
	readOnly *httptest.Server // EnableWrites: false
	writable *httptest.Server // EnableWrites: true
	jobs     *job.JobService
}

var (
	envOnce sync.Once
	env     *apiTestEnv
)

func testEnv(t *testing.T) *apiTestEnv {
	t.Helper()
	envOnce.Do(func() {
		os.Setenv("SQLITE_DB_URL", ":memory:")
		db := database.New(false)
		st := store.New(db, false)
		js := job.NewJobService(st)

		newServer := func(writes bool) *httptest.Server {
			srv := &Server{
				db:    db,
				store: st,
				jobs:  js,
				rest: config.RestAPI{
					EnableWrites:   writes,
					MaxBodyBytes:   1 << 20,
					AllowedOrigins: "*",
				},
			}
			return httptest.NewServer(srv.RegisterRoutes())
		}
		env = &apiTestEnv{readOnly: newServer(false), writable: newServer(true), jobs: js}

		// Seed objects for the read-only tests, in dependency order.
		seed := func(key string, deps []string) {
			t.Helper()
			if _, err := st.Register(types.RegisterArgs{
				Key:       key,
				Object:    map[string]any{"v": float64(1)},
				Init:      true,
				DependsOn: types.Dependencies{DependsOn: deps},
			}); err != nil {
				t.Fatalf("seed register %q: %v", key, err)
			}
		}
		seed("api-child", nil)
		seed("api-parent", []string{"api-child"})
		if err := st.SetNotify("api-child", []string{"api-parent"}); err != nil {
			t.Fatalf("seed notify: %v", err)
		}
		// A second version so the versions endpoint has history to list.
		if _, err := st.Set(types.SetArgs{Key: "api-parent", Object: map[string]any{"v": float64(2)}}); err != nil {
			t.Fatalf("seed set: %v", err)
		}
	})
	return env
}

func get(t *testing.T, base *httptest.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	base.Config.Handler.ServeHTTP(rec, req)
	return rec
}

func doJSON(t *testing.T, base *httptest.Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	base.Config.Handler.ServeHTTP(rec, req)
	return rec
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) apiError {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body does not decode: %v body=%s", err, rec.Body.String())
	}
	return env.Error
}

// --- read-only server: reads work, writes are rejected ---

func TestStoreKeysList(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/keys?prefix=api-")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp keysResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := map[string]bool{}
	for _, k := range resp.Keys {
		found[k] = true
	}
	if !found["api-child"] || !found["api-parent"] {
		t.Fatalf("expected seeded keys in list, got %v", resp.Keys)
	}
}

func TestStoreGetObject(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/objects/api-child")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != `{"v":1}` {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}
}

func TestStoreGetObjectPinnedVersion(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/objects/api-parent/versions")
	if rec.Code != http.StatusOK {
		t.Fatalf("versions: expected 200, got %d", rec.Code)
	}
	var versions versionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions.Versions) < 2 {
		t.Fatalf("expected at least 2 versions, got %v", versions.Versions)
	}

	rec = get(t, e.readOnly, "/store/objects/api-parent?storeId="+versions.Versions[len(versions.Versions)-1])
	if rec.Code != http.StatusOK {
		t.Fatalf("pinned get: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != `{"v":1}` {
		t.Fatalf("expected oldest version payload, got %s", rec.Body.String())
	}

	rec = get(t, e.readOnly, "/store/objects/api-parent?storeId=01J9INVALID0000000000000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bogus storeId: expected 404, got %d", rec.Code)
	}
}

func TestStoreGetMeta(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/objects/api-parent/meta")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var meta types.MetaData
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Key != "api-parent" || !meta.Init || !meta.Fixed {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if len(meta.DependsOn.DependsOn) != 1 || meta.DependsOn.DependsOn[0] != "api-child" {
		t.Fatalf("unexpected deps in meta: %+v", meta.DependsOn)
	}
}

func TestStoreGetDepsAndNotify(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/objects/api-parent/deps")
	if rec.Code != http.StatusOK {
		t.Fatalf("deps: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "api-child") {
		t.Fatalf("expected api-child in deps, got %s", rec.Body.String())
	}

	rec = get(t, e.readOnly, "/store/objects/api-child/notify")
	if rec.Code != http.StatusOK {
		t.Fatalf("notify: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "api-parent") {
		t.Fatalf("expected api-parent in notify list, got %s", rec.Body.String())
	}
}

func TestStoreUnknownAndInvalidKeys(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/store/objects/api-nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ae := decodeErr(t, rec); ae.Code != "not_found" {
		t.Fatalf("expected not_found, got %q", ae.Code)
	}

	rec = get(t, e.readOnly, "/store/objects/bad!key")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if ae := decodeErr(t, rec); ae.Code != "invalid_key" {
		t.Fatalf("expected invalid_key, got %q", ae.Code)
	}
}

func TestStoreWritesDisabled(t *testing.T) {
	e := testEnv(t)
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/store/objects", `{"key":"x","object":{"a":1}}`},
		{http.MethodPut, "/store/objects/api-child", `{"data":{"a":1}}`},
		{http.MethodDelete, "/store/objects/api-child", ``},
		{http.MethodPost, "/store/objects/api-child/publish", `{"data":{"a":1}}`},
		{http.MethodPatch, "/store/objects/api-child/meta", `{"check":true}`},
		{http.MethodPut, "/store/objects/api-parent/deps", `{"keys":["api-child"]}`},
		{http.MethodDelete, "/store/objects/api-parent/deps", `{"keys":["api-child"]}`},
		{http.MethodPut, "/store/objects/api-child/notify", `{"keys":["api-parent"]}`},
		{http.MethodPost, "/job/run-graphs", `{"rootKey":"api-parent"}`},
		{http.MethodPost, "/job/run-graphs/api-parent/exec", ``},
		{http.MethodPost, "/job/notifications", `{"key":"api-parent"}`},
		{http.MethodPut, "/perf/enabled", `{"enabled":true}`},
		{http.MethodPost, "/perf/reset", ``},
	}
	for _, tc := range cases {
		rec := doJSON(t, e.readOnly, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			continue
		}
		if ae := decodeErr(t, rec); ae.Code != "writes_disabled" {
			t.Errorf("%s %s: expected writes_disabled, got %q", tc.method, tc.path, ae.Code)
		}
	}
}

func TestJobDefinitionAndOrder(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/job/definitions/api-parent")
	if rec.Code != http.StatusOK {
		t.Fatalf("definition: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var def definitionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if def.RootKey != "api-parent" || len(def.Vertices) != 2 {
		t.Fatalf("unexpected definition: %+v", def)
	}
	if len(def.Edges) != 1 || def.Edges[0].From != "api-parent" || def.Edges[0].To != "api-child" {
		t.Fatalf("unexpected edges: %+v", def.Edges)
	}

	rec = get(t, e.readOnly, "/job/definitions/api-parent/order?direction=leaves-first")
	if rec.Code != http.StatusOK {
		t.Fatalf("order: expected 200, got %d", rec.Code)
	}
	// Both seeded objects are static maps (Fixed), so nothing is executable.
	if !strings.Contains(rec.Body.String(), `"order"`) {
		t.Fatalf("unexpected order body: %s", rec.Body.String())
	}

	rec = get(t, e.readOnly, "/job/definitions/api-parent/order?direction=sideways")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad direction: expected 400, got %d", rec.Code)
	}

	rec = get(t, e.readOnly, "/job/definitions/api-nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown key: expected 404, got %d", rec.Code)
	}
}

func TestJobRunGraphNotBuilt(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/job/run-graphs/api-child")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestJobStatus(t *testing.T) {
	e := testEnv(t)
	rec := get(t, e.readOnly, "/job/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var st job.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}

func TestPerfEndpointsReadOnly(t *testing.T) {
	e := testEnv(t)
	perf.SetEnabled(true)
	t.Cleanup(func() { perf.SetEnabled(false) })

	rec := get(t, e.readOnly, "/perf/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var stats []perf.Stat
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec = get(t, e.readOnly, "/perf/stats?sort=calls&top=5&name=store")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats with params: expected 200, got %d", rec.Code)
	}
	for _, st := range mustStats(t, rec) {
		if !strings.Contains(st.Name, "store") {
			t.Fatalf("name filter leaked %q", st.Name)
		}
	}

	rec = get(t, e.readOnly, "/perf/stats?sort=nope")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad sort: expected 400, got %d", rec.Code)
	}

	rec = get(t, e.readOnly, "/perf/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("config: expected 200, got %d", rec.Code)
	}
	var settings perf.Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !settings.Enabled {
		t.Fatalf("expected enabled=true after SetEnabled(true)")
	}
}

func mustStats(t *testing.T, rec *httptest.ResponseRecorder) []perf.Stat {
	t.Helper()
	var stats []perf.Stat
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return stats
}

// --- writable server: full CRUD and job flow ---

func TestWriteFlow(t *testing.T) {
	e := testEnv(t)

	rec := doJSON(t, e.writable, http.MethodPost, "/store/objects", `{"key":"w-flow","object":{"v":1},"init":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var meta types.MetaData
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// HTTP-registered objects have no Go update method: Fixed is forced on.
	if !meta.Fixed || !meta.Init {
		t.Fatalf("unexpected meta after register: %+v", meta)
	}

	rec = doJSON(t, e.writable, http.MethodPost, "/store/objects", `{"key":"w-flow","object":{"v":9}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-register: expected 409, got %d", rec.Code)
	}

	rec = doJSON(t, e.writable, http.MethodPut, "/store/objects/w-flow", `{"data":{"v":2}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = get(t, e.writable, "/store/objects/w-flow")
	if strings.TrimSpace(rec.Body.String()) != `{"v":2}` {
		t.Fatalf("get after set: got %s", rec.Body.String())
	}

	rec = get(t, e.writable, "/store/objects/w-flow/versions")
	var versions versionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions.Versions) != 2 {
		t.Fatalf("expected 2 versions, got %v", versions.Versions)
	}

	rec = doJSON(t, e.writable, http.MethodPost, "/store/objects/w-flow/publish", `{"data":{"v":3}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, e.writable, http.MethodPatch, "/store/objects/w-flow/meta", `{"alwaysRun":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch meta: expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !meta.AlwaysRun {
		t.Fatalf("alwaysRun not applied: %+v", meta)
	}

	// fixed is frozen once the object is initialized.
	rec = doJSON(t, e.writable, http.MethodPatch, "/store/objects/w-flow/meta", `{"fixed":false}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("patch fixed on initialized: expected 409, got %d", rec.Code)
	}

	rec = doJSON(t, e.writable, http.MethodDelete, "/store/objects/w-flow", ``)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}

	rec = get(t, e.writable, "/store/objects/w-flow")
	if rec.Code != http.StatusGone {
		t.Fatalf("get deleted: expected 410, got %d", rec.Code)
	}

	// Soft-deleted keys leave the default key list but stay in meta.
	rec = get(t, e.writable, "/store/keys?prefix=w-flow")
	if strings.Contains(rec.Body.String(), "w-flow") {
		t.Fatalf("deleted key still listed: %s", rec.Body.String())
	}
	rec = get(t, e.writable, "/store/keys?prefix=w-flow&includeDeleted=true")
	if !strings.Contains(rec.Body.String(), "w-flow") {
		t.Fatalf("deleted key missing with includeDeleted: %s", rec.Body.String())
	}
	rec = get(t, e.writable, "/store/objects/w-flow/meta")
	if rec.Code != http.StatusOK {
		t.Fatalf("meta of deleted: expected 200, got %d", rec.Code)
	}
}

func TestWriteDepsAndNotify(t *testing.T) {
	e := testEnv(t)
	for _, key := range []string{"w-a", "w-b", "w-c"} {
		rec := doJSON(t, e.writable, http.MethodPost, "/store/objects", `{"key":"`+key+`","object":{"v":0},"init":true}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("register %s: expected 201, got %d body=%s", key, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, e.writable, http.MethodPut, "/store/objects/w-a/deps", `{"keys":["w-b"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "w-b") {
		t.Fatalf("put deps: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, e.writable, http.MethodPost, "/store/objects/w-a/deps", `{"keys":["w-c"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "w-c") {
		t.Fatalf("post deps: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, e.writable, http.MethodDelete, "/store/objects/w-a/deps", `{"keys":["w-c"]}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "w-c") {
		t.Fatalf("delete deps: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown dependency targets are rejected.
	rec = doJSON(t, e.writable, http.MethodPut, "/store/objects/w-a/deps", `{"keys":["w-missing"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("deps with unknown key: expected 400, got %d", rec.Code)
	}

	rec = doJSON(t, e.writable, http.MethodPut, "/store/objects/w-b/notify", `{"keys":["w-a"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "w-a") {
		t.Fatalf("put notify: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, e.writable, http.MethodDelete, "/store/objects/w-b/notify", `{"keys":["w-a"]}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "w-a") {
		t.Fatalf("delete notify: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWriteJobFlow(t *testing.T) {
	e := testEnv(t)

	rec := doJSON(t, e.writable, http.MethodPost, "/job/run-graphs", `{"rootKey":"w-a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run graph: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var rg types.RunGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &rg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rg.RootKey != "w-a" {
		t.Fatalf("unexpected run graph: %+v", rg)
	}

	rec = get(t, e.writable, "/job/run-graphs/w-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("get run graph: expected 200, got %d", rec.Code)
	}

	// All HTTP-registered objects are Fixed, so exec runs nothing but succeeds.
	rec = doJSON(t, e.writable, http.MethodPost, "/job/run-graphs/w-a/exec", ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("exec: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, e.writable, http.MethodPost, "/job/notifications", `{"key":"w-a"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("notify: expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	e.jobs.WaitIdle()

	rec = doJSON(t, e.writable, http.MethodPost, "/job/notifications", `{"key":"w-missing"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("notify unknown: expected 404, got %d", rec.Code)
	}
}

func TestWritePerfControls(t *testing.T) {
	e := testEnv(t)
	t.Cleanup(func() { perf.SetEnabled(false) })

	rec := doJSON(t, e.writable, http.MethodPut, "/perf/enabled", `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !perf.Enabled() {
		t.Fatal("perf.Enabled() is false after PUT /perf/enabled")
	}

	rec = doJSON(t, e.writable, http.MethodPost, "/perf/reset", ``)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: expected 204, got %d", rec.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	srv := &Server{rest: config.RestAPI{EnableWrites: true, MaxBodyBytes: 16}}
	req := httptest.NewRequest(http.MethodPost, "/store/objects",
		strings.NewReader(`{"key":"w-huge","object":{"v":"0123456789"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.registerObjectHandler(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ae := decodeErr(t, rec); ae.Code != "body_too_large" {
		t.Fatalf("expected body_too_large, got %q", ae.Code)
	}
}

func TestContentTypeRejected(t *testing.T) {
	e := testEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/store/objects", strings.NewReader(`{"key":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	e.writable.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d body=%s", rec.Code, rec.Body.String())
	}
}
