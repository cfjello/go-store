package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cfjello/go-store/pkg/perf"
)

// activeMetaFilter matches metadata rows that are not soft-deleted: the
// "deleted" JSON field is absent or still the initial "0000" marker.
const activeMetaFilter = `COALESCE(json_extract(meta_json, '$.deleted'), '0000') = '0000'`

// ListKeys returns up to limit registered meta keys in ascending key order,
// starting strictly after cursor (pass "" for the first page). prefix, when
// non-empty, restricts the result to keys beginning with it. includeDeleted
// also lists soft-deleted keys. Only the main schema is listed: metadata is
// always written to main, and persist-schema copies are archives.
func (s *DBService) ListKeys(prefix string, includeDeleted bool, limit int, cursor string) ([]string, error) {
	defer perf.Track("db.ListKeys")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT meta_key FROM main.meta WHERE (? = '' OR meta_key > ?)`
	args := []any{cursor, cursor}
	if prefix != "" {
		query += ` AND meta_key LIKE ? ESCAPE '\'`
		args = append(args, likePrefix(prefix)+"%")
	}
	if !includeDeleted {
		query += ` AND ` + activeMetaFilter
	}
	query += ` ORDER BY meta_key ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	defer rows.Close()

	keys := make([]string, 0, limit)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan key row: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// ListVersions returns up to limit store IDs (data_id) for key, newest first
// (ULIDs are time-ordered), starting strictly before cursor (pass "" for the
// newest page). When a persist layer is attached, versions present only there
// are included; duplicates copied to both schemas are returned once.
func (s *DBService) ListVersions(key string, limit int, cursor string) ([]string, error) {
	defer perf.Track("db.ListVersions")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query, args, err := versionPageSQL(s.DbAlias, key, limit, cursor)
	if err != nil {
		return nil, err
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions for key %q: %w", key, err)
	}
	defer rows.Close()

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan version row: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// versionPageSQL builds the keyset-paginated version query. Without an alias
// only main.data is read; with an alias both schemas are unioned. Returns the
// query and its args (meta_key/cursor repeated per branch, limit last).
func versionPageSQL(alias, key string, limit int, cursor string) (string, []any, error) {
	branch := `SELECT data_id FROM %s.data WHERE meta_key = ? AND (? = '' OR data_id < ?)`
	main := fmt.Sprintf(branch, "main")
	if alias == "" {
		return main + ` ORDER BY data_id DESC LIMIT ?`, []any{key, cursor, cursor, limit}, nil
	}
	if !schemaIdentRe.MatchString(alias) {
		return "", nil, fmt.Errorf("invalid persist alias %q", alias)
	}
	persist := fmt.Sprintf(branch, alias)
	query := fmt.Sprintf(`SELECT data_id FROM (%s UNION %s) ORDER BY data_id DESC LIMIT ?`, main, persist)
	return query, []any{key, cursor, cursor, key, cursor, cursor, limit}, nil
}

// likePrefix escapes the LIKE wildcard characters so prefix matches literally
// under ESCAPE '\'.
func likePrefix(prefix string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(prefix)
}
