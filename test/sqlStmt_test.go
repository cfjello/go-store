package test

import (
	"path/filepath"
	"testing"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/types"

	_ "github.com/mattn/go-sqlite3"
)

// TestMetaDataStmts verifies that the SQL statements are prepared successfully
// against a real database and that a meta data row can be inserted and read back.
//
// SQLITE_DB_URL is pointed at a temporary file so the test never touches the
// real development database (database.New would otherwise attach and, with
// reset=true, drop tables in the default file DB).
func TestMetaDataStmts(t *testing.T) {
	t.Setenv("SQLITE_DB_URL", "file:"+filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("FILE_DB_ALIAS", "persist")

	svc := database.New(true)
	defer svc.Close()

	for _, name := range []string{"MetaInsert", "MetaSelect", "DataInsert", "JobInsert"} {
		if svc.SQL[name] == nil {
			t.Fatalf("Expected prepared statement %q to exist", name)
		}
	}

	metaKey := "testKey"
	metaParent := "testParent"
	metaJSON := `{"key":"testKey","oper":"set"}`

	// MetaInsert: (meta_key, meta_parent_id, meta_json)
	res, err := svc.SQL["MetaInsert"].Exec(metaKey, metaParent, metaJSON)
	if err != nil {
		t.Fatal("Could not insert Meta Data:", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		t.Fatal("Failed to get rows affected:", err)
	}
	if rowsAffected != 1 {
		t.Errorf("Meta Data insert should affect 1 row, got %d", rowsAffected)
	}

	// MetaSelect: (meta_key, schemaKey)
	var fetched string
	err = svc.SQL["MetaSelect"].QueryRow(metaKey, metaKey).Scan(&fetched)
	if err != nil {
		t.Fatal("Could not fetch Meta Data:", err)
	}
	if fetched != metaJSON {
		t.Errorf("Fetched meta_json = %s, want %s", fetched, metaJSON)
	}
}

// TestGetMaxDataIDs verifies the dynamic IN-clause query that returns the
// maximum data_id per meta_key for a variable-length list of meta_keys.
func TestGetMaxDataIDs(t *testing.T) {
	t.Setenv("SQLITE_DB_URL", "file:"+filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("FILE_DB_ALIAS", "persist")

	svc := database.New(true)
	defer svc.Close()

	// Insert data rows across three meta_keys; two of them get multiple rows.
	insert := func(dataID, jobID, metaID string) {
		t.Helper()
		if _, err := svc.SQL["DataInsert"].Exec(dataID, jobID, metaID, `{"v":1}`); err != nil {
			t.Fatalf("Could not insert data %s: %v", dataID, err)
		}
	}
	insert("d1", "j1", "m1")
	insert("d9", "j1", "m1") // max for m1
	insert("d2", "j1", "m2") // only row for m2
	insert("d3", "j1", "m3")
	insert("d8", "j1", "m3") // max for m3

	got, err := svc.GetMaxDataIDs([]string{"m1", "m2", "m3"}, false)
	if err != nil {
		t.Fatalf("GetMaxDataIDs failed: %v", err)
	}
	want := map[string]string{"m1": "d9", "m2": "d2", "m3": "d8"}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d (%v)", len(want), len(got), got)
	}
	for metaID, wantDataID := range want {
		if got[metaID] != wantDataID {
			t.Errorf("meta_key %s: got max data_id %q, want %q", metaID, got[metaID], wantDataID)
		}
	}

	// Variable length: single id and empty list.
	single, err := svc.GetMaxDataIDs([]string{"m2"}, false)
	if err != nil || len(single) != 1 || single["m2"] != "d2" {
		t.Errorf("single-id query got %v, err %v", single, err)
	}
	empty, err := svc.GetMaxDataIDs(nil, false)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty list should return empty map and no error, got %v, err %v", empty, err)
	}
}

// TestSetDataIndexesByObjectKey verifies that a version is stored under the
// object key (not the schema parent), that a reused job id does not collide,
// and that a row written only to the file database is still found.
func TestSetDataIndexesByObjectKey(t *testing.T) {
	t.Setenv("SQLITE_DB_URL", "file:"+filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("FILE_DB_ALIAS", "persist")

	svc := database.New(true)
	defer svc.Close()

	key := "maker-1"
	args := types.SetArgs{
		Key:       key,
		Object:    map[string]any{"status": "off"},
		JobId:     "job-1",
		ParentKey: "CoffeeMaker",
		Publish:   true,
	}
	if !svc.SetData(key, args) {
		t.Fatal("first SetData failed")
	}
	args.Object = map[string]any{"status": "on"}
	if !svc.SetData(key, args) {
		t.Fatal("second SetData with the same job id failed")
	}

	storeID, err := svc.GetCurrStoreID(key)
	if err != nil {
		t.Fatalf("GetCurrStoreID: %v", err)
	}
	if storeID == "" || storeID == key {
		t.Fatalf("store id = %q, want a generated id distinct from the object key", storeID)
	}

	var got map[string]any
	if _, err := svc.GetData(storeID, &got); err != nil {
		t.Fatalf("GetData: %v", err)
	}
	if got["status"] != "on" {
		t.Fatalf("status = %v, want on", got["status"])
	}

	persistKey := "persist-only"
	persistID := "01PERSISTONLYSTOREID000000"
	if _, err := svc.SQL["pDataInsert"].Exec(persistID, "j", persistKey, `{"status":"disk"}`); err != nil {
		t.Fatalf("persist insert: %v", err)
	}
	gotID, err := svc.GetCurrStoreID(persistKey)
	if err != nil {
		t.Fatalf("GetCurrStoreID persist fallback: %v", err)
	}
	if gotID != persistID {
		t.Fatalf("persist store id = %q, want %q", gotID, persistID)
	}
}
