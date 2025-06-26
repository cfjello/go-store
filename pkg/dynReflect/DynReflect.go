package dynReflect

import (
	"encoding/json"
	"reflect"
)

// TypeInfo represents type information of a value
type TypeInfo struct {
	Type      string              `json:"type"`
	Kind      string              `json:"kind"`
	Fields    map[string]TypeInfo `json:"fields,omitempty"`
	Elem      *TypeInfo           `json:"elem,omitempty"`      // For pointer, slice, array, chan
	KeyType   *TypeInfo           `json:"keyType,omitempty"`   // For maps
	ValueType *TypeInfo           `json:"valueType,omitempty"` // For maps
}

// ObjectToTypeJSON converts an object to a JSON representation of its type structure
func ObjectToTypeJSON(obj interface{}) (string, error) {
	typeInfo := BuildTypeInfo(reflect.ValueOf(obj))
	jsonData, err := json.MarshalIndent(typeInfo, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}

// BuildTypeInfo recursively builds type information for a value
func BuildTypeInfo(v reflect.Value) TypeInfo {
	t := v.Type()
	info := TypeInfo{
		Type: t.String(),
		Kind: t.Kind().String(),
	}

	switch t.Kind() {
	case reflect.Struct:
		info.Fields = make(map[string]TypeInfo)
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			// Skip unexported fields
			if field.PkgPath != "" {
				continue
			}
			fieldValue := v.Field(i)
			info.Fields[field.Name] = BuildTypeInfo(fieldValue)
		}

	case reflect.Map:
		// For maps, capture both key and value types
		keyType := t.Key()
		keyInfo := TypeInfo{
			Type: keyType.String(),
			Kind: keyType.Kind().String(),
		}
		info.KeyType = &keyInfo

		valueType := t.Elem()
		valueInfo := TypeInfo{
			Type: valueType.String(),
			Kind: valueType.Kind().String(),
		}
		info.ValueType = &valueInfo

		// If the map has elements and value type is complex, examine a sample
		if v.Len() > 0 && isComplexType(valueType.Kind()) {
			iter := v.MapRange()
			if iter.Next() {
				sampleValue := BuildTypeInfo(iter.Value())
				info.ValueType = &sampleValue
			}
		}

	case reflect.Slice, reflect.Array:
		// For slices and arrays, capture element type
		elemType := t.Elem()
		elemInfo := TypeInfo{
			Type: elemType.String(),
			Kind: elemType.Kind().String(),
		}

		// If the slice/array has elements and element type is complex, examine a sample
		if v.Len() > 0 && isComplexType(elemType.Kind()) {
			sampleElem := BuildTypeInfo(v.Index(0))
			info.Elem = &sampleElem
		} else {
			info.Elem = &elemInfo
		}

	case reflect.Ptr:
		// For pointers, capture the type it points to
		elemType := t.Elem()
		elemInfo := TypeInfo{
			Type: elemType.String(),
			Kind: elemType.Kind().String(),
		}

		// If the pointer is not nil and points to a complex type, examine what it points to
		if !v.IsNil() && isComplexType(elemType.Kind()) {
			pointedValue := BuildTypeInfo(v.Elem())
			info.Elem = &pointedValue
		} else {
			info.Elem = &elemInfo
		}

	case reflect.Interface:
		// For interfaces, if not nil, examine the concrete type
		if !v.IsNil() {
			concreteValue := BuildTypeInfo(v.Elem())
			info.Elem = &concreteValue
		}

	case reflect.Chan:
		// For channels, capture element type
		elemType := t.Elem()
		elemInfo := TypeInfo{
			Type: elemType.String(),
			Kind: elemType.Kind().String(),
		}
		info.Elem = &elemInfo
	}

	return info
}

// isComplexType returns true if the kind represents a complex type
// that might need further inspection
func isComplexType(kind reflect.Kind) bool {
	switch kind {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array, reflect.Ptr, reflect.Interface:
		return true
	default:
		return false
	}
}
