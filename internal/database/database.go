package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cfjello/go-store/pkg/config"
	"github.com/cfjello/go-store/pkg/dynReflect"
	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"

	_ "github.com/mattn/go-sqlite3"
)

// StoreService represents a service that interacts with a database.
type DBService struct {
	DB      *sql.DB
	SQL     map[string]*sql.Stmt
	DbUrl   string // URL of the main database (":memory:" or a file DSN)
	DbAlias string // Alias of the attached persist layer; empty when there is none
}

var dbInstance *DBService

func New(reset bool) *DBService {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	util.SetEnv() // Load default environment variables

	// SQLITE_DB_URL selects the main database: empty or ":memory:" keeps an
	// in-memory database; anything else opens a file database with
	// SQLITE_DB_FLAGS appended to the DSN (WAL mode by default).
	dbURL := os.Getenv("SQLITE_DB_URL")
	inMemory := dbURL == "" || dbURL == ":memory:"

	var db *sql.DB
	var err error
	if inMemory {
		dbURL = ":memory:"
		db, err = sql.Open("sqlite3", dbURL)
	} else {
		if mkErr := ensureParentDir(dbURL); mkErr != nil {
			log.Fatalf("Failed to create directory for %s: %v", dbURL, mkErr)
		}
		db, err = sql.Open("sqlite3", dbURL+os.Getenv("SQLITE_DB_FLAGS"))
	}
	if err != nil {
		log.Fatalf("Failed to open database %s: %v", dbURL, err)
	}

	// Pool sizing. An in-memory SQLite database is per connection: a second
	// pooled connection would open its own empty database, so the pool is
	// pinned to a single connection. A file database in WAL mode supports
	// concurrent readers and a single writer across connections, so the pool
	// size is configurable via SQLITE_MAX_CONNS (default 4); SQLITE_BUSY under
	// concurrent writers is handled by _busy_timeout in SQLITE_DB_FLAGS.
	if inMemory {
		db.SetMaxOpenConns(1)
	} else {
		n := maxOpenConns()
		db.SetMaxOpenConns(n)
		db.SetMaxIdleConns(n)
	}

	dbInstance = &DBService{
		DbUrl:   dbURL,
		DB:      db,
		SQL:     nil, // Will be populated by PrepareStmts
		DbAlias: "",  // Set when a persist layer is attached below
	}

	// Optionally attach a file database as the persist layer. This is only
	// meaningful alongside an in-memory main database: a file main database
	// is already persistent, so nothing is attached in file mode.
	if inMemory {
		if attachURL := os.Getenv("SQLITE_ATTACH_URL"); attachURL != "" {
			alias := os.Getenv("FILE_DB_ALIAS")
			if alias == "" {
				alias = "persist" // Default alias if not set
			}
			if err := dbInstance.AttachFileDB(attachURL, alias); err != nil {
				log.Fatalf("Error: Failed to attach file database: %v", err)
			}
			dbInstance.DbAlias = alias
		}
	}

	// Initialize the schema: always the main schema, plus the attached
	// persist schema when present.
	if err := initDB(db, reset, dbInstance.DbAlias); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Prepare SQL statements
	sqlStmts, err := PrepareStmts(db, dbInstance.DbAlias)
	if err != nil {
		log.Fatalf("Failed to prepare SQL statements: %v", err)
	}

	dbInstance.SQL = sqlStmts

	// defer dbInstance.Close() // Ensure the database is closed when the program exits
	return dbInstance
}

// maxOpenConns reads SQLITE_MAX_CONNS, defaulting to 4 when unset or invalid.
func maxOpenConns() int {
	n, err := strconv.Atoi(os.Getenv("SQLITE_MAX_CONNS"))
	if err != nil || n < 1 {
		return 4
	}
	return n
}

