package database

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

// mockDB is a struct that satisfies the sql.DB interface for testing
type mockDB struct {
	pingError error
	stats     sql.DBStats
}

func (m *mockDB) PingContext(ctx any) error {
	return m.pingError
}

func (m *mockDB) Stats() sql.DBStats {
	return m.stats
}

func (m *mockDB) Close() error {
	return nil
}

func TestHealth(t *testing.T) {
	tests := []struct {
		name       string
		pingError  error
		dbStats    sql.DBStats
		wantStatus string
		wantKeys   []string
	}{
		{
			name:       "Healthy DB",
			pingError:  nil,
			dbStats:    sql.DBStats{OpenConnections: 5, InUse: 2, Idle: 3},
			wantStatus: "up",
			wantKeys:   []string{"status", "message", "open_connections", "in_use", "idle", "wait_count", "wait_duration", "max_idle_closed", "max_lifetime_closed"},
		},
		{
			name:       "DB Down",
			pingError:  errors.New("connection refused"),
			dbStats:    sql.DBStats{},
			wantStatus: "down",
			wantKeys:   []string{"status", "error"},
		},
		{
			name:       "Heavy Load",
			pingError:  nil,
			dbStats:    sql.DBStats{OpenConnections: 45, InUse: 40, Idle: 5},
			wantStatus: "up",
			wantKeys:   []string{"status", "message", "open_connections", "in_use", "idle", "wait_count", "wait_duration", "max_idle_closed", "max_lifetime_closed"},
		},
		{
			name:       "High Wait Count",
			pingError:  nil,
			dbStats:    sql.DBStats{OpenConnections: 10, WaitCount: 1500},
			wantStatus: "up",
			wantKeys:   []string{"status", "message", "open_connections", "in_use", "idle", "wait_count", "wait_duration", "max_idle_closed", "max_lifetime_closed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &mockDB{
				pingError: tt.pingError,
				stats:     tt.dbStats,
			}

			s := &service{
				db: mockDB,
			}

			// Use defer to recover from the log.Fatalf call when DB is down
			defer func() {
				if r := recover(); r != nil {
					// Expected to panic for DB down test
					if tt.pingError == nil {
						t.Errorf("Health() unexpected panic: %v", r)
					}
				}
			}()

			health := s.Health()

			// Check if status matches expected
			if health["status"] != tt.wantStatus {
				t.Errorf("Health() status = %v, want %v", health["status"], tt.wantStatus)
			}

			// Check if all expected keys are present
			for _, key := range tt.wantKeys {
				if _, exists := health[key]; !exists {
					t.Errorf("Health() missing key: %s", key)
				}
			}

			// For DB down case, check if error message contains the ping error
			if tt.pingError != nil && !reflect.ValueOf(health["error"]).IsValid() {
				t.Errorf("Health() missing error message for DB down")
			}

			// For heavy load case, check if message reflects the issue
			if tt.dbStats.OpenConnections > 40 && !contains(health["message"], "heavy load") {
				t.Errorf("Health() message does not indicate heavy load: %s", health["message"])
			}

			// For high wait count case, check if message reflects the issue
			if tt.dbStats.WaitCount > 1000 && !contains(health["message"], "bottleneck") {
				t.Errorf("Health() message does not indicate bottlenecks: %s", health["message"])
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}
