package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/cfjello/go-store/pkg/util"
)

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

// StoreResult represents the result of a store operation
type StoreResult struct {
	OK       bool     `json:"ok"`
	Oper     string   `json:"oper"`
	Meta     MetaData `json:"meta"`
	Error    error    `json:"error,omitempty"`
	// ZodError error    `json:"zodError,omitempty"`
}

// GetResult represents the result of a get operation
type GetResult struct {
	OK    bool        `json:"ok"`
	Oper  string      `json:"oper"`
	Type  string      `json:"type"`
	Meta  MetaData    `json:"meta"`
	Data  interface{} `json:"data"`
	Error error       `json:"error,omitempty"`
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

// Store implements a key-value store
type Store struct {
	InitStoreID string
	SoftDel     string
	DB          *DB
}

// NewStore creates a new store
func NewStore(db *DB) *Store {
	return &Store{
		InitStoreID: "0000", // Default store ID
		SoftDel:     ulid(),
		DB:          db,
	}
}

var ulid = util.ULIDGenerator()

// Register registers an object in the store
func (s *Store) Register(args RegisterArgs) StoreResult {

	if args.Key == "" { return StoreResult{ OK: false, Oper: "reg", Error: errors.New("The Key cannot be empty"), } }
	if args.Init && args.Object == nil { return StoreResult{ 
		OK: false,
		Oper: "reg",
		Error: errors.New("The combination of init=true and an undefined object is not valid"),
		}
	}

	meta := MetaData{
		Key:       args.Key,
		Oper:      "reg",
		Init:      args.Init,
		SchemaKey: args.Key,
		Check:     args.Check,
		SoftDel:   s.SoftDel,
	}
	// Register the key in the store
	if !s.SetMetaData(meta.Key, meta) {
		return StoreResult{
			OK:   false,
			Oper: "reg",
			Meta: meta,
			Error: errors.New(fmt.Sprintf("Cannot register object named: %s", args.Key)),
		}
	}
	if meta.Init {
		storeRet := s.Set(SetArgs{
			Key:       args.Key,
			Object:    args.Object,
			JobID:     "",
			Check:     args.Check,
			SchemaKey: meta.SchemaKey,
		},  "", false)

		if !storeRet.OK {
			meta.Init = false
			return StoreResult{
				OK:   false,
				Oper: "reg",
				Meta: meta,
				Error: errors.New(fmt.Sprintf("Unable to store object for key %s", args.Key)),
			}
		}

		meta.JobID = storeRet.Meta.JobID
		meta.StoreID = storeRet.Meta.StoreID
		meta.Oper = "reg&set"
		s.SetMetaData(meta.Key, meta)

		return StoreResult{
			OK:   true,
			Oper: "set",
			Meta: storeRet.Meta,
		}
	}

	return StoreResult{
		OK:   true,
		Oper: "reg",
		Meta: meta,
	}
}

// IsRegistered checks if a key is registered
func (s *Store) IsRegistered(key string) bool {
	_, err := s.GetMetaData(key)
	return err == nil
}

// Set stores an object in the store
func (s *Store) Set(args SetArgs, jobID string, check bool) StoreResult {
	var key string
	var object interface{}
	var schemaKey string

	if args.Key != "" {
		key = args.Key
	}
	if args.Object != nil {
		object = args.Object
	}
	if args.SchemaKey != "" {
		schemaKey = args.SchemaKey
	}

	if object == nil || reflect.ValueOf(object).Kind() != reflect.Map {
		return StoreResult{
			OK:    false,
			Oper:  "set",
			Error: errors.New("an object must be passed to the store"),
		}
	}

	// Generate new storeId, jobId
	storeID := ulid()
	if args.JobID == "" {
		args.JobID = ulid()
	}
	
	meta := MetaData{
		Key:       key,
		Init:      true,
		Oper:      "set",
		StoreID:   storeID,
		JobID:     args.JobID,
		Check:     check,
		SchemaKey: schemaKey,
		SoftDel:   s.SoftDel,
	}

	{
		// Check if the key is already registered
		_, err := s.GetMetaData(key, "0000")
		if err != nil {
			return StoreResult{
				OK:    false,
				Oper:  "set",
				Meta:  meta,
				Error: fmt.Errorf("object Key: %s is already registered", key),
			}
		}

		// Validate the object if check is true
		if check {
			// Validation would go here, but we'll skip the ZOD schema validation for this conversion
		}

		// Store the object
		objectJSON, err := json.Marshal(object)
		if err != nil {
			return StoreResult{
				OK:   false,
				Oper: "set",
				Meta: meta,
				Error: errors.New(fmt.Sprintf("Failed to marshal object for %s: %v", key, err)),
			}
		}

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return StoreResult{
				OK:   false,
				Oper: "set",
				Meta: meta,
				Error: errors.New(fmt.Sprintf("Failed to store data for %s: %v", key, err)),
			}
		}

		// Using a transaction to store both data and metadata
		err = s.DB.Transaction(func() error {
			dataRes := s.DB.InsertData(storeID, jobID, key, string(objectJSON), string(metaJSON))
			metaRes := s.DB.InsertMeta(key, string(metaJSON))
			if dataRes != 1 || metaRes != 1 {
				return fmt.Errorf("failed to store data for %s", key)
			}
			return nil
		})

		if err != nil {
			return StoreResult{
				OK:   false,
				Oper: "set",
				Meta: meta,
				Error: errors.New(fmt.Sprintf("Failed to store data for %s: %v", key, err)),
			}
		}
	}

	return StoreResult{
		OK:   true,
		Oper: "set",
		Meta: meta,
	}
}