// ensureParentDir creates the parent directory of a file DSN when needed.
// Accepts "file:<path>?<query>" URLs and bare paths.
func ensureParentDir(dbURL string) error {
	p := strings.TrimPrefix(dbURL, "file:")
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	dir := filepath.Dir(p)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func (s *DBService) SetData(key string, args types.SetArgs) bool {
	defer perf.Track("db.SetData")()
	// Implementation for setting data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := marshalStoredObject(args.Object)
	if err != nil {
		log.Printf("Failed to marshal object data for key: %s, error: %v", key, err)
		return false
	}

	storeID, jobID := dataIDs(args)
	// Always write the main row. Publish additionally copies the same version
	// to the attached persist layer, when one is attached (a file main
	// database is already persistent and has no separate persist layer).
	if err := execDataInsert(ctx, s.SQL["DataInsert"], storeID, jobID, key, ObjJSON); err != nil {
		log.Printf("Failed to execute statement for key: %s, error: %v", key, err)
		return false
	}
	if args.Publish && s.DbAlias != "" {
		if err := execDataInsert(ctx, s.SQL["pDataInsert"], storeID, jobID, key, ObjJSON); err != nil {
			log.Printf("Failed to execute persistent statement for key: %s, error: %v", key, err)
			return false
		}
	}
	return true
}

// marshalStoredObject encodes the value that belongs in data_json. When obj has
// an exported field matching Names.DataName (default "data"), that field is the
// payload. Otherwise the whole object is stored — the standalone static case.
func marshalStoredObject(obj any) ([]byte, error) {
	payload := obj
	if v, ok := dynReflect.DataValue(obj, config.DefaultConfig().Names.DataName); ok {
		payload = v
	}
	return json.Marshal(payload)
}

// dataIDs returns the data_id and job_id for a version. A missing store id is
// generated so repeated saves with the same job id do not collide on
// PRIMARY KEY(data_id, job_id). The object key is stored separately as meta_key.
func dataIDs(args types.SetArgs) (storeID, jobID string) {
	storeID = args.StoreId
	if storeID == "" {
		storeID = util.Ulid()
	}
	jobID = args.JobId
	if jobID == "" {
		jobID = storeID
	}
	return storeID, jobID
}

// execDataInsert inserts one data version. metaKey is the object key, not the
// schema parent, so GetCurrStoreID(metaKey) can return storeID.
func execDataInsert(ctx context.Context, stmt *sql.Stmt, storeID, jobID, metaKey string, objJSON []byte) error {
	if stmt == nil {
		return fmt.Errorf("data insert statement is not prepared")
	}
	sqlRes, err := stmt.ExecContext(ctx, storeID, jobID, metaKey, objJSON)
	if err != nil {
		return err
	}
	rowsAffected, err := sqlRes.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected < 1 {
		return fmt.Errorf("data insert affected %d rows", rowsAffected)
	}
	return nil
}

// UpsertData performs an INSERT or UPDATE operation (UPSERT)
func (s *DBService) UpsertData(key string, args types.SetArgs) bool {
	defer perf.Track("db.UpsertData")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ObjJSON, err := marshalStoredObject(args.Object)
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

	storeID, jobID := dataIDs(args)
	if err := execDataInsert(ctx, stmt, storeID, jobID, key, ObjJSON); err != nil {
		log.Printf("Failed to execute upsert statement for key: %s, error: %v", key, err)
		return false
	}

	// Persist to disk if requested
	if (args.Publish || s.DbUrl != "") && s.DbAlias != "" {
		var pStmt *sql.Stmt
		if pStmt, ok = s.SQL["pDataUpsert"]; !ok {
			pStmt = s.SQL["pDataInsert"]
		}

		if err := execDataInsert(ctx, pStmt, storeID, jobID, key, ObjJSON); err != nil {
			log.Printf("Failed to execute persistent upsert statement for key: %s, error: %v", key, err)
			return false
		}
	}
	return true
}

// GetData retrieves the stored object for the given key. If out is non-nil,
// the JSON data is unmarshalled directly into out (which must be a pointer);
// otherwise the data is decoded into a generic value and returned.
func (s *DBService) GetData(key string, out any) (any, error) {
	defer perf.Track("db.GetData")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var data types.SetArgs
	dataJson, err := scanDataJSON(ctx, s.SQL["DataSelect"], key)
	if err == sql.ErrNoRows {
		dataJson, err = scanDataJSON(ctx, s.SQL["pDataSelect"], key)
	}
	if err != nil {
		log.Printf("Failed to get data for key: %s, error: %v", key, err)
		return nil, err
	}

	if out != nil {
		if err = json.Unmarshal(dataJson, out); err != nil {
			log.Printf("Failed to unmarshal object data into user object for key: %s, error: %v", key, err)
			return nil, err
		}
		return out, nil
	}

	err = json.Unmarshal(dataJson, &data.Object)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return nil, err
	}

	return data.Object, nil
}

func (s *DBService) SetMeta(key string, value types.MetaData) bool {
	defer perf.Track("db.SetMeta")()
	// Implementation for setting metadata in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metaJSON, err := json.Marshal(value)
	if err != nil {
		log.Printf("Failed to marshal meta data for key: %s, error: %v", key, err)
		return false
	}

	sqlRes, err := s.SQL["MetaInsert"].ExecContext(ctx, key, value.ParentKey, metaJSON)
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
	sqlRes, err := s.SQL["MetaInsert"].ExecContext(ctx, key, value.ParentKey, meta)
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
	defer perf.Track("db.GetMeta")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var meta types.MetaData
	var metaJson []byte
	if schemaKey == "" {
		schemaKey = key
	}
	// MetaSelect params: (key, schemaKey) — exact key match first, then a
	// fallback match on the schema key.
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

/*
	The following section contains functions related to job data management in the database.
*/

func (s *DBService) GetJobMeta(key string, schemaKey string) (types.JobDbEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var jobData types.JobDbEntry
	var jobJson []byte
	if schemaKey == "" {
		schemaKey = key
	}
	err := s.SQL["JobJson"].QueryRowContext(ctx, key, schemaKey).Scan(&jobJson)
	if err != nil {
		// log.Printf("Failed to get meta data for key: %s, error: %v", key, err)
		return types.JobDbEntry{}, err
	}
	err = json.Unmarshal(jobJson, &jobData)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return types.JobDbEntry{}, err
	}

	return jobData, nil
}

