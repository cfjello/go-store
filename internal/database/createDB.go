package database

import (
	_ "github.com/mattn/go-sqlite3"
)

func (s *service) initDB() error {
	// Drop existing tables and create new ones
	err := s.dropTables()
	if err != nil {
		return err
	}
	err = s.createTables()
	if err != nil {
		return err
	}
	return nil
}

func (s *service) createTables() error {
	// Create data table
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS data (
			data_id TEXT,
			job_id TEXT,
			obj_type TEXT,
			obj_data JSON NOT NULL,
			meta_data JSON NOT NULL,
			PRIMARY KEY(data_id )
		)
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_data_job_id ON data(job_id)
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_data_obj_type ON data(obj_type)
	`)
	if err != nil {
		return err
	}

	// Create job_graph table
	_, err = s.db.Exec(`
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
	_, err = s.db.Exec(`
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
	_, err = s.db.Exec(`
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

func (s *service) dropTables() error {
	tables := []string{"data", "job", "job_graph", "meta"}

	for _, table := range tables {
		_, err := s.db.Exec("DROP TABLE IF EXISTS " + table)
		if err != nil {
			return err
		}
	}

	return nil
}
