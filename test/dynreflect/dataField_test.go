package dynReflect_test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/dynReflect"
)

type payload struct {
	N int `json:"n"`
}

type withData struct {
	Data payload `json:"data"`
	Skip string  `json:"skip"`
}

type withJSONTag struct {
	Body payload `json:"data"`
}

type staticObj struct {
	N int `json:"n"`
}

func TestDataValueStructField(t *testing.T) {
	obj := &withData{Data: payload{N: 7}, Skip: "nope"}
	got, ok := dynReflect.DataValue(obj, "data")
	if !ok {
		t.Fatal("expected data field")
	}
	p, ok := got.(payload)
	if !ok || p.N != 7 {
		t.Fatalf("got %#v", got)
	}
}

func TestDataValueJSONTag(t *testing.T) {
	obj := withJSONTag{Body: payload{N: 3}}
	got, ok := dynReflect.DataValue(obj, "data")
	if !ok {
		t.Fatal("expected json-tagged field")
	}
	if got.(payload).N != 3 {
		t.Fatalf("got %#v", got)
	}
}

func TestDataValueMissing(t *testing.T) {
	if _, ok := dynReflect.DataValue(&staticObj{N: 1}, "data"); ok {
		t.Fatal("static object has no data field")
	}
	if _, ok := dynReflect.DataValue("solo", "data"); ok {
		t.Fatal("non-struct should not match")
	}
}

func TestDataValueMapIndex(t *testing.T) {
	obj := map[string]any{"data": payload{N: 5}, "skip": "nope"}
	got, ok := dynReflect.DataValue(obj, "data")
	if !ok {
		t.Fatal("expected map data key")
	}
	if got.(payload).N != 5 {
		t.Fatalf("got %#v", got)
	}
}

func TestDataFieldPtrRoundTrip(t *testing.T) {
	obj := &withData{Skip: "keep"}
	ptr, ok := dynReflect.DataFieldPtr(obj, "data")
	if !ok {
		t.Fatal("expected addressable data field")
	}
	p := ptr.(*payload)
	p.N = 11
	if obj.Data.N != 11 {
		t.Fatalf("Data.N = %d", obj.Data.N)
	}
}
