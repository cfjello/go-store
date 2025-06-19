package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

// OT represents an object of type T
type OT[T any] T

/*
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
*/

// Store implements a key-value store
type Store struct {
	InitStoreID string
	SoftDel     string
	db          *DB
}



// NewStore creates a new store
func NewStore(db *DB) *Store {
	return &Store{
		InitStoreID: "0000", // Default store ID
		SoftDel:     util.Ulid(),
		db:          db,
	}
}

// Register registers an object in the store
func (s *Store) Register(args RegisterArgs) (MetaData, error) {

	if args.Key == "" { return MetaData{}, errors.New("The Key cannot be empty") }
	if args.Init && args.Object == nil { return MetaData{}, errors.New("The combination of init=true and an undefined object is not valid") }

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
		return MetaData{}, errors.New(fmt.Sprintf("Cannot register object named: %s", args.Key))
	}
	if meta.Init {
		storeRet := s.Set(SetArgs{
			Key:       args.Key,
			Object:    args.Object,
			JobID:     "",
			Check:     args.Check,
			SchemaKey: meta.SchemaKey,
		})

		if !storeRet.OK {
			meta.Init = false
			return errors.New(fmt.Sprintf("Unable to store object for key %s", args.Key))
		}


		meta.Oper = "reg&set"
		s.SetMetaData(meta.Key, meta)

		return meta, nil
	}

	return meta, nil
}

// IsRegistered checks if a key is registered
func (s *Store) IsRegistered(key string) bool {
	_, err := s.GetMetaData(key)
	return err == nil
}

// Set stores an object in the store
func (s *Store) Set(args SetArgs) (MetaData, error) {

	if args.Object == nil || reflect.ValueOf(args.Object).Kind() != reflect.Map {
		return MetaData{}, errors.New("an object must be passed to the store")
	}

	// Generate new storeId, jobId
	storeID := util.Ulid()
	if args.JobID == "" { args.JobID = storeID}
	
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
		_, err := s.GetMetaData(args.Key); if err != nil { return MetaData{}, errors.New(fmt.Sprintf("object Key: %s is already registered", args.Key))}

		// Validate the object if check is true
		if check {
			// Validation would go here, but we'll skip the ZOD schema validation for this conversion
		}

		// Store the object
		objectJSON, err := json.Marshal(args.Object);if err != nil { return MetaData{}, err}

		metaJSON, err := json.Marshal(meta); if err != nil { return MetaData{}, err}

		// Using a transaction to store both data and metadata
		err = s.db.Transaction(func() error {
			dataRes := s.db.InsertData(storeID, jobID, key, string(objectJSON), string(metaJSON))
			metaRes := s.db.InsertMeta(key, string(metaJSON))
			if dataRes != 1 || metaRes != 1 {
				return fmt.Errorf("failed to store data for %s", key)
			}
			return nil
		})

		if err != nil {
			return errors.New(fmt.Sprintf("Failed to store data for %s: %v", key, err))
		}
	}
	return  meta, nil
}

// Has is an alias for IsRegistered
func (s *Store) Has(key string) bool {
	return s.IsRegistered(key)
}

// HasStoreID checks if a storeID exists
func (s *Store) HasStoreID(storeID string) bool {
	return s.db.HasData(storeID)
}

// UnRegister removes a key from the store
func (s *Store) UnRegister(key string) bool {
	meta, err := s.GetMetaData(key, "")
	if err != nil || meta.SoftDel != s.SoftDel {
		return false
	}
	meta.SoftDel = util.Ulid()
	return s.SetMetaData(key, meta)
}


// SetMetaData sets metadata for a key
func (s *Store) SetMetaData(key string, meta MetaData) bool {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return false
	}
	return s.db.InsertMeta(key, string(metaJSON)) == 1
}

// GetMetaData gets metadata for a key
func (s *Store) GetMetaData(key string) (MetaData, error) {
	metaStr, err := s.db.GetMeta(key);
	if err != nil {
		return MetaData{}, err
	}
	var metaData MetaData
	err = json.Unmarshal([]byte(metaStr), &metaData); 
	if err != nil {
		return MetaData{}, err
	}
	return metaData, nil
}

// Get gets an object from the store
func (s *Store) Get(storeID string, key string) (OT, error) {
	if key == "" && storeID == "" { return *new(OT), errors.New("No \"key\" provided for Get()")}
	// Here we lookup the latest storeID from metadata if not provided
	if storeID == "" {
		meta, err := s.GetMetaData(key)
		if err != nil {
			return *new(OT), errors.New("No \"default storeId\" provided for Get()")
		}
		storeID = meta.StoreID
	}
	if storeID == "" || storeID == "0000" {return *new(OT), errors.New("No \"storeId\" provided for getData()")}

	objData, err := s.db.GetData(storeID) 
	if err != nil {
		return *new(OT), errors.New(fmt.Sprintf("Failed to fetch data for %s with storeId: %s", key, storeID))
	}
	
	var data OT
	err = json.Unmarshal([]byte(objData), &data)
	if err != nil {
		return *new(OT), errors.New(fmt.Sprintf("Failed to unmarshal data for %s with storeId: %s", key, storeID))
	}

	return data, nil
}

// GetData gets data for a key
func (s *Store) GetTypedCall(key string, storeID string) (any, error) {
	result := s.GetTyped(s, storeID, key)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}*/


// GetStoreIDs gets store IDs for a type
func (s *Store) GetStoreIDs(objType string, jobID string) []string {
	if jobID == "" {
		jobID = "%"
	}
	return s.db.GetStoreIDsByType(objType, jobID)
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
	res := s.db.InsertJob(adt.JobID, adt.StoreID, string(adtJSON))
	return res == 1
}

