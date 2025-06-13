package database

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB(dbPath string) error {
	var err error
	// If dbPath is empty, use in-memory database
	if dbPath == "" {
		dbPath = ":memory:"
	}

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}

	// Drop existing tables and create new ones
	dropTables(db)
	createTables(db)

	return nil
}

func createTables(db *sql.DB) error {
	// Create data table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS data (
			data_id TEXT,
			job_id TEXT,
			obj_type TEXT,
			obj_data JSON NOT NULL,
			meta_data JSON NOT NULL,
			PRIMARY KEY(data_id)
		)
	`)
	if err != nil {
		return err
	}

	// Create job_graph table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS job_graph (
			graph_id TEXT,
			top_node TEXT NOT NULL,
			graph_data JSON NOT NULL,
			PRIMARY KEY(graph_id)
		)
	`)
	if err != nil {
		return err
	}

	// Create meta table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (
			meta_type TEXT,
			meta_data JSON NOT NULL,
			PRIMARY KEY(meta_type)
		)
	`)
	if err != nil {
		return err
	}

	// Create job table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS job (
			job_id TEXT NOT NULL,
			data_id TEXT REFERENCES data(data_id),
			job_data JSON NOT NULL,
			PRIMARY KEY(job_id, data_id)
		)
	`)
	if err != nil {
		return err
	}

	return nil
}

func dropTables(db *sql.DB) error {
	tables := []string{"data", "job", "job_graph", "meta"}

	for _, table := range tables {
		_, err := db.Exec("DROP TABLE IF EXISTS " + table)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetDB() *sql.DB {
	return db
}
