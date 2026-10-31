package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// keyRe is the URL-safe store key grammar. Restricting keys keeps them
// unambiguous in route wildcards and prevents log forgery via control
// characters. Store IDs and job IDs (ULIDs) are validated by idRe.
var (
	keyRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)
	idRe  = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
)

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

// apiError is the single error shape returned by every endpoint.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// writeJSON marshals v and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to encode response")
		return
	}
	writeRawJSON(w, status, body)
}

// writeRawJSON writes an already-encoded JSON document.
func writeRawJSON(w http.ResponseWriter, status int, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(raw); err != nil {
		fmt.Printf("Failed to write response: %v\n", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// decodeJSONBody decodes a JSON request body into dst, enforcing the
// configured size cap and, when a Content-Type is sent, requiring JSON.
// The returned write function reports the failure; it is nil on success.
func (s *Server) decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) (write func()) {
	ct := r.Header.Get("Content-Type")
	if ct != "" {
		if base, _, _ := strings.Cut(ct, ";"); strings.TrimSpace(base) != "application/json" {
			return func() {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
			}
		}
	}
	limit := s.rest.MaxBodyBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return func() {
				writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("request body exceeds %d bytes", limit))
			}
		}
		if errors.Is(err, io.EOF) {
			return func() { writeError(w, http.StatusBadRequest, "invalid_body", "request body is empty") }
		}
		return func() { writeError(w, http.StatusBadRequest, "invalid_body", "request body is not valid JSON") }
	}
	return nil
}

// requireWrites reports whether write operations are enabled, writing a 403
// writes_disabled error and returning false when they are not. Every mutating
// handler calls this first.
func (s *Server) requireWrites(w http.ResponseWriter) bool {
	if s.rest.EnableWrites {
		return true
	}
	writeError(w, http.StatusForbidden, "writes_disabled", "write operations are disabled by server configuration")
	return false
}

// pathKey extracts and validates the {key} route wildcard.
func pathKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.PathValue("key")
	if !keyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key must match [A-Za-z0-9._~-]{1,128}")
		return "", false
	}
	return key, true
}

// pageParams reads limit and cursor query parameters. limit defaults to
// defaultPageLimit and is capped at maxPageLimit.
func pageParams(r *http.Request) (limit int, cursor string) {
	limit = defaultPageLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, maxPageLimit)
		}
	}
	cursor = r.URL.Query().Get("cursor")
	if cursor != "" && !keyRe.MatchString(cursor) && !idRe.MatchString(cursor) {
		// An out-of-grammar cursor can never match a row; treat as absent.
		cursor = ""
	}
	return limit, cursor
}

// storeError maps store/job layer errors to HTTP status codes. Store error
// messages carry only client-supplied keys, never SQL or filesystem paths,
// so they are safe to return. Order matters: the most specific markers first.
func storeError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, sql.ErrNoRows),
		strings.Contains(msg, "is not registered"),
		strings.Contains(msg, "no stored job graph"),
		strings.Contains(msg, "no cached object"),
		// Store.Get wraps a missing data row with %v, losing sql.ErrNoRows.
		strings.Contains(msg, "failed to fetch data"):
		writeError(w, http.StatusNotFound, "not_found", msg)
	case strings.Contains(msg, "not a registered object"),
		strings.Contains(msg, "cannot be empty"),
		strings.Contains(msg, "must be a struct"),
		strings.Contains(msg, "validation failed"),
		strings.Contains(msg, "type mismatch"),
		strings.Contains(msg, "no \"storeId\""),
		strings.Contains(msg, "no \"default storeId\""):
		writeError(w, http.StatusBadRequest, "invalid_request", msg)
	case strings.Contains(msg, "dependency cycle"),
		strings.Contains(msg, "cannot change Fixed"),
		strings.Contains(msg, "already exists"):
		writeError(w, http.StatusConflict, "conflict", msg)
	default:
		writeError(w, http.StatusInternalServerError, "internal", "the operation failed")
	}
}

// isDeleted reports whether the metadata marks the object as soft-deleted
// (SoftDel starts as the "0000" sentinel and becomes a ULID on delete).
func isDeleted(softDel string) bool {
	return softDel != "" && softDel != "0000"
}
