package job_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cfjello/go-store/pkg/store"
)

// TestMain points the main database at a per-run temp file (file mode, WAL)
// so the job and notification tests exercise the multi-connection pool, and
// parallel test packages never share a database file.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-store-job-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("SQLITE_DB_URL", "file:"+filepath.Join(dir, "test.db"))
	code := m.Run()
	if s := store.New(nil, false); s != nil && s.DB != nil {
		_ = s.DB.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
