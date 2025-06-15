package test

import (
	"testing"
)

func TestStoreAndFetchMetaData(t *testing.T) {
	metaData := KvMetaData{
		Key:       monotonicUlid(),
		Oper:      "set",
		Init:      false,
		StoreId:   monotonicUlid(),
		JobId:     monotonicUlid(),
		SchemaKey: "TEST",
		Check:     false,
		DeleteTs:  monotonicUlid(),
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
