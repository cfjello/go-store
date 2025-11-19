package database

import (
	"database/sql"
	"fmt"
	"strings"
)

var stmtMap = map[string]string{
	"MetaInsert": "INSERT OR REPLACE INTO __meta__ (meta_key, schema_key, meta_data) VALUES (?, ?, ?)",
	"MetaSelect": "SELECT meta_data FROM __meta__ WHERE meta_key = ? OR schema_key = ?",
	// MetaSelInit: "SELECT init FROM __meta__ WHERE meta_key = ?",
	// MetaSelLast:  "SELECT meta_data FROM __meta__ WHERE meta_key = ? ORDER BY rowid DESC LIMIT 1",
	"MetaUpdate": "UPDATE __meta__ SET meta_data = ? WHERE meta_key = ?",
	// MetaUpdInit:  "UPDATE meta SET init = ? WHERE meta_key = ?",
	"DataInsert":   "INSERT INTO __data__ (data_id, job_id, meta_key, obj_data) VALUES (?, ?, ?, ? )",
	"DataUpsert":   "INSERT INTO __data__ (data_id, job_id, meta_key, obj_data) VALUES (?, ?, ?, ?)",
	"DataSelect":   "SELECT * FROM __data__ WHERE data_id = ?",
	"DataIdByType": "SELECT data_id FROM __data__ WHERE meta_key = ? and job_id LIKE ?",
	"DataSelLast":  "SELECT data_id FROM __data__ WHERE meta_key = ? ORDER BY data_id DESC LIMIT 1",
	"JobInsert":    "INSERT INTO __job__ (job_id, data_id, job_data) VALUES (?, ?, ? )",
	// "JobUpsert":    "INSERT INTO __job__ (job_id, data_id, job_data) VALUES (?, ?, ?) ON CONFLICT(job_id) DO UPDATE SET data_id = excluded.data_id, job_data = excluded.job_data",
	"JobSelJob": "SELECT * FROM __job__ WHERE job_id = ?",
	"pJob": `INSERT OR REPLACE INTO @@job@@ (job_id, data_id, job_data) 
						select job_id, data_id, job_data from main.job MJ where MJ.job_id = ?`,
	"pJobMeta": `INSERT INTO @@meta@@ (meta_key, schema_key, meta_data) 
						select meta_key, schema_key, meta_data from main.meta MM where MM.meta_key = ( 
							select meta_key from main.data MD where MD.data_id IN ( 
								select data_id from main.job MJ where MJ.job_id = ?
							) 
						) 
					ON CONFLICT(meta_key) DO 
						UPDATE SET schema_key = excluded.schema_key, 
								   meta_data = excluded.meta_data`,
	"pJobData": `INSERT OR REPLACE INTO @@data@@ (data_id, job_id, meta_key, obj_data) 
						select data_id, job_id, meta_key, obj_data 
						from main.data MD where MD.job_id = ?`,
}

func PrepareStmts(db *sql.DB) (map[string]*sql.Stmt, error) {

	PreparedStmts := map[string]*sql.Stmt{}
	prefix := GetPrefix()

	for name, query := range stmtMap {
		memQuery := strings.Replace(strings.Replace(query, "__", "main.", 1), "__", "", 1)
		persistOnly := strings.Contains(memQuery, "@@")
		if !persistOnly {
			memQuery = strings.Replace(memQuery, "@@", prefix, 1)
			stmt, err := db.Prepare(memQuery)
			if err != nil {
				return nil, fmt.Errorf("failed to prepare statement %s: %w", name, err)
			}
			PreparedStmts[name] = stmt
		}
		if prefix != "" && strings.Contains(query, "__") {
			pName := "p" + name
			persistQuery := strings.Replace(strings.Replace(query, "__", prefix+".", 1), "__", "", 1)
			persistStmt, err := db.Prepare(persistQuery)
			if err != nil {
				return nil, fmt.Errorf("failed to prepare statement %s: %w", pName, err)
			}
			PreparedStmts[pName] = persistStmt
		}

		if persistOnly {
			persistQuery := strings.Replace(strings.Replace(query, "@@", prefix+".", 1), "@@", "", 1)
			persistStmt, err := db.Prepare(persistQuery)
			if err != nil {
				return nil, fmt.Errorf("failed to prepare statement %s: %w", name, err)
			}
			PreparedStmts[name] = persistStmt
		}
	}
	return PreparedStmts, nil
}
