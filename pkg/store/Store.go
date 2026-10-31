package store

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/dynReflect"

	// "github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/config"
	"github.com/cfjello/go-store/pkg/perf"
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
	InitStoreId string
	SoftDel     string
	DB          *database.DBService
	// job         *job.JobService

	objectCache // caches references to existing registered objects, keyed by store key

	dataName  string
	updMethod string

	// notifier receives change notifications after successful Set/Publish
	// calls. Set once during setup via SetNotifier (nil-safe: no notifier
	// means no notifications).
	notifier Notifier
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
		InitStoreId: "0000",
		SoftDel:     "0000",
		DB:          dbServ,
		dataName:    config.DefaultConfig().Names.DataName,
		updMethod:   config.DefaultConfig().Names.UpdMethod,
		objectCache: objectCache{cache: make(map[string]any)},
		// job:         job.NewJobService(),
	}
}

var (
	storeInstance *Store
	storeOnce     sync.Once
)

// NewStoreService returns the process-wide singleton Store, creating it on the
// first call. reset is only honored on that first call; later calls return the
// existing instance regardless of reset, mirroring database.New's reuse behavior.
func NewStoreService(reset bool) *Store {
	storeOnce.Do(func() {
		storeInstance = New(nil, reset)
	})
	return storeInstance
}

// UpdMethod returns the name of the no-argument method invoked on a cached
// object when a run-graph node is dirty.
func (s *Store) UpdMethod() string {
	return s.updMethod
}

// Register registers an object in the store
func (s *Store) Register(args types.RegisterArgs) (types.MetaData, error) {
	defer perf.Track("store.Register")()

	if args.Key == "" {
		return types.MetaData{}, errors.New("the key cannot be empty")
	}

	if args.Object == nil {
		return types.MetaData{}, fmt.Errorf("the address of the object is not valid")
	}
	// No update method means a standalone static data object: jobs never
	// recompute it, so Fixed is forced on regardless of the caller's flag.
	if !s.hasUpdMethod(args.Object) {
		args.Fixed = true
	} else if !s.hasDataField(args.Object) {
		return types.MetaData{}, fmt.Errorf("object %q implements %s but has no %q data field", args.Key, s.updMethod, s.dataName)
	}
	// Cache a reference to the registered object for later retrieval by key
	s.SetCache(args.Key, args.Object)

	reg := "reg"
	if args.Init {
		reg = "reg&set"
	}
	parentKey := args.Key
	if args.ParentKey != "" {
		parentKey = args.ParentKey
	}

	meta := types.MetaData{
		Key:       args.Key,
		Oper:      reg,
		Init:      args.Init,
		Fixed:     args.Fixed,
		ParentKey: parentKey,
		Check:     args.Check,
		SoftDel:   s.SoftDel,
		AlwaysRun: args.AlwaysRun,
	}

	// Resolve the dependencies for the metadata: when the parent key equals the
	// key, use the dependencies from the register arguments; when it differs,
	// merge the parent's dependencies with the ones provided. Default to an
	// empty list when there are no dependencies. The notify list resolves the
	// same way: a parent's notify list propagates to objects registered under it.
	var dependsOn types.Dependencies
	var notifyTo types.NotifyList
	if parentKey == args.Key {
		dependsOn.SetDeps(args.DependsOn.DependsOn)
		notifyTo.Set(args.NotifyTo)
	} else {
		parentMeta, err := s.GetMeta(parentKey)
		if err != nil {
			return types.MetaData{}, fmt.Errorf("failed to get metadata for parent key %q: %v", parentKey, err)
		}
		dependsOn.SetDeps(parentMeta.DependsOn.GetDeps())
		dependsOn.MergeDeps(args.DependsOn)
		notifyTo.Set(parentMeta.NotifyTo)
		notifyTo.Merge(args.NotifyTo)
	}
	if len(dependsOn.DependsOn) == 0 {
		dependsOn.DependsOn = []string{}
	}
	meta.DependsOn = dependsOn
	meta.NotifyTo = notifyTo

	// TypeInfo is the shape of the persisted payload: the dataName field when
	// present, otherwise the whole object (static data with no data field and
	// no update method). Checked Set compares the write payload against this.
	meta.TypeInfo = dynReflect.BuildTypeInfo(reflect.ValueOf(s.storedPayload(args.Object)))
	// Register the key in the store
	if !s.SetMeta(meta.Key, meta) {
		return types.MetaData{}, fmt.Errorf("failed to register object named: %s", args.Key)
	}
	if meta.Init {
		checkMode := types.CheckSkip
		if args.Check {
			checkMode = types.CheckOn
		}
		res := s.DB.SetData(args.Key, types.SetArgs{
			Key:       args.Key,
			Object:    s.storedPayload(args.Object),
			JobId:     "",
			Check:     checkMode,
			ParentKey: meta.ParentKey,
		})
		if !res {
			meta.Init = false
			return meta, fmt.Errorf("failed to store object for key %s", args.Key)
		}

		return meta, nil
	}
	return meta, nil
}

