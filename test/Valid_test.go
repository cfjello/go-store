package test

import (
	"testing"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
)

// sampleValidated carries go-playground/validator struct tags used to exercise
// Store.ValidateCached and the Store.Set Check path.
type sampleValidated struct {
	Name string `validate:"required"`
	Age  int    `validate:"gte=0,lte=130"`
}

// newValidTestStore returns the package-test store. SQLITE_DB_URL is set in
// TestMain; database.New is a process-wide singleton, so per-test env changes
// cannot retarget it.
func newValidTestStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(nil, true)
	if s == nil {
		t.Fatal("failed to create store")
	}
	return s
}

// registerObject registers obj under key.
func registerObject(t *testing.T, s *store.Store, key string, obj any) {
	t.Helper()
	if _, err := s.Register(types.RegisterArgs{Key: key, Object: obj}); err != nil {
		t.Fatalf("failed to register key %q: %v", key, err)
	}
}

func TestValidateCached_ValidObject(t *testing.T) {
	s := newValidTestStore(t)
	registerObject(t, s, "valid-object", sampleValidated{Name: "Alice", Age: 30})

	if err := s.ValidateCached("valid-object"); err != nil {
		t.Fatalf("ValidateCached() unexpected error: %v", err)
	}
}

func TestValidateCached_InvalidObject(t *testing.T) {
	s := newValidTestStore(t)
	registerObject(t, s, "invalid-object", sampleValidated{Name: "", Age: 999})

	if err := s.ValidateCached("invalid-object"); err == nil {
		t.Fatal("ValidateCached() expected an error for an invalid object, got nil")
	}
}

func TestValidateCached_UnknownKey(t *testing.T) {
	s := newValidTestStore(t)

	if err := s.ValidateCached("does-not-exist"); err == nil {
		t.Fatal("ValidateCached() expected an error for an unregistered key, got nil")
	}
}

func TestValidateCached_NonStructValue(t *testing.T) {
	s := newValidTestStore(t)
	registerObject(t, s, "not-a-struct", "just a string")

	if err := s.ValidateCached("not-a-struct"); err == nil {
		t.Fatal("ValidateCached() expected an error for a non-struct cached value, got nil")
	}
}

func TestSet_ValidatesCachedObjectWhenCheckTrue(t *testing.T) {
	s := newValidTestStore(t)
	key := "set-check-valid"
	registerObject(t, s, key, sampleValidated{Name: "Bob", Age: 40})

	var payload any = sampleValidated{Name: "Bob", Age: 41}
	if _, err := s.Set(types.SetArgs{
		Key:    key,
		Object: payload,
		Check:  types.CheckOn,
	}); err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
}

func TestSet_FailsWhenCachedObjectInvalidAndCheckTrue(t *testing.T) {
	s := newValidTestStore(t)
	key := "set-check-invalid"
	// The registered (cached) object is invalid; ValidateCached checks that one, not the new payload below.
	registerObject(t, s, key, sampleValidated{Name: "", Age: 999})

	var payload any = sampleValidated{Name: "Carol", Age: 25}
	if _, err := s.Set(types.SetArgs{
		Key:    key,
		Object: payload,
		Check:  types.CheckOn,
	}); err == nil {
		t.Fatal("Set() expected an error because the cached object fails validation, got nil")
	}
}

func TestSet_SkipsValidationWhenCheckFalse(t *testing.T) {
	s := newValidTestStore(t)
	key := "set-check-false"
	registerObject(t, s, key, sampleValidated{Name: "", Age: 999})

	var payload any = sampleValidated{Name: "Dana", Age: 20}
	if _, err := s.Set(types.SetArgs{
		Key:    key,
		Object: payload,
		Check:  types.CheckSkip,
	}); err != nil {
		t.Fatalf("Set() unexpected error with Check=false: %v", err)
	}
}
