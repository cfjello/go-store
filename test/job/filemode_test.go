package job_test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

// TestFileMainWALAndPersistence verifies that a file main database runs in
// WAL mode and that metadata and data survive a close/reopen without reset.
// The test closes the shared database singleton, so it must run last in this
// package: this file name sorts after the other test files.
func TestFileMainWALAndPersistence(t *testing.T) {
	s := store.New(nil, false)
	if s == nil || s.DB == nil {
		t.Fatal("no store/database")
	}
	if s.DB.DbUrl == "" || s.DB.DbUrl == ":memory:" {
		t.Skip("main database is in-memory; file-mode test skipped")
	}

	var journalMode string
	if err := s.DB.DB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	obj := &struct{}{}
	if _, err := s.Register(types.RegisterArgs{Key: "fm-persist", Object: obj}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := s.Set(types.SetArgs{Key: "fm-persist", Object: obj}); err != nil {
		t.Fatalf("set: %v", err)
	}
	storeID, err := s.GetCurrStoreID("fm-persist")
	if err != nil {
		t.Fatalf("GetCurrStoreID: %v", err)
	}

	// Close clears the database singleton; New reopens the same file and,
	// with reset=false, keeps its tables and rows.
	if err := s.DB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2 := store.New(nil, false)
	if !s2.IsRegistered("fm-persist") {
		t.Fatal("metadata did not survive reopen")
	}
	storeID2, err := s2.GetCurrStoreID("fm-persist")
	if err != nil {
		t.Fatalf("GetCurrStoreID after reopen: %v", err)
	}
	if storeID2 != storeID {
		t.Fatalf("store ID after reopen = %q, want %q", storeID2, storeID)
	}
}
