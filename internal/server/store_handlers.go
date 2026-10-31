package server

import (
	"encoding/json"
	"net/http"

	"github.com/cfjello/go-store/pkg/types"
)

// registerRequest is the POST /store/objects body. It mirrors
// types.RegisterArgs with the object as a raw JSON payload.
type registerRequest struct {
	Key       string          `json:"key"`
	SchemaKey string          `json:"schemaKey"`
	Object    json.RawMessage `json:"object"`
	Init      bool            `json:"init"`
	Check     bool            `json:"check"`
	Fixed     bool            `json:"fixed"`
	AlwaysRun bool            `json:"alwaysRun"`
	Depends   []string        `json:"depends"`
	NotifyTo  []string        `json:"notifyTo"`
}

// setRequest is the PUT /store/objects/{key} body.
type setRequest struct {
	Data      json.RawMessage `json:"data"`
	JobID     string          `json:"jobId"`
	Check     types.CheckMode `json:"check"`
	Fixed     *bool           `json:"fixed"`
	AlwaysRun *bool           `json:"alwaysRun"`
}

// publishRequest is the POST /store/objects/{key}/publish body.
type publishRequest struct {
	Data  json.RawMessage `json:"data"`
	JobID string          `json:"jobId"`
}

// patchMetaRequest is the PATCH /store/objects/{key}/meta body; nil fields
// are left unchanged.
type patchMetaRequest struct {
	Check     *bool `json:"check"`
	Fixed     *bool `json:"fixed"`
	AlwaysRun *bool `json:"alwaysRun"`
}

// keyListRequest is the shared deps/notify mutation body: Keys for
// set/add/remove, Merge (deps only) to union another object's list.
type keyListRequest struct {
	Keys  []string `json:"keys"`
	Merge []string `json:"merge"`
}

type keysResponse struct {
	Keys       []string `json:"keys"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type versionsResponse struct {
	Versions   []string `json:"versions"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// listKeysHandler serves GET /store/keys.
func (s *Server) listKeysHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, cursor := pageParams(r)
	includeDeleted := q.Get("includeDeleted") == "true"
	// Fetch one extra row to decide whether a next page exists.
	keys, err := s.db.ListKeys(q.Get("prefix"), includeDeleted, limit+1, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list keys")
		return
	}
	resp := keysResponse{Keys: keys}
	if len(keys) > limit {
		resp.Keys = keys[:limit]
		resp.NextCursor = keys[limit-1]
	}
	writeJSON(w, http.StatusOK, resp)
}