// IsRegistered checks if a key is registered
func (s *Store) IsRegistered(key string) bool {
	_, err := s.GetMeta(key, key)
	return err == nil
}

// Set stores an object in the store
func (s *Store) Set(args types.SetArgs) (types.MetaData, error) {
	defer perf.Track("store.Set")()

	if args.Object == nil || args.Key == "" {
		return types.MetaData{}, errors.New("an named object must be passed to the store")
	}

	// Each Set is a new data version. StoreId is data_id; Key is meta_key so
	// GetCurrStoreID(Key) returns this id. JobId may be reused across versions.
	storeID := util.Ulid()
	args.StoreId = storeID
	if args.JobId == "" {
		args.JobId = storeID
	}

	if args.ParentKey == "" {
		args.ParentKey = args.Key
	}

	var meta types.MetaData
	meta, err := s.GetMeta(args.Key, args.ParentKey)
	if err != nil {
		return types.MetaData{}, fmt.Errorf("object %s is not registered: %v", args.Key, err)
	}

	// Allow a Set to flip the Fixed flag on the existing metadata only when
	// explicitly requested (non-nil), and only before the object has been
	// initialized (meta.Init=true freezes Fixed).
	if args.Fixed != nil && *args.Fixed != meta.Fixed {
		if meta.Init {
			return types.MetaData{}, fmt.Errorf("cannot change Fixed on initialized object %s", meta.Key)
		}
		meta.Fixed = *args.Fixed
		s.SetMeta(meta.Key, meta)
	}
	// Allow a Set to update the AlwaysRun flag only when explicitly requested
	// (non-nil); an unspecified flag leaves the registered value intact.
	if args.AlwaysRun != nil && *args.AlwaysRun != meta.AlwaysRun {
		meta.AlwaysRun = *args.AlwaysRun
		s.SetMeta(meta.Key, meta)
	}

	// args.Check left unspecified (CheckNone) defers to the existing metadata's Check flag
	check := args.Check.Resolve(meta.Check)
	payload := s.storedPayload(args.Object)
	if check {
		if err := s.checkPayloadType(meta.Key, meta.TypeInfo, payload); err != nil {
			return types.MetaData{}, err
		}
		if err := s.ValidateCached(meta.Key); err != nil {
			return types.MetaData{}, err
		}
	}
	// store the object data (the data field when present, else the whole object)
	args.Object = payload
	if !s.DB.SetData(args.Key, args) {
		return types.MetaData{}, fmt.Errorf("failed to store data for %s", meta.Key)
	}
	// The write succeeded: notify every object named in the notify list.
	s.notifyChange(meta)
	return meta, nil
}

func (s *Store) Publish(args types.SetArgs) bool {
	defer perf.Track("store.Publish")()
	args.Publish = true
	args.Object = s.storedPayload(args.Object)
	if !s.DB.SetData(args.Key, args) {
		return false
	}
	// The write succeeded: notify every object named in the notify list. A
	// metadata lookup failure only skips the notification, never the publish.
	if meta, err := s.GetMeta(args.Key); err == nil {
		s.notifyChange(meta)
	}
	return true
}

// IsPublished checks if a key has been published
func (s *Store) IsPublished(key string) bool {
	meta, err := s.GetMeta(key, key)
	if err != nil {
		return false
	}
	return meta.Init
}

// Has is an alias for IsRegistered
func (s *Store) Has(key string) bool {
	return s.IsRegistered(key)
}

// UnRegister removes a key from the store
func (s *Store) UnRegister(key string) bool {
	meta, err := s.GetMeta(key, key)
	if err != nil {
		return false
	}
	meta.SoftDel = util.Ulid()
	return s.SetMeta(key, meta)
}

// SetMeta sets metadata for a key
func (s *Store) SetMeta(key string, meta types.MetaData) bool {
	defer perf.Track("store.SetMeta")()
	return s.DB.SetMeta(key, meta)
}

// GetMeta gets metadata for a key
func (s *Store) GetMeta(key string, schemaKeys ...string) (types.MetaData, error) {
	defer perf.Track("store.GetMeta")()
	schemaKey := key
	if len(schemaKeys) > 0 {
		schemaKey = schemaKeys[0]
	}
	meta, err := s.DB.GetMeta(key, schemaKey)
	if err != nil {
		return types.MetaData{}, err
	}
	return meta, nil
}

