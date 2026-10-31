package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

type dataPayload struct {
	N int `json:"n"`
}

type updatableObj struct {
	Data dataPayload `json:"data"`
	Skip string      `json:"skip"`
}

func (o *updatableObj) SetData() error { return nil }

type staticPlain struct {
	N int `json:"n"`
}

func TestRegisterWithoutUpdMethodSetsFixed(t *testing.T) {
	key := keyOf(t)
	meta, err := testStore.Register(types.RegisterArgs{
		Key:    key,
		Object: &staticPlain{N: 1},
		Fixed:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Fixed {
		t.Fatal("static object without SetData should be Fixed")
	}
	got, err := testStore.GetMeta(key)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Fixed {
		t.Fatal("persisted metadata should be Fixed")
	}
}

type updNoData struct{}

func (o *updNoData) SetData() error { return nil }

func TestRegisterUpdMethodRequiresDataField(t *testing.T) {
	_, err := testStore.Register(types.RegisterArgs{
		Key:    keyOf(t),
		Object: &updNoData{},
	})
	if err == nil {
		t.Fatal("expected Register to reject SetData without a data field")
	}
}

func TestRegisterWithUpdMethodKeepsFixed(t *testing.T) {
	key := keyOf(t)
	meta, err := testStore.Register(types.RegisterArgs{
		Key:    key,
		Object: &updatableObj{Data: dataPayload{N: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Fixed {
		t.Fatal("object with SetData should not be forced Fixed")
	}
}

func TestSetStoresDataFieldNotWrapper(t *testing.T) {
	key := keyOf(t)
	obj := &updatableObj{Data: dataPayload{N: 9}, Skip: "secret"}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Init: true}); err != nil {
		t.Fatal(err)
	}

	var payload dataPayload
	if _, err := testStore.Get("", key, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.N != 9 {
		t.Fatalf("payload.N = %d, want 9", payload.N)
	}

	var wrap updatableObj
	got, err := testStore.Get("", key, &wrap)
	if err != nil {
		t.Fatal(err)
	}
	if wrap.Data.N != 9 {
		t.Fatalf("wrap.Data.N = %d, want 9", wrap.Data.N)
	}
	if p, ok := got.(*dataPayload); !ok || p != &wrap.Data || p.N != 9 {
		t.Fatalf("Get returned %#v, want pointer to wrap.Data", got)
	}
	if wrap.Skip != "" {
		t.Fatalf("wrapper-only field was persisted: %q", wrap.Skip)
	}

	id, err := testStore.GetCurrStoreID(key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := testStore.DB.GetData(id, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"n":9}` {
		t.Fatalf("stored json = %s, want {\"n\":9}", b)
	}
}

func TestStaticObjectRoundTrip(t *testing.T) {
	key := keyOf(t)
	if _, err := testStore.Register(types.RegisterArgs{
		Key:    key,
		Object: &staticPlain{N: 4},
		Init:   true,
	}); err != nil {
		t.Fatal(err)
	}
	var got staticPlain
	if _, err := testStore.Get("", key, &got); err != nil {
		t.Fatal(err)
	}
	if got.N != 4 {
		t.Fatalf("N = %d, want 4", got.N)
	}
}

func TestRegisterTypeInfoUsesDataField(t *testing.T) {
	key := keyOf(t)
	obj := &updatableObj{Data: dataPayload{N: 1}, Skip: "ignored"}
	meta, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj})
	if err != nil {
		t.Fatal(err)
	}
	if meta.TypeInfo.Type != "store_test.dataPayload" {
		t.Fatalf("TypeInfo.Type = %q, want store_test.dataPayload", meta.TypeInfo.Type)
	}
	if _, ok := meta.TypeInfo.Fields["Skip"]; ok {
		t.Fatal("TypeInfo should describe the data field, not the wrapper")
	}
}

func TestRegisterTypeInfoUsesWholeStaticObject(t *testing.T) {
	key := keyOf(t)
	obj := &staticPlain{N: 1}
	meta, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj})
	if err != nil {
		t.Fatal(err)
	}
	if meta.TypeInfo.Type != "*store_test.staticPlain" {
		t.Fatalf("TypeInfo.Type = %q, want *store_test.staticPlain", meta.TypeInfo.Type)
	}
}

func TestSetCheckRejectsPayloadTypeMismatch(t *testing.T) {
	key := keyOf(t)
	obj := &updatableObj{Data: dataPayload{N: 1}}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Set(types.SetArgs{
		Key:    key,
		Object: &staticPlain{N: 2},
		Check:  types.CheckOn,
	}); err == nil {
		t.Fatal("expected type mismatch for a static object written over a data payload")
	}
}

func TestSetCheckAcceptsMatchingPayloadType(t *testing.T) {
	key := keyOf(t)
	obj := &updatableObj{Data: dataPayload{N: 1}}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Set(types.SetArgs{
		Key:    key,
		Object: dataPayload{N: 3},
		Check:  types.CheckOn,
	}); err != nil {
		t.Fatalf("same data payload type should pass: %v", err)
	}
	if _, err := testStore.Set(types.SetArgs{
		Key:    key,
		Object: &updatableObj{Data: dataPayload{N: 4}},
		Check:  types.CheckOn,
	}); err != nil {
		t.Fatalf("wrapper with the same data field type should pass: %v", err)
	}
}

func TestSetCheckSkipAllowsTypeMismatch(t *testing.T) {
	key := keyOf(t)
	obj := &updatableObj{Data: dataPayload{N: 1}}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.Set(types.SetArgs{
		Key:    key,
		Object: &staticPlain{N: 2},
		Check:  types.CheckSkip,
	}); err != nil {
		t.Fatalf("CheckSkip should bypass type check: %v", err)
	}
}

func TestIterWalksDataFieldCollection(t *testing.T) {
	key := keyOf(t)
	type listed struct {
		Data []string `json:"data"`
	}
	obj := &listed{Data: []string{"a", "b"}}
	if _, err := testStore.Register(types.RegisterArgs{Key: key, Object: obj, Init: true}); err != nil {
		t.Fatal(err)
	}
	it, err := testStore.OpenIter(store.OpenOpts{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for {
		el, ok := it.Next(context.Background())
		if !ok {
			break
		}
		got = append(got, el.Value.(string))
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("iterated %v, want [a b]", got)
	}
}
