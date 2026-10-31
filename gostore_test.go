package gostore_test

import (
	"testing"

	gostore "github.com/cfjello/go-store"
)

func TestPublicAPICompiles(t *testing.T) {
	t.Helper()
	_ = gostore.NewStoreService
	_ = gostore.NewJobService
	_ = gostore.RegisterArgs{}
	_ = gostore.SetArgs{}
	_ = gostore.CheckOn
	_ = gostore.Ptr(true)
	_ = gostore.DefaultConfig
	_ = gostore.SetEnv
	_ = gostore.Ulid
	_ = gostore.Enabled
}
