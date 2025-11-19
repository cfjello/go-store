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
type DBService struct {
	DB      *sql.DB
	SQL     map[string]*sql.Stmt
	DbUrl   string // URL of the attached file database
	DbAlias string // Alias for the attached file database, if any
}

var dbInstance *DBService

func New(reset bool) *DBService {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	util.SetEnv() // Load default environment variables

	// db, err := sql.Open("sqlite3", ":memory:")
	db, err := sql.Open("sqlite3", ":memory:") // Open an in-memory SQLite database
	if err != nil {
		log.Fatalf("Failed to open in-memory database: %v", err)
	}

	fileDbPath := os.Getenv("SQLITE_DB_URL")
	fileDbAlias := os.Getenv("FILE_DB_ALIAS")

	dbInstance = &DBService{
		DbUrl:   fileDbPath,
		DB:      db,
		SQL:     nil, // Will be populated by PrepareStmts
		DbAlias: fileDbAlias,
	}

	// Optionally attach a file database if specified in environment
	if fileDbPath != "" {
		if fileDbAlias == "" {
			fileDbAlias = "persist" // Default alias if not set
		}
		err := dbInstance.AttachFileDB(fileDbPath, fileDbAlias)
		if err != nil {
			log.Fatalf("Error: Failed to attach file database: %v", err)
		}
	}
	// Initialize the in-memory and optionally the file database schema
	initDB(db, reset)

	// Prepare SQL statements
	sqlStmts, err := PrepareStmts(db)
	if err != nil {
		log.Fatalf("Failed to prepare SQL statements: %v", err)
	}

	dbInstance.SQL = sqlStmts

	// defer dbInstance.Close() // Ensure the database is closed when the program exits
	return dbInstance
}

func (s *DBService) SetData(key string, args types.SetArgs) bool {
	// Implementation for setting data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := json.Marshal(args.Object)
	if err != nil {
		log.Printf("Failed to marshal object data for key: %s, error: %v", key, err)
		return false
	}

	// Persist to disk if requested
	if args.Persist {
		// If we have a file database attached, also insert into the persistent store
		sqlRes2, err := s.SQL["pDataInsert"].ExecContext(ctx, key, args.JobID, args.Schema, ObjJSON)
		if err != nil {
			log.Printf("Failed to execute persistent statement for key: %s, error: %v", key, err)
			return false
		}
		// Check if the execution was successful
		rowsAffected, err := sqlRes2.RowsAffected()
		if err != nil || rowsAffected != 1 {
			log.Printf("Failed to set persistent data for key: %s, error: %v", key, err)
			return false
		}
	} else {
		sqlRes, err := s.SQL["DataInsert"].ExecContext(ctx, key, args.JobID, args.Schema, ObjJSON)
		if err != nil {
			log.Printf("Failed to execute statement for key: %s, error: %v", key, err)
			return false
		}
		// Check if the execution was successful
		rowsAffected, err := sqlRes.RowsAffected()
		if err != nil || rowsAffected != 1 {
			log.Printf("Failed to set data for key: %s, error: %v", key, err)
			return false
		}
	}
	return true
}

// UpsertData performs an INSERT or UPDATE operation (UPSERT)
func (s *DBService) UpsertData(key string, args types.SetArgs) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := json.Marshal(args.Object)
	if err != nil {
		log.Printf("Failed to marshal object data for key: %s, error: %v", key, err)
		return false
	}

	// Use the DataUpsert statement if available, otherwise fall back to DataInsert
	var stmt *sql.Stmt
	var ok bool
	if stmt, ok = s.SQL["DataUpsert"]; !ok {
		stmt = s.SQL["DataInsert"]
	}

	sqlRes, err := stmt.ExecContext(ctx, key, args.JobID, args.Schema, ObjJSON)
	if err != nil {
		log.Printf("Failed to execute upsert statement for key: %s, error: %v", key, err)
		return false
	}

	rowsAffected, err := sqlRes.RowsAffected()
	if err != nil || rowsAffected < 1 {
		log.Printf("Failed to upsert data for key: %s, error: %v", key, err)
		return false
	}

	// Persist to disk if requested
	if (args.Persist || s.DbUrl != "") && s.DbAlias != "" {
		var pStmt *sql.Stmt
		if pStmt, ok = s.SQL["pDataUpsert"]; !ok {
			pStmt = s.SQL["pDataInsert"]
		}

		sqlRes2, err := pStmt.ExecContext(ctx, key, args.JobID, args.Schema, ObjJSON)
		if err != nil {
			log.Printf("Failed to execute persistent upsert statement for key: %s, error: %v", key, err)
			return false
		}

		rowsAffected, err := sqlRes2.RowsAffected()
		if err != nil || rowsAffected < 1 {
			log.Printf("Failed to upsert persistent data for key: %s, error: %v", key, err)
			return false
		}
	}
	return true
}

