// Package gostore is the convenience entry point for the go-store module.
//
//	import gostore "github.com/cfjello/go-store"
//
// The module path is github.com/cfjello/go-store; the package name is gostore
// because a Go package name cannot contain a hyphen. The same types and
// constructors remain available under their original paths
// (github.com/cfjello/go-store/pkg/store, …/pkg/types, …/pkg/job, …).
//
// internal/ packages are not re-exported. Run the HTTP API with
//
//	go run github.com/cfjello/go-store/cmd/api
package gostore

import (
	"github.com/cfjello/go-store/pkg/config"
	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

type (
	Store      = store.Store
	Notifier   = store.Notifier
	OpenOpts   = store.OpenOpts
	Iter       = store.Iter
	Element    = store.Element
	JobService = job.JobService
	Status     = job.Status
	RunStatus  = job.RunStatus
)

var (
	ErrClosed  = store.ErrClosed
	ErrRestock = store.ErrRestock
)

// NewStoreService returns the process-wide singleton Store. reset is honored
// only on the first call in the process.
func NewStoreService(reset bool) *Store {
	return store.NewStoreService(reset)
}

// NewJobService builds a JobService on s and registers it as the store's
// change notifier. Repeated calls return the process-wide singleton.
func NewJobService(s *Store) *JobService {
	return job.NewJobService(s)
}

type (
	RegisterArgs = types.RegisterArgs
	SetArgs      = types.SetArgs
	PublishArgs  = types.PublishArgs
	MetaData     = types.MetaData
	Dependencies = types.Dependencies
	NotifyList   = types.NotifyList
	CheckMode    = types.CheckMode
	RunGraph     = types.RunGraph
	RunNode      = types.RunNode
	JobVertex    = types.JobVertex
	JobDbEntry   = types.JobDbEntry
	ADT_Job      = types.ADT_Job
	ExtError     = types.ExtError
)

const (
	CheckNone = types.CheckNone
	CheckOn   = types.CheckOn
	CheckSkip = types.CheckSkip
)

// Ptr returns a pointer to v, for optional fields such as SetArgs.Fixed.
func Ptr[T any](v T) *T { return types.Ptr(v) }

// ValidateDeps reports whether each dependency key is registered and not self.
func ValidateDeps(self string, isValid func(string) bool, deps ...string) error {
	return types.ValidateDeps(self, isValid, deps...)
}

// ValidateNotify reports whether each notify target is registered and not self.
func ValidateNotify(self string, isValid func(string) bool, targets ...string) error {
	return types.ValidateNotify(self, isValid, targets...)
}

type (
	Config  = config.Config
	RestAPI = config.RestAPI
)

// DefaultConfig returns the compiled-in defaults, including DefaultEnv.
func DefaultConfig() Config { return config.DefaultConfig() }

// SetEnv applies DefaultEnv for every variable that is not already set.
func SetEnv() { util.SetEnv() }

// Ulid returns a new time-ordered ULID string.
func Ulid() string { return util.Ulid() }

type (
	Stat     = perf.Stat
	SortBy   = perf.SortBy
	Settings = perf.Settings
)

const (
	ByTotal = perf.ByTotal
	ByCalls = perf.ByCalls
)

func Track(name string) func()     { return perf.Track(name) }
func Enabled() bool                { return perf.Enabled() }
func SetEnabled(on bool)           { perf.SetEnabled(on) }
func Snapshot() []Stat             { return perf.Snapshot() }
func Top(n int, by SortBy) []Stat  { return perf.Top(n, by) }
func Reset()                       { perf.Reset() }
func CurrentSettings() Settings    { return perf.CurrentSettings() }
func SyncFromEnv()                 { perf.SyncFromEnv() }
func StartReporter() (stop func()) { return perf.StartReporter() }
