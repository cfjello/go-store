package database

import (
	"database/sql"
	"os"
	"reflect"
	"strings"

	"github.com/cfjello/go-store/pkg/util"
	// _ "github.com/mattn/go-sqlite3"
)

func initDB(db *sql.DB, reset bool, alias string) error {
	// Drop existing tables and create new ones
	err := dropTables(db, reset, alias)
	if err != nil {
		return err
	}

	err = createTables(db, alias)
	if err != nil {
		return err
	}
	err = CreateIndexes(db, alias)
	if err != nil {
		return err
	}
	return nil
}

var CreateStmtSql = struct {
	Meta string
	// Deps  string
	Data  string
	Graph string
	Job   string
}{
	Meta: `
		meta (
			meta_key  TEXT NOT NULL,
			meta_parent_id TEXT,
			meta_json JSON NOT NULL,
			PRIMARY KEY(meta_key)
		)`,
	/*
		Deps: `
			deps (
				deps_id  TEXT,
				upper_meta_key TEXT NOT NULL,
				lower_meta_key TEXT NOT NULL,
				PRIMARY KEY(deps_id)
		)`,
	*/
	Data: `
		data (
			data_id TEXT,
			meta_key TEXT NOT NULL,
			job_id TEXT,
			data_json JSON NOT NULL,
			PRIMARY KEY(data_id, job_id)
		)`,

	Graph: `
			graph (
				graph_id TEXT,
				root_node TEXT NOT NULL,
				graph_json JSON NOT NULL,
				PRIMARY KEY(graph_id)
			)`,
	Job: `
			job (
				job_id TEXT NOT NULL,
				-- job_name TEXT,
				meta_key TEXT REFERENCES data(meta_key),
				job_json JSON NOT NULL,
				PRIMARY KEY(job_id, meta_key)
			)`,
}

// GetPrefix returns the FILE_DB_ALIAS environment value. Deprecated inside
// this package: schema init and statement preparation receive the alias
// directly from New, and methods use DBService.DbAlias.
func GetPrefix() string {
	envSourced := os.Getenv("ENV_SOURCED")
	if envSourced != "true" {
		util.SetEnv() // Load default environment variables
	}
	return os.Getenv("FILE_DB_ALIAS")
}

// createTables creates the main schema tables and, when alias names an
// attached persist database, the persist schema tables. Creation uses
// IF NOT EXISTS and is unconditional: unlike drops it cannot lose data, and
// a file main database must still have its schema after a restart.
func createTables(db *sql.DB, alias string) error {
	// Create tables using the CreateStmtSql struct
	statements := []string{
		CreateStmtSql.Meta,
		CreateStmtSql.Data,
		CreateStmtSql.Graph,
		CreateStmtSql.Job,
	}

	for _, stmt := range statements {
		_, err := db.Exec("CREATE TABLE IF NOT EXISTS main." + stmt)
		if err != nil {
			return err
		}
		if alias != "" {
			_, err := db.Exec("CREATE TABLE IF NOT EXISTS " + alias + "." + stmt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateIndexes creates the main schema indexes and, when alias names an
// attached persist database, the persist schema indexes.
func CreateIndexes(db *sql.DB, alias string) error {
	// Create indexes for data table
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_data_job_id ON data(job_id)`)
	if err != nil {
		return err
	}
	if alias != "" {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + alias + `.idx_data_job_id ON data(job_id)`)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_data_meta_key ON data(meta_key)`)
	if err != nil {
		return err
	}

	if alias != "" {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + alias + `.idx_data_meta_key ON data(meta_key)`)
		if err != nil {
			return err
		}
	}

	// Job Indexes
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_job_meta_key ON job(meta_key)`)
	if err != nil {
		return err
	}

	if alias != "" {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + alias + `.idx_job_meta_key ON job(meta_key)`)
		if err != nil {
			return err
		}
	}

	return nil
}

// dropTables drops tables only when reset is true. Drops must never run
// unconditionally: the main database may be a persistent file database.
func dropTables(db *sql.DB, reset bool, alias string) error {
	if !reset {
		return nil
	}

	// Get table names dynamically using reflection on CreateStmtSql struct
	v := reflect.ValueOf(CreateStmtSql)
	t := reflect.TypeOf(CreateStmtSql)

	var tables []string
	for i := 0; i < v.NumField(); i++ {
		fieldName := strings.ToLower(t.Field(i).Name)
		tables = append(tables, fieldName)
	}

	for _, table := range tables {
		_, err := db.Exec("DROP TABLE IF EXISTS main." + table)
		if err != nil {
			return err
		}
		if alias != "" {
			_, err := db.Exec("DROP TABLE IF EXISTS " + alias + "." + table)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
