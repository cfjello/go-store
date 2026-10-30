package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DataPageRow is one version row from a keyset page. DataID is the store id.
type DataPageRow struct {
	DataID   string
	MetaKey  string
	JobID    string
	DataJSON []byte
}

// SelectDataPage returns the next page of versions for metaKey. afterID is the
// last data_id already returned; empty means the first page. Rows are ordered
// by data_id (ascending unless desc). jobID, when set, keeps only that job.
//
// Memory and the attached file database are both read. A data_id present in
// memory is not also taken from the file, so a published copy is returned once.
// Each side contributes at most limit rows; the merged page is the next limit
// unique ids. The caller does not hold a SQLite cursor between pages.
func (s *DBService) SelectDataPage(ctx context.Context, metaKey, jobID, afterID string, limit int, desc bool) ([]DataPageRow, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is not configured")
	}
	if metaKey == "" {
		return nil, errors.New("meta key is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	mem, err := s.queryDataPage(ctx, "main", metaKey, jobID, afterID, limit, desc, false)
	if err != nil {
		return nil, err
	}
	var disk []DataPageRow
	if s.DbAlias != "" {
		disk, err = s.queryDataPage(ctx, s.DbAlias, metaKey, jobID, afterID, limit, desc, true)
		if err != nil {
			return nil, err
		}
	}
	return mergeDataPages(mem, disk, limit, desc), nil
}

func (s *DBService) queryDataPage(ctx context.Context, schema, metaKey, jobID, afterID string, limit int, desc, excludeMain bool) ([]DataPageRow, error) {
	query, err := dataPageSQL(schema, desc, excludeMain)
	if err != nil {
		return nil, err
	}
	args := []any{metaKey, jobID, jobID, afterID, afterID}
	if excludeMain {
		args = append(args, metaKey)
	}
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("data page %s: %w", schema, err)
	}
	defer rows.Close()

	var out []DataPageRow
	for rows.Next() {
		var row DataPageRow
		if err := rows.Scan(&row.DataID, &row.MetaKey, &row.JobID, &row.DataJSON); err != nil {
			return nil, fmt.Errorf("data page %s: %w", schema, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data page %s: %w", schema, err)
	}
	return out, nil
}

// mergeDataPages takes the next limit unique data_ids from two ordered pages.
// Memory wins when the same id is on both sides.
func mergeDataPages(mem, disk []DataPageRow, limit int, desc bool) []DataPageRow {
	out := make([]DataPageRow, 0, limit)
	seen := make(map[string]struct{}, limit)
	i, j := 0, 0
	for len(out) < limit && (i < len(mem) || j < len(disk)) {
		takeMem := j >= len(disk) || (i < len(mem) && (mem[i].DataID == disk[j].DataID || dataIDBefore(mem[i].DataID, disk[j].DataID, desc)))
		var row DataPageRow
		if takeMem {
			row = mem[i]
			i++
		} else {
			row = disk[j]
			j++
		}
		if _, ok := seen[row.DataID]; ok {
			continue
		}
		seen[row.DataID] = struct{}{}
		out = append(out, row)
	}
	return out
}

func dataIDBefore(a, b string, desc bool) bool {
	if desc {
		return a > b
	}
	return a < b
}

// GetDataRow reads one data row by store id, memory first, then the attached
// file database. The returned meta key lets a caller reject a store id that
// belongs to a different object.
func (s *DBService) GetDataRow(ctx context.Context, storeID string) (metaKey, jobID string, raw []byte, err error) {
	if s == nil || s.DB == nil {
		return "", "", nil, errors.New("database is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	metaKey, jobID, raw, err = scanDataRow(ctx, s.SQL["DataSelect"], storeID)
	if err == sql.ErrNoRows {
		metaKey, jobID, raw, err = scanDataRow(ctx, s.SQL["pDataSelect"], storeID)
	}
	return metaKey, jobID, raw, err
}

func scanDataRow(ctx context.Context, stmt *sql.Stmt, storeID string) (metaKey, jobID string, raw []byte, err error) {
	if stmt == nil {
		return "", "", nil, sql.ErrNoRows
	}
	var dataID string
	err = stmt.QueryRowContext(ctx, storeID).Scan(&dataID, &metaKey, &jobID, &raw)
	return metaKey, jobID, raw, err
}
