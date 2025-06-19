package test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/sql"
	"github.com/cfjello/go-store/pkg/util"
)

func TestStoreAndFetchMetaData(t *testing.T) {
	metaData := KvMetaData{
		Key:       "testKey",
		Oper:      "set",
		Init:      false,
		StoreId:   util.Ulid(),
		JobId:     util.Ulid(),
		SchemaKey: "TEST",
		Check:     false,
		DeleteTs:  util.Ulid(),
	}

	res, err := db.Exec(sql.META_INSERT, metaData.Key, metaData)
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
}
