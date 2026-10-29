package store

import (
	"fmt"
	"reflect"

	"github.com/go-playground/validator/v10"
)

// validate is shared across all validation calls; it is safe for concurrent use
// once built, and caches struct tag parsing per type for re-use across lookups.
var validate = validator.New()

// ValidateCached looks up the object cached under key (populated by Register via
// SetCache) and runs struct-tag based validation (`validate:"..."` annotations)
// against it using the shared validator instance.
func (s *Store) ValidateCached(key string) error {
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
