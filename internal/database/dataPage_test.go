package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

var pageDB *DBService

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-store-page-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("SQLITE_DB_URL", "file:"+filepath.Join(dir, "test.db"))
	os.Setenv("FILE_DB_ALIAS", "persist")
	pageDB = New(true)
	code := m.Run()
	if pageDB != nil {
		_ = pageDB.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestDataPageSQLRejectsBadSchema(t *testing.T) {
	if _, err := dataPageSQL("persist;drop", false, false); err == nil {
		t.Fatal("expected invalid schema to be rejected")
	}
}

func TestSelectDataPageKeysetAndDedupe(t *testing.T) {
	key := "page-key"
	insert := func(stmt, id, job, body string) {
		t.Helper()
		if _, err := pageDB.SQL[stmt].Exec(id, job, key, body); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("DataInsert", "b", "j1", `{"n":"b"}`)
	insert("DataInsert", "d", "j2", `{"n":"d"}`)
	insert("DataInsert", "a", "j1", `{"n":"a"}`)
	insert("pDataInsert", "a", "j1", `{"n":"a-copy"}`)
	insert("pDataInsert", "c", "j1", `{"n":"c"}`)
	insert("DataInsert", "e", "j9", `{"n":"e"}`)

	page, err := pageDB.SelectDataPage(context.Background(), key, "j1", "", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(page); got != "a,b" {
		t.Fatalf("first page = %s, want a,b", got)
	}
	page, err = pageDB.SelectDataPage(context.Background(), key, "j1", "b", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(page); got != "c" {
		t.Fatalf("second page = %s, want c (published a once, other jobs excluded)", got)
	}

	desc, err := pageDB.SelectDataPage(context.Background(), key, "", "d", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(desc); got != "c,b,a" {
		t.Fatalf("desc page = %s, want c,b,a", got)
	}
}

func idsOf(rows []DataPageRow) string {
	out := ""
	for i, row := range rows {
		if i > 0 {
			out += ","
		}
		out += row.DataID
	}
	return out
}