// registerObjectHandler serves POST /store/objects. HTTP-registered objects
// are plain JSON: they carry no Go update method (Register forces them Fixed)
// and no struct tags, so Check is forced off; type-shape checking still
// applies to their writes when requested per call.
func (s *Server) registerObjectHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	var req registerRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if !keyRe.MatchString(req.Key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key must match [A-Za-z0-9._~-]{1,128}")
		return
	}
	if s.store.IsRegistered(req.Key) {
		writeError(w, http.StatusConflict, "already_registered", "an object with this key is already registered")
		return
	}
	var obj any
	if len(req.Object) > 0 {
		if err := json.Unmarshal(req.Object, &obj); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "object is not valid JSON")
			return
		}
	}
	meta, err := s.store.Register(types.RegisterArgs{
		Key:       req.Key,
		ParentKey: req.SchemaKey,
		Object:    obj,
		Init:      req.Init,
		Check:     false, // struct-tag validation requires Go structs; see above
		Fixed:     req.Fixed,
		AlwaysRun: req.AlwaysRun,
		DependsOn: types.Dependencies{DependsOn: req.Depends},
		NotifyTo:  req.NotifyTo,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

// getObjectHandler serves GET /store/objects/{key}?storeId=.
func (s *Server) getObjectHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	meta, err := s.store.GetMeta(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	if isDeleted(meta.SoftDel) {
		writeError(w, http.StatusGone, "deleted", "object is soft-deleted")
		return
	}
	storeID := r.URL.Query().Get("storeId")
	if storeID != "" && !idRe.MatchString(storeID) {
		writeError(w, http.StatusBadRequest, "invalid_store_id", "storeId must be an alphanumeric id of at most 32 characters")
		return
	}
	if storeID == "" && !meta.Init {
		writeError(w, http.StatusNotFound, "not_initialized", "object is registered but has no stored data")
		return
	}
	var raw json.RawMessage
	if _, err := s.store.Get(storeID, key, &raw); err != nil {
		storeError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, raw)
}

// setObjectHandler serves PUT /store/objects/{key}.
func (s *Server) setObjectHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	var req setRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	switch req.Check {
	case "", types.CheckNone, types.CheckOn, types.CheckSkip:
	default:
		writeError(w, http.StatusBadRequest, "invalid_check", "check must be one of none, check, skip")
		return
	}
	var data any
	if len(req.Data) > 0 {
		if err := json.Unmarshal(req.Data, &data); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "data is not valid JSON")
			return
		}
	}
	meta, err := s.store.Set(types.SetArgs{
		Key:       key,
		Object:    data,
		JobId:     req.JobID,
		Check:     req.Check,
		Fixed:     req.Fixed,
		AlwaysRun: req.AlwaysRun,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// publishObjectHandler serves POST /store/objects/{key}/publish.
func (s *Server) publishObjectHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	var req publishRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if !s.store.IsRegistered(key) {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	var data any
	if len(req.Data) > 0 {
		if err := json.Unmarshal(req.Data, &data); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "data is not valid JSON")
			return
		}
	}
	if !s.store.Publish(types.SetArgs{Key: key, Object: data, JobId: req.JobID}) {
		writeError(w, http.StatusInternalServerError, "internal", "the publish operation failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"published": key})
}

// deleteObjectHandler serves DELETE /store/objects/{key}: a soft delete.
// Historical versions remain fetchable by storeId.
func (s *Server) deleteObjectHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	if !s.store.IsRegistered(key) {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	if !s.store.UnRegister(key) {
		writeError(w, http.StatusInternalServerError, "internal", "the delete operation failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listVersionsHandler serves GET /store/objects/{key}/versions.
func (s *Server) listVersionsHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	if !s.store.IsRegistered(key) {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	limit, cursor := pageParams(r)
	ids, err := s.db.ListVersions(key, limit+1, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list versions")
		return
	}
	resp := versionsResponse{Versions: ids}
	if len(ids) > limit {
		resp.Versions = ids[:limit]
		resp.NextCursor = ids[limit-1]
	}
	writeJSON(w, http.StatusOK, resp)
}

// getMetaHandler serves GET /store/objects/{key}/meta.
func (s *Server) getMetaHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	meta, err := s.store.GetMeta(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// patchMetaHandler serves PATCH /store/objects/{key}/meta. Only the mutable
// flags (check, alwaysRun, and fixed while uninitialized) can change.
func (s *Server) patchMetaHandler(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	var req patchMetaRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	meta, err := s.store.GetMeta(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "object is not registered")
		return
	}
	if req.Fixed != nil && *req.Fixed != meta.Fixed {
		if meta.Init {
			writeError(w, http.StatusConflict, "fixed_immutable", "cannot change fixed on an initialized object")
			return
		}
		meta.Fixed = *req.Fixed
	}
	if req.Check != nil {
		meta.Check = *req.Check
	}
	if req.AlwaysRun != nil {
		meta.AlwaysRun = *req.AlwaysRun
	}
	if !s.store.SetMeta(key, meta) {
		writeError(w, http.StatusInternalServerError, "internal", "failed to update metadata")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// getDepsHandler serves GET /store/objects/{key}/deps.
func (s *Server) getDepsHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	deps, err := s.store.GetDeps(key)
	if err != nil {
		if !s.store.IsRegistered(key) {
			writeError(w, http.StatusNotFound, "not_found", "object is not registered")
			return
		}
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"keys": deps})
}

// putDepsHandler serves PUT /store/objects/{key}/deps (replace).
func (s *Server) putDepsHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateDeps(w, r, func(key string, req keyListRequest) error {
		return s.store.SetDeps(key, req.Keys)
	})
}

// postDepsHandler serves POST /store/objects/{key}/deps (add or merge).
func (s *Server) postDepsHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateDeps(w, r, func(key string, req keyListRequest) error {
		if len(req.Merge) > 0 {
			return s.store.MergeDeps(key, req.Merge)
		}
		return s.store.AddDeps(key, req.Keys...)
	})
}

// deleteDepsHandler serves DELETE /store/objects/{key}/deps.
func (s *Server) deleteDepsHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateDeps(w, r, func(key string, req keyListRequest) error {
		return s.store.RemoveDeps(key, req.Keys...)
	})
}

func (s *Server) mutateDeps(w http.ResponseWriter, r *http.Request, mutate func(string, keyListRequest) error) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	var req keyListRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if err := mutate(key, req); err != nil {
		storeError(w, err)
		return
	}
	s.getDepsHandler(w, r)
}

// getNotifyHandler serves GET /store/objects/{key}/notify.
func (s *Server) getNotifyHandler(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	targets, err := s.store.GetNotify(key)
	if err != nil {
		if !s.store.IsRegistered(key) {
			writeError(w, http.StatusNotFound, "not_found", "object is not registered")
			return
		}
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"keys": targets})
}

// putNotifyHandler serves PUT /store/objects/{key}/notify (replace).
func (s *Server) putNotifyHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateNotify(w, r, func(key string, req keyListRequest) error {
		return s.store.SetNotify(key, req.Keys)
	})
}

// postNotifyHandler serves POST /store/objects/{key}/notify (add).
func (s *Server) postNotifyHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateNotify(w, r, func(key string, req keyListRequest) error {
		return s.store.AddNotify(key, req.Keys...)
	})
}

// deleteNotifyHandler serves DELETE /store/objects/{key}/notify.
func (s *Server) deleteNotifyHandler(w http.ResponseWriter, r *http.Request) {
	s.mutateNotify(w, r, func(key string, req keyListRequest) error {
		return s.store.RemoveNotify(key, req.Keys...)
	})
}

func (s *Server) mutateNotify(w http.ResponseWriter, r *http.Request, mutate func(string, keyListRequest) error) {
	if !s.requireWrites(w) {
		return
	}
	key, ok := pathKey(w, r)
	if !ok {
		return
	}
	var req keyListRequest
	if write := s.decodeJSONBody(w, r, &req); write != nil {
		write()
		return
	}
	if err := mutate(key, req); err != nil {
		storeError(w, err)
		return
	}
	s.getNotifyHandler(w, r)
}
