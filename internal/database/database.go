package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/cfjello/go-store/pkg/types"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/mattn/go-sqlite3"
)

// Service represents a service that interacts with a database.
type Service interface {
	SetMeta(key string, meta types.MetaData) bool
	GetMeta(key string, storeId string) (types.MetaData, error)
	SetData(key string, data types.SetArgs) bool
	GetData(key string) (any, error)
	Close() error
	// initDB() error
	// createTables() error
	// dropTables() error

	// Health returns a map of health status information.
	// The keys and values in the map are service-specific.
	Health() map[string]string

	// Close terminates the database connection.
	// It returns an error if the connection cannot be closed.

}

type service struct {
	db  *sql.DB
	sql *SqlStmt
}

var (
	// dburl = os.Getenv("SQLITE_DB_URL")
	dburl = "file:F:/Sqlite3/go-store.db"
	// dbInstance is a singleton instance of the database service.
	dbInstance *service
)

func New() Service {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	dburl := os.Getenv("SQLITE_DB_URL")
	if dburl == "" {
		// Default to a file-based SQLite database if not set
		dburl = "file:F:/Sqlite3/go-store.db"
	}
	db, err := sql.Open("sqlite3", dburl) // Use this for a persistent database

	if err != nil {
		// This will not be a connection error, but a DSN parse error or
		// another initialization error.
		log.Fatal(err)
	}

	dropErr := dropTables(db)
	if dropErr != nil {
		log.Fatal(dropErr)
	}
	// Create tables after dropping existing ones
	dbErr := createTables(db)
	if dbErr != nil {
		log.Fatal(dbErr)
	}

	// Prepare SQL statements
	sqlStmt, err := NewSqlStmt(db)
	if err != nil {
		log.Fatalf("Failed to prepare SQL statements: %v", err)
	}

	dbInstance = &service{
		db:  db,
		sql: sqlStmt,
	}

	return dbInstance
}

func (s *service) SetData(key string, value types.SetArgs) bool {
	// Implementation for setting data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := json.Marshal(value.Object)
	if err != nil {
		log.Printf("Failed to marshal object data for key: %s, error: %v", key, err)
		return false
	}

	_, err = s.sql.dataInsStmt.ExecContext(ctx, key, value.JobID, value.SchemaKey, ObjJSON)

	if err != nil {
		log.Printf("Failed to set data for key: %s, error: %v", key, err)
		return false
	}
	log.Printf("Data set for key: %s", key)
	return true
}

func (s *service) GetData(key string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var data types.SetArgs
	var dataJson []byte
	err := s.sql.dataSelStmt.QueryRowContext(ctx, key).Scan(&data.Key, &data.JobID, &data.SchemaKey, &dataJson)
	if err != nil {
		log.Printf("Failed to get data for key: %s, error: %v", key, err)
		return nil, err
	}

	// Unmarshal the JSON object data

	err = json.Unmarshal(dataJson, &data.Object)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return nil, err
	}

	return data.Object, nil
}

func (s *service) SetMeta(key string, value types.MetaData) bool {
	// Implementation for setting metadata in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	metaJSON, err := json.Marshal(value)
	if err != nil {
		log.Printf("Failed to marshal meta data for key: %s, error: %v", key, err)
		return false
	}
	meta := string(metaJSON)
	s.sql.metaInsStmt.ExecContext(ctx, key, meta)
	log.Printf("Meta data set for key: %s", key)
	return true
}

func (s *service) GetMeta(key string, storeId string) (types.MetaData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var meta types.MetaData
	err := s.sql.metaSelStmt.QueryRowContext(ctx, key).Scan(&meta)
	if err != nil {
		log.Printf("Failed to get meta data for key: %s, storeId: %s, error: %v", key, storeId, err)
		return types.MetaData{}, err
	}
	return meta, nil
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)

	// Ping the database
	err := s.db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Fatalf("db down: %v", err) // Log the error and terminate the program
		return stats
	}

	// Database is up, add more statistics
	stats["status"] = "up"
	stats["message"] = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := s.db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats["message"] = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

// Close closes the database connection.
// It logs a message indicating the disconnection from the specific database.
// If the connection is successfully closed, it returns nil.
// If an error occurs while closing the connection, it returns the error.
func (s *service) Close() error {
	log.Printf("Disconnecting from database: %s", dburl)
	return s.db.Close()
}
