package store

import (
	"fmt"
	"reflect"

	"github.com/cfjello/go-store/pkg/dynReflect"
	"github.com/cfjello/go-store/pkg/perf"
	"github.com/go-playground/validator/v10"
)

// validate is shared across all validation calls; it is safe for concurrent use
// once built, and caches struct tag parsing per type for re-use across lookups.
var validate = validator.New()

// ValidateCached looks up the object cached under key (populated by Register via
// SetCache) and runs struct-tag based validation (`validate:"..."` annotations)
// against it using the shared validator instance.
func (s *Store) ValidateCached(key string) error {
	defer perf.Track("store.ValidateCached")()
	cached, ok := s.GetCache(key)
	if !ok {
		return fmt.Errorf("no cached object found for key %q", key)
	}

	structVal, err := resolveStructValue(cached)
	if err != nil {
		return fmt.Errorf("object for key %q: %w", key, err)
	}

	if err := validate.Struct(structVal.Interface()); err != nil {
		return fmt.Errorf("validation failed for key %q: %w", key, err)
	}
	return nil
}

// checkPayloadType compares the persisted payload's TypeInfo against the shape
// recorded at Register. An empty registered TypeInfo (legacy metadata) skips
// the comparison. payload is already the dataName field or the whole static object.
func (s *Store) checkPayloadType(key string, registered dynReflect.TypeInfo, payload any) error {
	if registered.Type == "" && registered.Kind == "" {
		return nil
	}
	got := dynReflect.BuildTypeInfo(reflect.ValueOf(payload))
	if registered.Equal(got) {
		return nil
	}
	return fmt.Errorf("type mismatch for key %q: registered %s, got %s", key, registered.Type, got.Type)
}

// resolveStructValue dereferences pointers/interfaces down to the underlying struct value.
func resolveStructValue(obj any) (reflect.Value, error) {
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, fmt.Errorf("object is nil")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("must be a struct, got %s", v.Kind())
	}
	return v, nil
}