// GetCurrStoreID returns the current (newest) store ID registered for a key.
func (s *Store) GetCurrStoreID(key string) (string, error) {
	return s.DB.GetCurrStoreID(key)
}

// withMetaDeps validates the dependency keys, fetches the metadata for key, applies
// mutate to its Dependencies, and persists the result. The isValid callback used for
// dependency registration checks is s.IsRegistered.
func (s *Store) withMetaDeps(key string, deps []string, mutate func(*types.Dependencies)) error {
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	if err := types.ValidateDeps(key, s.IsRegistered, deps...); err != nil {
		return err
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	mutate(&meta.DependsOn)
	if !s.SetMeta(key, meta) {
		return fmt.Errorf("failed to update dependencies for %q", key)
	}
	return nil
}

// SetDeps replaces the dependency list of the object identified by key with keys
func (s *Store) SetDeps(key string, keys []string) error {
	return s.withMetaDeps(key, keys, func(d *types.Dependencies) {
		d.SetDeps(keys)
	})
}

// GetDeps returns the dependency list of the object identified by key
func (s *Store) GetDeps(key string) ([]string, error) {
	if key == "" {
		return nil, errors.New("the key cannot be empty")
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return nil, fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	return meta.DependsOn.GetDeps(), nil
}

// AddDeps appends keys to the dependency list of the object identified by key
func (s *Store) AddDeps(key string, keys ...string) error {
	return s.withMetaDeps(key, keys, func(d *types.Dependencies) {
		d.AddDeps(keys...)
	})
}

// RemoveDeps removes keys from the dependency list of the object identified by key
func (s *Store) RemoveDeps(key string, keys ...string) error {
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	meta.DependsOn.RemoveDeps(keys...)
	if !s.SetMeta(key, meta) {
		return fmt.Errorf("failed to update dependencies for %q", key)
	}
	return nil
}

// MergeDeps unions the dependency lists of srcKeys into the object identified by key
func (s *Store) MergeDeps(key string, srcKeys []string) error {
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	if err := types.ValidateDeps(key, s.IsRegistered, srcKeys...); err != nil {
		return err
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	for _, src := range srcKeys {
		srcMeta, err := s.GetMeta(src)
		if err != nil {
			return fmt.Errorf("cannot get metadata for dependency %q: %v", src, err)
		}
		meta.DependsOn.MergeDeps(srcMeta.DependsOn)
	}
	if !s.SetMeta(key, meta) {
		return fmt.Errorf("failed to update dependencies for %q", key)
	}
	return nil
}

// Get gets an object from the store
func (s *Store) Get(storeID string, key string, userObj any) (any, error) {
	defer perf.Track("store.Get")()
	if key == "" && storeID == "" {
		return *new(any), errors.New("no \"key\" provided for Get()")
	}
	// Here we lookup the latest storeID from metadata if not provided
	if storeID == "" {
		SID, err := s.DB.GetCurrStoreID(key)
		if err != nil {
			return *new(any), errors.New("no \"default storeId\" provided for Get()")
		}
		storeID = SID
	}
	if storeID == "" || storeID == "0000" {
		return *new(any), errors.New("no \"storeId\" provided for getData()")
	}

	// The stored JSON is the data field when present; otherwise the whole
	// object (standalone static data with no updMethod / no data field).
	dest := s.payloadDest(userObj)
	objData, err := s.DB.GetData(storeID, dest)
	if err != nil {
		return *new(any), fmt.Errorf("failed to fetch data for %s with storeId: %s: %v", key, storeID, err)
	}
	return objData, nil
}

// hasUpdMethod reports whether obj implements the store's no-argument update method.
func (s *Store) hasUpdMethod(obj any) bool {
	if obj == nil || s.updMethod == "" {
		return false
	}
	m := reflect.ValueOf(obj).MethodByName(s.updMethod)
	return m.IsValid() && m.Type().NumIn() == 0
}

// storedPayload is the value persisted for obj: the field named dataName when
// present, otherwise the object itself (standalone static data).
func (s *Store) storedPayload(obj any) any {
	if v, ok := dynReflect.DataValue(obj, s.dataName); ok {
		return v
	}
	return obj
}

func (s *Store) hasDataField(obj any) bool {
	_, ok := dynReflect.DataValue(obj, s.dataName)
	return ok
}

func (s *Store) dataFieldPtr(obj any) (any, bool) {
	return dynReflect.DataFieldPtr(obj, s.dataName)
}

// payloadDest is the unmarshal target for stored JSON: a pointer to
// obj[dataName] when that field exists, otherwise obj itself.
func (s *Store) payloadDest(obj any) any {
	if ptr, ok := s.dataFieldPtr(obj); ok {
		return ptr
	}
	return obj
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
