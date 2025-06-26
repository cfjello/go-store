package store

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/dynReflect"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

/*
type StoreIntf[T any] interface {
	Register(args types.RegisterArgs) (types.MetaData, error)
	IsRegistered(key string) bool
	Set(args types.SetArgs) (types.MetaData, error)
	Has(key string) bool
	// HasStoreID(storeID string) bool
	UnRegister(key string) bool
	SetMetaData(key string, meta types.MetaData) bool
	GetMetaData(key string) (types.MetaData, error)
	Get(storeID string, key string) (interface{}, error)
	// GetTypedCall(key string, storeID string) (any, error)
	// GetStoreIDs(objType string, jobID string) []string
	// GetStoreID(key string) (string, error)
	// SetJobIdx(adt types.ADT_Job) bool
}
*/

// Store implements a key-value store
type Store struct {
	InitStoreID string
	SoftDel     string
	db          *database.DBService
}

// NewStore creates a new store
func New(dbServ *database.DBService) *Store {
	return &Store{
		InitStoreID: "0000",
		SoftDel:     util.Ulid(),
		db:          dbServ,
	}
}

/*
// Register registers an object in the store
func (s *Store) Register(args types.RegisterArgs) (types.MetaData, error) {

	if args.Key == "" {
		return types.MetaData{}, errors.New("the key cannot be empty")
	}
	if args.Init && args.Object == nil {
		return types.MetaData{}, fmt.Errorf("the combination of init=true and an undefined object is not valid")
	}

	meta := types.MetaData{
		Key:       args.Key,
		Oper:      "reg",
		Init:      args.Init,
		SchemaKey: args.Key,
		Check:     args.Check,
		SoftDel:   s.SoftDel,
	}
	if args.Init {
		typeInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(args.Object))
		meta.TypeInfo = typeInfo
	}
	// Register the key in the store
	if !s.SetMetaData(meta.Key, meta) {
		return types.MetaData{}, fmt.Errorf("cannot register object named: %s", args.Key)
	}
	if meta.Init {
		res := s.db.SetData(args.Key, types.SetArgs{
			Key:       args.Key,
			Object:    args.Object,
			JobID:     "",
			Check:     args.Check,
			SchemaKey: meta.SchemaKey,
		})
		if !res {
			meta.Init = false
			return meta, fmt.Errorf("unable to store object for key %s", args.Key)
		}
		meta.Oper = "reg&set"
		s.SetMetaData(meta.Key, meta)
		return meta, nil
	}
	return meta, nil
}
*/
// IsRegistered checks if a key is registered
func (s *Store) IsRegistered(key string) bool {
	_, err := s.GetMetaData(key, key)
	return err == nil
}

// Set stores an object in the store
func (s *Store) Set(args types.SetArgs) (types.MetaData, error) {

	if args.Object == nil || reflect.ValueOf(args.Object).Kind() != reflect.Map {
		return types.MetaData{}, errors.New("an object must be passed to the store")
	}

	// Generate new storeId, jobId
	storeID := util.Ulid()
	if args.JobID == "" {
		args.JobID = storeID
	}
	if args.SchemaKey == "" {
		args.SchemaKey = args.Key
	}

	var meta types.MetaData

	meta, err := s.GetMetaData(args.Key, args.SchemaKey)
	if err != nil {
		// If the key is not registered, we create a new metadata object
		meta = types.MetaData{
			Key:  args.Key,
			Init: true,
			Oper: "set",
			// StoreID:   storeID,
			// JobID:     args.JobID,
			Check:     args.Check,
			SchemaKey: args.SchemaKey,
			SoftDel:   s.SoftDel,
			TypeInfo:  dynReflect.BuildTypeInfo(reflect.ValueOf(args.Object)),
		}
		// First, set the metadata
		if !s.SetMetaData(meta.Key, meta) {
			return types.MetaData{}, fmt.Errorf("failed to store metadata for %s", meta.Key)
		}
	} else {
		// Validate the object if check is true
		if meta.Check || args.Check {
			// Validation would go here, but we'll skip  validation for now
		}
	}
	// store the object data
	if !s.db.SetData(args.Key, args) {
		return types.MetaData{}, fmt.Errorf("failed to store data for %s", meta.Key)
	}

	return meta, nil
}

// Has is an alias for IsRegistered
func (s *Store) Has(key string) bool {
	return s.IsRegistered(key)
}

// HasStoreID checks if a storeID exists
// func (s *Store) HasStoreID(storeID string) bool {
// 	return s.db.HasData(storeID)
// }

// UnRegister removes a key from the store
func (s *Store) UnRegister(key string) bool {
	meta, err := s.GetMetaData(key, key)
	if err != nil {
		return false
	}
	meta.SoftDel = util.Ulid()
	return s.SetMetaData(key, meta)
}

// SetMetaData sets metadata for a key
func (s *Store) SetMetaData(key string, meta types.MetaData) bool {
	return s.db.SetMeta(key, meta)
}

// GetMetaData gets metadata for a key
func (s *Store) GetMetaData(key string, schemaKey string) (types.MetaData, error) {
	// var meta types.MetaData
	if schemaKey == "" {
		schemaKey = key
	}
	meta, err := s.db.GetMeta(key, schemaKey)
	if err != nil {
		return types.MetaData{}, err
	}
	return meta, nil
}

// Get gets an object from the store
func (s *Store) Get(storeID string, key string) (interface{}, error) {
	if key == "" && storeID == "" {
		return *new(interface{}), errors.New("no \"key\" provided for Get()")
	}
	// Here we lookup the latest storeID from metadata if not provided
	if storeID == "" {
		SID, err := s.db.GetCurrStoreID(key)
		if err != nil {
			return *new(interface{}), errors.New("no \"default storeId\" provided for Get()")
		}
		storeID = SID
	}
	if storeID == "" || storeID == "0000" {
		return *new(interface{}), errors.New("no \"storeId\" provided for getData()")
	}

	objData, err := s.db.GetData(storeID)
	if err != nil {
		return *new(interface{}), fmt.Errorf("failed to fetch data for %s with storeId: %s", key, storeID)
	}
	return objData, nil
}

/*

// GetData gets data for a key
func (s *Store) GetTypedCall(key string, storeID string) (any, error) {
	result := s.GetTyped(s, storeID, key)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}
*/

/*

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
*/