func (s *DBService) GetCurrJobDef(key string, schemaKey string) (types.JobDbEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var jobData types.JobDbEntry
	var jobJson []byte
	if schemaKey == "" {
		schemaKey = key
	}
	err := s.SQL["JobJson"].QueryRowContext(ctx, key, schemaKey).Scan(&jobJson)
	if err != nil {
		// log.Printf("Failed to get meta data for key: %s, error: %v", key, err)
		return types.JobDbEntry{}, err
	}
	err = json.Unmarshal(jobJson, &jobData)
	if err != nil {
		log.Printf("Failed to unmarshal object data for key: %s, error: %v", key, err)
		return types.JobDbEntry{}, err
	}

	return jobData, nil
}

// SetJobData sets job data for a given JobID
func (s *DBService) PersistJob(JobID string) error {
	if s.DbAlias == "" {
		// No attached persist layer: either the main database is in-memory
		// without an attachment, or it is already the persistent store.
		return nil
	}
	// Implementation for setting job data in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Failed to begin transaction for JobID: %s, error: %v", JobID, err)
		return err
	}
	defer tx.Rollback()

	// Bind the prepared statements to the transaction: the pool is pinned to
	// a single connection, so pool-level statements would wait for the
	// connection this transaction holds and deadlock.
	_, err = tx.StmtContext(ctx, s.SQL["pJobData"]).ExecContext(ctx, JobID)
	if err != nil {
		log.Printf("Failed to insert job data for JobID: %s, error: %v", JobID, err)
		return nil
	}
	_, err = tx.StmtContext(ctx, s.SQL["pJob"]).ExecContext(ctx, JobID)
	if err != nil {
		log.Printf("Failed to persist job data for JobID: %s, error: %v", JobID, err)
		return err
	}

	_, err = tx.StmtContext(ctx, s.SQL["pJobMeta"]).ExecContext(ctx, JobID)
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
	defer perf.Track("db.GetCurrStoreID")()
	// Implementation for getting current store ID. Reads the in-memory table
	// first, then the attached file database, so a published version is still
	// found after the process restarts.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	storeID, err := scanStoreID(ctx, s.SQL["DataSelLast"], key)
	if err == sql.ErrNoRows {
		storeID, err = scanStoreID(ctx, s.SQL["pDataSelLast"], key)
	}
	if err != nil {
		log.Printf("failed to get current store ID for key: %s, error: %v", key, err)
		return "", err
	}
	return storeID, nil
}

func scanStoreID(ctx context.Context, stmt *sql.Stmt, key string) (string, error) {
	if stmt == nil {
		return "", sql.ErrNoRows
	}
	var storeID string
	err := stmt.QueryRowContext(ctx, key).Scan(&storeID)
	return storeID, err
}

func scanDataJSON(ctx context.Context, stmt *sql.Stmt, storeID string) ([]byte, error) {
	if stmt == nil {
		return nil, sql.ErrNoRows
	}
	var dataID, metaKey, jobID string
	var dataJSON []byte
	err := stmt.QueryRowContext(ctx, storeID).Scan(&dataID, &metaKey, &jobID, &dataJSON)
	return dataJSON, err
}

func (s *DBService) GetCurrJobID(key string) (string, error) {
	// Implementation for getting current job ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var jobID string
	err := s.SQL["JobSelLast"].QueryRowContext(ctx, key).Scan(&jobID)
	if err != nil {
		log.Printf("failed to get current job ID for key: %s, error: %v", key, err)
		return "", err
	}
	return jobID, nil
}

// GetMaxDataIDs returns the maximum data_id for each meta_key in the provided
// list. The IN clause is built dynamically because the list is of variable
// length, so this query is executed directly (not via a prepared statement).
// It targets the in-memory main schema; set persist to true to query the
// attached file database schema instead.
func (s *DBService) GetMaxDataIDs(metaIDs []string, persist bool) (map[string]string, error) {
	result := make(map[string]string, len(metaIDs))
	if len(metaIDs) == 0 {
		return result, nil
	}

	schema := "main"
	if persist {
		prefix := s.DbAlias
		if prefix == "" {
			return nil, fmt.Errorf("no file database attached: cannot query persisted max data IDs")
		}
		schema = prefix
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(metaIDs)), ",")
	args := make([]any, len(metaIDs))
	for i, id := range metaIDs {
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT meta_key, MAX(data_id) FROM %s.data WHERE meta_key IN (%s) GROUP BY meta_key",
		schema, placeholders,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query max data IDs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var metaID, dataID string
		if err := rows.Scan(&metaID, &dataID); err != nil {
			return nil, fmt.Errorf("failed to scan max data ID row: %w", err)
		}
		result[metaID] = dataID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max data ID rows: %w", err)
	}
	return result, nil
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
		log.Printf("db down: %v", err) // Log the error, but let the caller decide how to react
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
	if s.DbAlias != "" {
		s.DetachFileDB(s.DbAlias)
	}
	log.Printf("Disconnecting from database: %s", s.DbUrl)
	err := s.DB.Close()
	// Clear the singleton so a later New() call (e.g. in another test) gets a
	// fresh, open connection instead of this closed instance.
	if dbInstance == s {
		dbInstance = nil
	}
	return err
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