func (s *DBService) GetData(key string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var data types.SetArgs
	var dataJson []byte
	err := s.SQL["DataSelect"].QueryRowContext(ctx, key).Scan(&data.Key, &data.JobID, &data.Schema, &dataJson)
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

	sqlRes, err := s.SQL["MetaInsert"].ExecContext(ctx, key, value.Schema, metaJSON)
	if err != nil {
		log.Printf("Failed to execute meta insert for key: %s, error: %v", key, err)
		return false
	}

	rowsAffected, err := sqlRes.RowsAffected()
	if err != nil || rowsAffected < 1 {
		log.Printf("Failed to insert meta data for key: %s, error: %v", key, err)
		return false
	}

	return true
}

// UpsertMeta performs an INSERT or UPDATE operation for metadata
func (s *DBService) UpsertMeta(key string, value types.MetaData) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metaJSON, err := json.Marshal(value)
	if err != nil {
		log.Printf("Failed to marshal meta data for key: %s, error: %v", key, err)
		return false
	}

	meta := string(metaJSON)
	sqlRes, err := s.SQL["MetaInsert"].ExecContext(ctx, key, value.Schema, meta)
	if err != nil {
		log.Printf("Failed to execute meta upsert for key: %s, error: %v", key, err)
		return false
	}

	rowsAffected, err := sqlRes.RowsAffected()
	if err != nil || rowsAffected < 1 {
		log.Printf("Failed to upsert meta data for key: %s, error: %v", key, err)
		return false
	}

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
	err := s.SQL["MetaSelect"].QueryRowContext(ctx, key, schemaKey).Scan(&metaJson)
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

// SetJobData sets job data for a given JobID
func (s *DBService) PersistJob(JobID string) error {
	// Implementation for setting job data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Failed to begin transaction for JobID: %s, error: %v", JobID, err)
		return err
	}
	defer tx.Rollback()

	_, err = s.SQL["pJobData"].ExecContext(ctx, JobID)
	if err != nil {
		log.Printf("Failed to insert job data for JobID: %s, error: %v", JobID, err)
		return nil
	}
	_, err = s.SQL["pJob"].ExecContext(ctx, JobID)
	if err != nil {
		log.Printf("Failed to persist job data for JobID: %s, error: %v", JobID, err)
		return err
	}

	_, err = s.SQL["pJobMeta"].ExecContext(ctx, JobID)
	if err != nil {
		log.Printf("Failed to persist meta data for JobID: %s, error: %v", JobID, err)
		return err
	}
	tx.Commit()
	// log.Printf("Job data set for JobID: %s", JobID)

	return nil
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
	err := s.SQL["DataSelLast"].QueryRowContext(ctx, key).Scan(&storeID)
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
	if s.DbUrl != "" {
		s.DetachFileDB(s.DbAlias)
	}
	log.Printf("Disconnecting from database: %s", s.DbUrl)
	return s.DB.Close()
}

// AttachFileDB attaches a file-based SQLite database to the current in-memory database
func (s *DBService) AttachFileDB(filePath string, alias string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get the SQLite flags from environment variable
	dbFlags := os.Getenv("SQLITE_DB_FLAGS")

	// Construct the connection string with flags
	var connectionString string
	if dbFlags != "" {
		connectionString = filePath + dbFlags
	} else {
		connectionString = filePath
	}

	query := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", connectionString, alias)
	_, err := s.DB.ExecContext(ctx, query)
	if err != nil {
		log.Printf("Failed to attach file database %s as %s: %v", connectionString, alias, err)
		return err
	}

	log.Printf("Successfully attached file database %s as %s", connectionString, alias)
	return nil
}

// DetachFileDB detaches a previously attached database
func (s *DBService) DetachFileDB(alias string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf("DETACH DATABASE %s", alias)
	_, err := s.DB.ExecContext(ctx, query)
	if err != nil {
		log.Printf("Failed to detach database %s: %v", alias, err)
		return err
	}

	log.Printf("Successfully detached database %s", alias)
	return nil
}
