package test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/cfjello/go-store/internal/database"

	_ "github.com/mattn/go-sqlite3"
)

// openTestDB opens a real in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	return db
}

func TestHealth(t *testing.T) {
	healthKeys := []string{"status", "message", "open_connections", "in_use", "idle", "wait_count", "wait_duration", "max_idle_closed", "max_lifetime_closed"}

	t.Run("Healthy DB", func(t *testing.T) {
		db := openTestDB(t)
		defer db.Close()

		svc := &database.DBService{DB: db}
		health := svc.Health()

		if health["status"] != "up" {
			t.Errorf("Health() status = %v, want %v", health["status"], "up")
		}
		for _, key := range healthKeys {
			if _, exists := health[key]; !exists {
				t.Errorf("Health() missing key: %s", key)
			}
		}
	})

	t.Run("DB Down", func(t *testing.T) {
		db := openTestDB(t)
		db.Close() // Close immediately to simulate a down database

		svc := &database.DBService{DB: db}
		health := svc.Health()

		if health["status"] != "down" {
			t.Errorf("Health() status = %v, want %v", health["status"], "down")
		}
		if _, exists := health["error"]; !exists {
			t.Errorf("Health() missing error message for DB down")
		}
	})

	t.Run("Heavy Load", func(t *testing.T) {
		db := openTestDB(t)
		defer db.Close()

		// Pin more than 40 open connections to simulate heavy load
		db.SetMaxOpenConns(100)
		conns := make([]*sql.Conn, 0, 45)
		for i := 0; i < 45; i++ {
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatalf("Failed to pin connection %d: %v", i, err)
			}
			conns = append(conns, conn)
		}
		defer func() {
			for _, conn := range conns {
				conn.Close()
			}
		}()

		svc := &database.DBService{DB: db}
		health := svc.Health()

		if health["status"] != "up" {
			t.Errorf("Health() status = %v, want %v", health["status"], "up")
		}
		if !strings.Contains(health["message"], "heavy load") {
			t.Errorf("Health() message does not indicate heavy load: %s", health["message"])
		}
	})
}