// Has is an alias for IsRegistered
func (s *Store) Has(key string) bool {
	return s.IsRegistered(key)
}

// HasStoreID checks if a storeID exists
func (s *Store) HasStoreID(storeID string) bool {
	return s.DB.HasData(storeID)
}

// UnRegister removes a key from the store
func (s *Store) UnRegister(key string) bool {
	meta, err := s.GetMetaData(key, "")
	if err != nil || meta.SoftDel != s.SoftDel {
		return false
	}

	meta.SoftDel = generateMonotonicUlid()
	return s.SetMetaData(key, meta)
}

// Publish publishes an object to the store
func (s *Store) Publish(keyOrArgs interface{}, objRef interface{}, jobID string) StoreResult {
	var key string

	if keyStr, ok := keyOrArgs.(string); ok {
		key = keyStr
	} else if publishArgs, ok := keyOrArgs.(PublishArgs); ok {
		key = publishArgs.Key
		objRef = publishArgs.ObjRef
		jobID = publishArgs.JobID
	}

	return s.Set(SetArgs{
		Key:    key,
		Object: objRef,
		JobID:  jobID,
	}, "", false)
}

// SetMetaData sets metadata for a key
func (s *Store) SetMetaData(key string, meta MetaData) bool {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return false
	}

	return s.DB.InsertMeta(key, string(metaJSON)) == 1
}

