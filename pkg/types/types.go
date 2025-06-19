package types

import "fmt"

// MetaData represents metadata about stored objects
type MetaData struct {
	Key       string `json:"key"`
	Init      bool   `json:"init"`
	Oper      string `json:"oper"`
	StoreID   string `json:"storeId"`
	JobID     string `json:"jobId"`
	Check     bool   `json:"check"`
	SchemaKey string `json:"schemaKey"`
	SoftDel   string `json:"deleted,omitempty"`
}

// SetArgs represents arguments for the set operation
type SetArgs struct {
	Key       string      `json:"key"`
	Object    interface{} `json:"object"`
	JobID     string      `json:"jobId,omitempty"`
	Check     bool        `json:"check,omitempty"`
	SchemaKey string      `json:"schemaKey,omitempty"`
}

// RegisterArgs represents arguments for the register operation
type RegisterArgs struct {
	Key    string      `json:"key"`
	Schema interface{} `json:"schema,omitempty"`
	Object interface{} `json:"object,omitempty"`
	Init   bool        `json:"init,omitempty"`
	Check  bool        `json:"check,omitempty"`
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
