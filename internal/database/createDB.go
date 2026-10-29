package database

import (
	"database/sql"
	"os"
	"reflect"
	"strings"

	"github.com/cfjello/go-store/pkg/util"
	// _ "github.com/mattn/go-sqlite3"
)

func initDB(db *sql.DB, reset bool) error {
	// Drop existing tables and create new ones
	err := dropTables(db, reset)
	if err != nil {
		return err
	}

	err = createTables(db, reset)
	if err != nil {
		return err
	}
	err = CreateIndexes(db, reset)
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

func GetPrefix() string {
	envSourced := os.Getenv("ENV_SOURCED")
	if envSourced != "true" {
		util.SetEnv() // Load default environment variables
	}
	return os.Getenv("FILE_DB_ALIAS")
}

func createTables(db *sql.DB, reset bool) error {

	prefix := GetPrefix()

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
		if prefix != "" && reset {
			// Create prefixed tables if reset is true
			_, err := db.Exec("CREATE TABLE IF NOT EXISTS " + prefix + "." + stmt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func CreateIndexes(db *sql.DB, reset bool) error {
	prefix := GetPrefix()
	// Create indexes for data table
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_data_job_id ON data(job_id)`)
	if err != nil {
		return err
	}
	if reset {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + prefix + `.idx_data_job_id ON data(job_id)`)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_data_meta_key ON data(meta_key)`)
	if err != nil {
		return err
	}

	if reset {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + prefix + `.idx_data_meta_key ON data(meta_key)`)
		if err != nil {
			return err
		}
	}

	// Job Indexes
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS main.idx_job_meta_key ON job(meta_key)`)
	if err != nil {
		return err
	}

	if reset {
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + prefix + `.idx_job_meta_key ON job(meta_key)`)
		if err != nil {
			return err
		}
	}

	return nil
}

func dropTables(db *sql.DB, reset bool) error {
	prefix := GetPrefix()

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
		if reset {
			_, err := db.Exec("DROP TABLE IF EXISTS " + prefix + "." + table)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
