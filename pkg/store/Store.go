package store

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/dynReflect"

	// "github.com/cfjello/go-store/pkg/job"
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
	// job         *job.JobService
}

// var Store *StoreService

// NewStore creates a new store
func New(dbServ *database.DBService, reset bool) *Store {

	if dbServ == nil {
		dbServ = database.New(reset)
		// defer dbServ.Close() // Ensure the database is closed when the program exits
	}
	if dbServ == nil {
		panic("failed to initialize database service")
	}
	return &Store{
		InitStoreID: "0000",
		SoftDel:     "0000",
		db:          dbServ,
		// job:         job.NewJobService(),
	}
}

func NewStore(reset bool) *Store {
	return New(nil, reset)
}

// Register registers an object in the store
func (s *Store) Register(args types.RegisterArgs) (types.MetaData, error) {

	if args.Key == "" {
		return types.MetaData{}, errors.New("the key cannot be empty")
	}
	if args.Init && args.Object == nil {
		return types.MetaData{}, fmt.Errorf("the combination of init=true and an undefined object is not valid")
	}

	reg := "reg"
	if args.Init {
		reg = "reg&set"
	}
	meta := types.MetaData{
		Key:     args.Key,
		Oper:    reg,
		Init:    args.Init,
		Schema:  args.Key,
		Check:   args.Check,
		SoftDel: s.SoftDel,
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
			Key:    args.Key,
			Object: args.Object,
			JobID:  "",
			Check:  args.Check,
			Schema: meta.Schema,
		})
		if !res {
			meta.Init = false
			return meta, fmt.Errorf("unable to store object for key %s", args.Key)
		}

		return meta, nil
	}
	return meta, nil
}

// IsRegistered checks if a key is registered
func (s *Store) IsRegistered(key string) bool {
	_, err := s.GetMetaData(key, key)
	return err == nil
}

func (s *Store) SetDependsOn(MetaKey string, deps []string, replace bool) error {
	if MetaKey == "" || !s.IsRegistered(MetaKey) {
		return fmt.Errorf("key %s is not registered", MetaKey)
	}

	for _, dep := range deps {
		if !s.IsRegistered(dep) {
			return fmt.Errorf("dependency %s is not registered", dep)
		}
	}
	meta, err := s.GetMetaData(MetaKey, MetaKey)
	if err != nil {
		return fmt.Errorf("failed to get metadata for key %s: %v", MetaKey, err)
	}
	if !replace {
		dependsOn := util.UniqueStrings(append(meta.DependsOn, deps...))
		meta.DependsOn = dependsOn
	} else {
		meta.DependsOn = deps
	}
	if !s.db.UpsertMeta(MetaKey, meta) {
		return fmt.Errorf("failed to update metadata for key %s with dependencies %v", MetaKey, deps)
	}
	return nil
}

func (s *Store) GetDependsOn(MetaKey string) ([]string, error) {
	if MetaKey == "" || !s.IsRegistered(MetaKey) {
		return nil, fmt.Errorf("key %s is not registered", MetaKey)
	}
	meta, err := s.GetMetaData(MetaKey, MetaKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata for key %s: %v", MetaKey, err)
	}
	return meta.DependsOn, nil
}

// Set stores an object in the store
func (s *Store) Set(args types.SetArgs) (types.MetaData, error) {

	if args.Object == nil && args.Key == "" {
		return types.MetaData{}, errors.New("an named object must be passed to the store")
	}

	// Generate new storeId, jobId
	storeID := util.Ulid()
	if args.JobID == "" {
		args.JobID = storeID
	}
	if args.Schema == "" {
		args.Schema = args.Key
	}

	var meta types.MetaData

	meta, err := s.GetMetaData(args.Key, args.Schema)
	if err != nil {
		return types.MetaData{}, fmt.Errorf("object %s is not registered: %v", args.Key, err)
		/*
			// If the key is not registered, we create a new metadata object
			regArgs := types.RegisterArgs{
				Key:       args.Key,
				Object:    args.Object,
				Init:      true,
				Check:     args.Check,
				SchemaKey: args.SchemaKey,
			}
			meta, err = s.Register(regArgs)
			if err != nil {
				return types.MetaData{}, fmt.Errorf("failed to register object %s: %v", args.Key, err)
			}
		*/
	}
	/*
		// Validate the object if check is true-
		if meta.Check || args.Check {
			// If the type info is not set, we build it from the object
			if reflect.DeepEqual(meta.TypeInfo, dynReflect.TypeInfo{}) {
				meta.TypeInfo = dynReflect.BuildTypeInfo(reflect.ValueOf(args.Object))
				meta.Oper = "set"
				meta.Init = true
				meta.Check = args.Check
				meta.SoftDel = s.SoftDel
				s.db.SetMeta(meta.Key, meta)
			} else {
				curObjTypes := dynReflect.BuildTypeInfo(reflect.ValueOf(args.Object))
				if curObjTypes.Type != meta.TypeInfo.Type {
					return types.MetaData{}, fmt.Errorf("object %s does not match the expected type: %s", args.Key, meta.TypeInfo.Type)
				}
			}
		}
	*/
	// store the object data

	if !s.db.SetData(args.Key, args) {
		return types.MetaData{}, fmt.Errorf("failed to store data for %s", meta.Key)
	}
	return meta, nil
}

func (s *Store) Persist(args types.SetArgs) bool {
	args.Persist = true
	return s.db.SetData(args.Key, args)
}

func (s *Store) PersistJob(JobID string) error {
	return s.db.PersistJob(JobID)

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
func (s *Store) GetMetaData(key string, schemaKeys ...string) (types.MetaData, error) {
	schemaKey := key
	if len(schemaKeys) > 0 {
		schemaKey = schemaKeys[0]
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
		return *new(any), errors.New("no \"key\" provided for Get()")
	}
	// Here we lookup the latest storeID from metadata if not provided
	if storeID == "" {
		SID, err := s.db.GetCurrStoreID(key)
		if err != nil {
			return *new(any), errors.New("no \"default storeId\" provided for Get()")
		}
		storeID = SID
	}
	if storeID == "" || storeID == "0000" {
		return *new(any), errors.New("no \"storeId\" provided for getData()")
	}

	objData, err := s.db.GetData(storeID)
	if err != nil {
		return *new(any), fmt.Errorf("failed to fetch data for %s with storeId: %s", key, storeID)
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


func YourFunction(firstParam SomeType, schemaKeys ...string) {
	var schemaKey string
	if len(schemaKeys) > 0 {
		schemaKey = schemaKeys[0]
	}
	// Use schemaKey (will be empty string if not provided)
}

// Usage:
YourFunction(firstParam)              // schemaKey will be ""
YourFunction(firstParam, "mySchema")  // schemaKey will be "mySchema"
*/
