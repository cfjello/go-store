package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

/*
meta_key  TEXT NOT NULL,
meta_parent_id TEXT,
meta_json JSON NOT NULL,
PRIMARY KEY(meta_key)
*/
var stmtMap = map[string]string{
	"MetaInsert": "INSERT OR REPLACE INTO __meta__ (meta_key, meta_parent_id, meta_json) VALUES (?, ?, ?)",
	"MetaSelect": "SELECT meta_json FROM __meta__ WHERE meta_key = ? OR meta_key = ? LIMIT 1",
	// MetaSelInit: "SELECT init FROM __meta__ WHERE meta_key = ?",
	// MetaSelLast:  "SELECT meta_json FROM __meta__ WHERE meta_key = ? ORDER BY rowid DESC LIMIT 1",
	"MetaUpdate": "UPDATE __meta__ SET meta_json = ? WHERE meta_key = ?",
	// MetaUpdInit:  "UPDATE meta SET init = ? WHERE meta_key = ?",
	"DataInsert":   "INSERT INTO __data__ (data_id, job_id, meta_key, data_json) VALUES (?, ?, ?, ? )",
	"DataUpsert":   "INSERT INTO __data__ (data_id, job_id, meta_key, data_json) VALUES (?, ?, ?, ?)",
	"DataSelect":   "SELECT * FROM __data__ WHERE data_id = ?",
	"DataIdByType": "SELECT data_id FROM __data__ WHERE meta_key = ? and job_id LIKE ?",
	"DataSelLast":  "SELECT data_id FROM __data__ WHERE meta_key = ? ORDER BY data_id DESC LIMIT 1",
	"JobInsert":    "INSERT INTO __job__ (job_id, meta_key, job_json) VALUES (?, ?, ? )",
	// "JobUpsert":    "INSERT INTO __job__ (job_id, data_id, job_json) VALUES (?, ?, ?) ON CONFLICT(job_id) DO UPDATE SET data_id = excluded.data_id, job_json = excluded.job_json",
	"JobSelect":  "SELECT * FROM __job__ WHERE job_id = ?",
	"JobSelLast": "SELECT job_id FROM __job__ WHERE meta_key = ? ORDER BY job_id DESC LIMIT 1",
	"JobJson":    "SELECT job_id, meta_key, job_json FROM __job__  WHERE job_id = ( SELECT max(job_id) FROM __data__ WHERE meta_key = ? ) ",
	"pJob": `INSERT OR REPLACE INTO @@job@@ (job_id, meta_key, job_json) 
						select job_id, meta_key, job_json from main.job MJ where MJ.job_id = ?`,
	"pJobMeta": `INSERT INTO @@meta@@ (meta_key, meta_parent_id, meta_json)
						select meta_key, meta_parent_id, meta_json from main.meta MM where MM.meta_key = (
							select meta_key from main.data MD where MD.data_id IN (
								select data_id from main.job MJ where MJ.job_id = ?
							)
						)
					ON CONFLICT(meta_key) DO
						UPDATE SET meta_parent_id = excluded.meta_parent_id,
								   meta_json = excluded.meta_json`,
	"pJobData": `INSERT OR REPLACE INTO @@data@@ (data_id, job_id, meta_key, data_json) 
						select data_id, job_id, meta_key, data_json 
						from main.data MD where MD.job_id = ?`,
}

func PrepareStmts(db *sql.DB) (map[string]*sql.Stmt, error) {

	PreparedStmts := map[string]*sql.Stmt{}
	prefix := GetPrefix()

	for name, query := range stmtMap {
		persistOnly := strings.Contains(query, "@@")
		if !persistOnly {
			stmt, err := db.Prepare(expandTokens(query, "main", prefix))
			if err != nil {
				return nil, fmt.Errorf("failed to prepare statement %s: %w", name, err)
			}
			PreparedStmts[name] = stmt
		}
		if prefix != "" && (persistOnly || strings.Contains(query, "__")) {
			pName := name
			if !persistOnly {
				pName = "p" + name
			}
			persistStmt, err := db.Prepare(expandTokens(query, prefix, prefix))
			if err != nil {
				return nil, fmt.Errorf("failed to prepare statement %s: %w", pName, err)
			}
			PreparedStmts[pName] = persistStmt
		}
	}
	return PreparedStmts, nil
}

var (
	memTokenRe     = regexp.MustCompile(`__(\w+)__`)
	persistTokenRe = regexp.MustCompile(`@@(\w+)@@`)
)

// expandTokens replaces all table tokens in a query:
// __name__ becomes memSchema.name and @@name@@ becomes persistSchema.name.
func expandTokens(query, memSchema, persistSchema string) string {
	q := persistTokenRe.ReplaceAllString(query, persistSchema+".$1")
	return memTokenRe.ReplaceAllString(q, memSchema+".$1")
}
