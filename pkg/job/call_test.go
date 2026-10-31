package job

import (
	"errors"
	"testing"
)

type setDataObj struct {
	Data  struct{} `json:"data"`
	calls int
	err   error
}

func (o *setDataObj) SetData() error {
	o.calls++
	return o.err
}

func (o *setDataObj) WithArg(n int) {}

func TestCallNoArgMethod(t *testing.T) {
	okObj := &setDataObj{}
	if err := callNoArgMethod(okObj, "SetData"); err != nil {
		t.Fatalf("SetData: %v", err)
	}
	if okObj.calls != 1 {
		t.Fatalf("SetData calls = %d, want 1", okObj.calls)
	}

	failObj := &setDataObj{err: errors.New("update failed")}
	if err := callNoArgMethod(failObj, "SetData"); err == nil || err.Error() != "update failed" {
		t.Fatalf("SetData error = %v, want update failed", err)
	}

	if err := callNoArgMethod(okObj, "Missing"); err == nil {
		t.Fatal("expected missing method to fail")
	}
	if err := callNoArgMethod(okObj, "WithArg"); err == nil {
		t.Fatal("expected a method that requires arguments to fail")
	}
}
