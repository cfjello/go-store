package database

import (
	"database/sql"
)

type SqlStmt struct {
	MetaInsert   string
	MetaSelect   string
	MetaSelLast  string
	MetaUpdate   string
	DataInsert   string
	DataSelect   string
	DataIdByType string
	DataSelLast  string
	JobInsert    string
	JobSelJob    string

	db               *sql.DB
	dataInsStmt      *sql.Stmt
	dataSelStmt      *sql.Stmt
	dataIdByTypeStmt *sql.Stmt
	dataSelLastStmt  *sql.Stmt
	metaInsStmt      *sql.Stmt
	metaSelStmt      *sql.Stmt
	metaSelLastStmt  *sql.Stmt
	metaUpdStmt      *sql.Stmt
	jobInsStmt       *sql.Stmt
	jobSelAllStmt    *sql.Stmt
}

func NewSqlStmt(db *sql.DB) (*SqlStmt, error) {
	s := &SqlStmt{
		MetaInsert:   "INSERT INTO meta (meta_type, meta_data) VALUES (?, ?) on conflict(meta_type) do update set meta_data = excluded.meta_data",
		MetaSelect:   "SELECT meta_data FROM meta WHERE meta_type = ?",
		MetaSelLast:  "SELECT meta_data FROM meta WHERE meta_type = ? ORDER BY rowid DESC LIMIT 1",
		MetaUpdate:   "UPDATE meta SET meta_data = ? WHERE meta_type = ?",
		DataInsert:   "INSERT INTO data (data_id, job_id, obj_type, obj_data, meta_data) VALUES (?, ?, ?, ?, ? )",
		DataSelect:   "SELECT * FROM data WHERE data_id = ?",
		DataIdByType: "SELECT data_id FROM data WHERE obj_type = ? and job_id LIKE ?",
		DataSelLast:  "SELECT * FROM data ORDER BY rowid DESC LIMIT 1",
		JobInsert:    "INSERT INTO job (job_id, data_id, job_data) VALUES (?, ?, ? )",
		JobSelJob:    "SELECT * FROM job WHERE job_id = ?",
		db:           db,
	}

	var err error

	s.dataInsStmt, err = db.Prepare(s.DataInsert)
	if err != nil {
		return nil, err
	}
	s.dataSelStmt, err = db.Prepare(s.DataSelect)
	if err != nil {
		return nil, err
	}
	s.dataIdByTypeStmt, err = db.Prepare(s.DataIdByType)
	if err != nil {
		return nil, err
	}
	s.dataSelLastStmt, err = db.Prepare(s.DataSelLast)
	if err != nil {
		return nil, err
	}
	s.metaInsStmt, err = db.Prepare(s.MetaInsert)
	if err != nil {
		return nil, err
	}
	s.metaSelStmt, err = db.Prepare(s.MetaSelect)
	if err != nil {
		return nil, err
	}
	s.metaSelLastStmt, err = db.Prepare(s.MetaSelLast)
	if err != nil {
		return nil, err
	}
	s.metaUpdStmt, err = db.Prepare(s.MetaUpdate)
	if err != nil {
		return nil, err
	}
	s.jobInsStmt, err = db.Prepare(s.JobInsert)
	if err != nil {
		return nil, err
	}
	s.jobSelAllStmt, err = db.Prepare(s.JobSelJob)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *SqlStmt) CheckTables() bool {
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='table';")
	if err != nil {
		return false
	}
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false
		}
		tables[name] = true
	}

	return tables["data"] && tables["job"] && tables["meta"] && tables["job_graph"]
}
