package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cfjello/go-store/pkg/store"
)

// TestMain points SQLITE_DB_URL at a per-run temp file so package test never
// attaches to the developer database. database.New is a process-wide singleton,
// so later tests reuse this file rather than honoring per-test t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-store-pkg-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("SQLITE_DB_URL", ":memory:")
	os.Setenv("SQLITE_ATTACH_URL", "file:"+filepath.Join(dir, "test.db"))
	os.Setenv("FILE_DB_ALIAS", "persist")
	code := m.Run()
	if s := store.New(nil, false); s != nil && s.DB != nil {
		_ = s.DB.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
