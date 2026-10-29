package types

import (
	"fmt"

	"github.com/cfjello/go-store/pkg/dynReflect"
)

// Dependencies represents the dependencies of a stored object
type Dependencies struct {
	DependsOn []string `json:"depends,omitempty"`
}

// MetaData represents metadata about stored objects
type MetaData struct {
	Key   string `json:"key"`
	Init  bool   `json:"init"`
	Oper  string `json:"oper"`
	Fixed bool   `json:"fixed,omitempty"`
	// StoreID  string              `json:"storeId"`
	// JobID    string              `json:"jobId"`
	Check     bool                `json:"check"`
	SoftDel   string              `json:"deleted,omitempty"`
	ParentKey string              `json:"schema"`
	AlwaysRun bool                `json:"alwaysRun,omitempty"`
	DependsOn Dependencies        `json:"depends,omitempty"`
	TypeInfo  dynReflect.TypeInfo `json:"typeInfo,omitempty"`
}

type JobDbEntry struct {
	JobId string `json:"jobId"`
	// JobName string `json:"job_name"`
	MetaId  string `json:"meta_key"`
	JobJson string `json:"job_json"`
}

// Define a struct to hold vertex information
type JobVertex struct {
	// ID    string
	JobId     string
	Key       string
	NodeName  string
	Level     int
	Fixed     bool
	AlwaysRun bool
}

// RunNode captures the per-vertex data persisted in a stored run graph.
type RunNode struct {
	Key       string   `json:"key"`
	StoreId   string   `json:"storeId"`
	Level     int      `json:"level"`
	Fixed     bool     `json:"fixed,omitempty"`
	AlwaysRun bool     `json:"alwaysRun,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
	IsDirty   bool     `json:"isDirty"`
}

// RunGraph is the serializable snapshot of a job graph that is stored and
// later executed by ExecRunGraph. Order holds the node keys in leaves-first
// execution order.
type RunGraph struct {
	RootKey     string              `json:"rootKey"`
	RootStoreId string              `json:"rootStoreId"`
	Order       []string            `json:"order"`
	Nodes       map[string]*RunNode `json:"nodes"`
}

// CheckMode tells Set whether to validate the cached object: CheckNone (the
// zero value) defers to the existing metadata's Check flag, CheckOn forces
// validation on, and CheckSkip forces it off, regardless of that metadata.
type CheckMode string

const (
	CheckNone CheckMode = "none"
	CheckOn   CheckMode = "check"
	CheckSkip CheckMode = "skip"
)

// Resolve returns whether validation should run: CheckOn forces true, CheckSkip
// forces false, and CheckNone (or an unset "") defers to fallback.
func (m CheckMode) Resolve(fallback bool) bool {
	switch m {
	case CheckOn:
		return true
	case CheckSkip:
		return false
	default:
		return fallback
	}
}

// SetArgs represents arguments for the set operation.
// StoreId is the data row id (a new ULID per version). Key is stored as
// meta_key so GetCurrStoreID(Key) can find the newest StoreId. ParentKey is
// the schema key and is not written to the data table.
type SetArgs struct {
	Key       string    `json:"key"`
	Object    any       `json:"object"`
	StoreId   string    `json:"storeId,omitempty"`
	JobId     string    `json:"jobId,omitempty"`
	Check     CheckMode `json:"check,omitempty"`
	ParentKey string    `json:"schemaKey,omitempty"`
	Publish   bool      `json:"publish,omitempty"`
	Fixed     bool      `json:"fixed,omitempty"`
	AlwaysRun bool      `json:"alwaysRun,omitempty"`
}

// Ptr returns a pointer to v, useful for optional pointer fields.
func Ptr[T any](v T) *T {
	return &v
}

// RegisterArgs represents arguments for the register operation
type RegisterArgs struct {
	Key       string `json:"key"`
	ParentKey string `json:"schemaKey,omitempty"`
	// SchemaDef interface{}  `json:"schema,omitempty"`
	Object    any          `json:"object,omitempty"`
	Init      bool         `json:"init,omitempty"`
	Check     bool         `json:"check,omitempty"`
	Fixed     bool         `json:"fixed,omitempty"`
	AlwaysRun bool         `json:"alwaysRun,omitempty"`
	DependsOn Dependencies `json:"depends,omitempty"`
}

// PublishArgs represents arguments for the publish operation
type PublishArgs struct {
	Key    string      `json:"key"`
	ObjRef interface{} `json:"objRef"`
	JobID  string      `json:"jobId,omitempty"`
}

// ADT_Job represents a job in the store
type ADT_Job struct {
	JobID   string `json:"jobId"`
	StoreID string `json:"storeId"`
}

// ExtError represents an extended error with additional info
type ExtError struct {
	Message string
	Name    string
	Cause   error
	Info    map[string]string
}

func (e *ExtError) Error() string {
	return fmt.Sprintf("%s: %s", e.Name, e.Message)
}
