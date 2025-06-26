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
	"github.com/cfjello/go-store/pkg/util"

	_ "github.com/mattn/go-sqlite3"
)

// StoreService represents a service that interacts with a database.

/*
type DBFunctions interface {
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
*/

type DBService struct {
	DB    *sql.DB
	SQL   *SqlStmt
	DbUrl string
}

var dbInstance *DBService

func New() *DBService {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	util.SetEnv() // Load default environment variables

	dbUrl := os.Getenv("SQLITE_DB_URL")
	if dbUrl == "" {
		// Default to a file-based SQLite database if not set
		dbUrl = "file:F:/Sqlite3/go-store.db"
	}
	/*
		dbFlags := os.Getenv("SQLITE_DB_FLAGS")
		if dbFlags == "" {
			// Default flags for SQLite database
			dbFlags = ";PRAGMA journal_mode=OFF;"
		}
	*/
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatalf("Failed to open in-memory database: %v", err)
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

	dbInstance = &DBService{
		DbUrl: dbUrl,
		DB:    db,
		SQL:   sqlStmt,
	}

	return dbInstance
}

func (s *DBService) SetData(key string, value types.SetArgs) bool {
	// Implementation for setting data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := json.Marshal(value.Object)
	if err != nil {
		log.Printf("Failed to marshal object data for key: %s, error: %v", key, err)
		return false
	}

	sqlRes, err := s.SQL.dataInsStmt.ExecContext(ctx, key, value.JobID, value.SchemaKey, ObjJSON)
	if err != nil {
		log.Printf("Failed to execute statement for key: %s, error: %v", key, err)
		return false
	}

	rowsAffected, err := sqlRes.RowsAffected()
	if err != nil || rowsAffected != 1 {
		log.Printf("Failed to set data for key: %s, error: %v", key, err)
		return false
	}
	// log.Printf("Data set for key: %s", key)
	return true
}

func (s *DBService) GetData(key string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var data types.SetArgs
	var dataJson []byte
	err := s.SQL.dataSelStmt.QueryRowContext(ctx, key).Scan(&data.Key, &data.JobID, &data.SchemaKey, &dataJson)
	if err != nil {
		log.Printf("Failed to get data for key: %s, error: %v", key, err)
		return nil, err
	}

	err = json.Unmarshal(dataJson, &data.Object)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return nil, err
	}

	return data.Object, nil
}

func (s *DBService) SetMeta(key string, value types.MetaData) bool {
	// Implementation for setting metadata in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metaJSON, err := json.Marshal(value)
	if err != nil {
		log.Printf("Failed to marshal meta data for key: %s, error: %v", key, err)
		return false
	}
	meta := string(metaJSON)
	s.SQL.metaInsStmt.ExecContext(ctx, key, meta)
	// log.Printf("Meta data set for key: %s", key)
	return true
}

func (s *DBService) GetMeta(key string, schemaKey string) (types.MetaData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var meta types.MetaData
	var metaJson []byte
	if schemaKey == "" {
		schemaKey = key
	}
	err := s.SQL.metaSelStmt.QueryRowContext(ctx, key, schemaKey).Scan(&metaJson)
	if err != nil {
		// log.Printf("Failed to get meta data for key: %s, error: %v", key, err)
		return types.MetaData{}, err
	}

	err = json.Unmarshal(metaJson, &meta)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return types.MetaData{}, err
	}
	return meta, nil
}

/*
	func (s *DBService) GetMetaInit(key string) (bool, error) {
		// Implementation for getting initial metadata
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		init := false
		err := s.SQL.metaSelInitStmt.QueryRowContext(ctx, key).Scan(&init)
		if err != nil {
			log.Printf("Failed to get init meta data for key: %s, error: %v", key, err)

		}
		// If no row is found, return false for init
		return init, err
	}
*/
func (s *DBService) GetCurrStoreID(key string) (string, error) {
	// Implementation for getting current store ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var storeID string
	err := s.SQL.dataSelLastStmt.QueryRowContext(ctx, key).Scan(&storeID)
	if err != nil {
		log.Printf("failed to get current store ID for key: %s, error: %v", key, err)
		return "", err
	}
	return storeID, nil
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *DBService) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)

	// Ping the database
	err := s.DB.PingContext(ctx)
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
	dbStats := s.DB.Stats()
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
func (s *DBService) Close() error {
	log.Printf("Disconnecting from database: %s", s.DbUrl)
	return s.DB.Close()
}