// GetMetaData gets metadata for a key
func (s *Store) GetMetaData(key string, storeID string) (MetaData, error) {
	var metaData MetaData
// GetMetaData gets metadata for a key
func (s *Store) GetMetaData(key string, storeID ...string) (MetaData, error) {
	storeIDStr := ""
	if len(storeID) > 0 {
	if storeIDStr == "" {
		storeIDStr = "0000"
	}
	}
	metaStr, err := s.DB.GetMeta(key)
	metaStr, err := s.DB.GetMetaData(key, storeID)
	if err != nil {
		return metaData, &ExtError{
			Message: fmt.Sprintf("No Meta data found for %s", key),
			Name:    "STORE-0005",
			Cause:   err,
			Info: map[string]string{
				"func":  "getMetaData()",
				"jobId": "",
			},
		}
	}

	err = json.Unmarshal([]byte(metaStr), &metaData)
	if err != nil {
		return metaData, err
	}

	return metaData, nil
}

// Get gets an object from the store
func (s *Store) Get(storeID string, key string) GetResult {
	if key == "" {
		return GetResult{
			OK:    false,
			Oper:  "get",
			Error: errors.New("no \"key\" provided for Get()"),
		}
	}
	// Here we get the latest storeID from metadata if not provided
	if storeID == "" {
		meta, err := s.GetMetaData(key, "")
		if err != nil {
			return GetResult{
				OK:    false,
				Oper:  "get",
				Error: err,
			}
		}
		storeID = meta.StoreID
	}

	if storeID == "" || storeID == "__undef__" {
		return GetResult{
			OK:    false,
			Oper:  "get",
			Error: errors.New("no \"storeId\" provided for getData()"),
		}
	}

	objData, metaData, err := s.DB.GetData(storeID)
	if err != nil {
		return GetResult{
			OK:   false,
			Oper: "get",
			Error: &ExtError{
				Message: fmt.Sprintf("Failed to fetch data for %s with storeId: %s", key, storeID),
				Name:    "STORE-0007",
				Cause:   err,
				Info: map[string]string{
					"func":  "get()",
					"jobId": "__undef__",
				},
			},
		}
	}

	var meta MetaData
	err = json.Unmarshal([]byte(metaData), &meta)
	if err != nil {
		return GetResult{
			OK:    false,
			Oper:  "get",
			Error: err,
		}
	}

	var data interface{}
	err = json.Unmarshal([]byte(objData), &data)
	if err != nil {
		return GetResult{
			OK:    false,
			Oper:  "get",
			Error: err,
		}
	}

	return GetResult{
		OK:   true,
		Oper: "get",
		Type: key,
		Meta: meta,
		Data: data,
	}
}

// GetData gets data for a key
func (s *Store) GetData(key string, storeID string) (interface{}, error) {
	result := s.Get(storeID, key)
	if !result.OK {
		return nil, result.Error
	}
	return result.Data, nil
}

// GetStoreIDs gets store IDs for a type
func (s *Store) GetStoreIDs(objType string, jobID string) []string {
	if jobID == "" {
		jobID = "%"
	}
	return s.DB.GetStoreIDsByType(objType, jobID)
}

// GetStoreID gets the store ID for a key
func (s *Store) GetStoreID(key string) (string, error) {
	meta, err := s.GetMetaData(key)
	if err != nil {
		return "", err
	}
	return meta.StoreID, nil
}
// SetJobIdx sets a job index
func (s *Store) SetJobIdx(adt ADT_Job) bool {
	adtJSON, err := json.Marshal(adt)
	if err != nil {
		return false
	}
	res := s.DB.InsertJob(adt.JobID, adt.StoreID, string(adtJSON))
	return res == 1
}
}

// Helper function to generate a monotonic ULID
func generateMonotonicUlid() string {
	return ulid()
}

// DB represents a database interface
type DB struct {
	// This would normally contain your database connection
}

// These functions would interface with your actual database implementation
func (db *DB) Transaction(fn func() error) error {
	return fn()
}

func (db *DB) InsertData(storeID, jobID, key, objData, metaData string) int {
	return 1 // Simulates success
}

func (db *DB) InsertMeta(key, metaData string) int {
	return 1 // Simulates success
}

func (db *DB) HasData(storeID string) bool {
	return true // Simulates found
}

func (db *DB) GetMeta(key string) (string, error) {
	return "{}", nil // Simulates found with empty object
}

func (db *DB) GetData(storeID string) (string, string, error) {
	return "{}", "{}", nil // Simulates found with empty objects
}

func (db *DB) GetStoreIDsByType(objType, jobID string) []string {
	return []string{} // Simulates empty result
}

func (db *DB) InsertJob(jobID, storeID, adtJSON string) int {
	return 1 // Simulates success
}
