package dynReflect

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
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

// JSONToObjectType inverts ObjectToTypeJSON: it parses type JSON and returns a new
// zero-valued instance of the reconstructed (structurally equivalent) type. Named
// types cannot be recovered from the JSON alone, so the result is always built from
// anonymous struct/slice/map/pointer/etc. types matching the recorded structure.
func JSONToObjectType(jsonData string) (interface{}, error) {
	var info TypeInfo
	if err := json.Unmarshal([]byte(jsonData), &info); err != nil {
		return nil, fmt.Errorf("failed to parse type JSON: %w", err)
	}
	t, err := TypeInfoToType(info)
	if err != nil {
		return nil, fmt.Errorf("failed to rebuild type: %w", err)
	}
	return reflect.New(t).Elem().Interface(), nil
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

// primitiveTypes maps a reflect.Kind's string form to a sample value of that kind's type.
var primitiveTypes = map[string]reflect.Type{
	"bool":       reflect.TypeOf(false),
	"int":        reflect.TypeOf(int(0)),
	"int8":       reflect.TypeOf(int8(0)),
	"int16":      reflect.TypeOf(int16(0)),
	"int32":      reflect.TypeOf(int32(0)),
	"int64":      reflect.TypeOf(int64(0)),
	"uint":       reflect.TypeOf(uint(0)),
	"uint8":      reflect.TypeOf(uint8(0)),
	"uint16":     reflect.TypeOf(uint16(0)),
	"uint32":     reflect.TypeOf(uint32(0)),
	"uint64":     reflect.TypeOf(uint64(0)),
	"uintptr":    reflect.TypeOf(uintptr(0)),
	"float32":    reflect.TypeOf(float32(0)),
	"float64":    reflect.TypeOf(float64(0)),
	"complex64":  reflect.TypeOf(complex64(0)),
	"complex128": reflect.TypeOf(complex128(0)),
	"string":     reflect.TypeOf(""),
}

// TypeInfoToType recursively rebuilds a reflect.Type from a TypeInfo, inverting BuildTypeInfo.
func TypeInfoToType(info TypeInfo) (reflect.Type, error) {
	if t, ok := primitiveTypes[info.Kind]; ok {
		return t, nil
	}

	switch info.Kind {
	case "struct":
		names := make([]string, 0, len(info.Fields))
		for name := range info.Fields {
			names = append(names, name)
		}
		sort.Strings(names) // map iteration order isn't stable, so sort for a deterministic layout

		fields := make([]reflect.StructField, 0, len(names))
		for _, name := range names {
			fieldType, err := TypeInfoToType(info.Fields[name])
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", name, err)
			}
			fields = append(fields, reflect.StructField{Name: name, Type: fieldType})
		}
		return reflect.StructOf(fields), nil

	case "map":
		if info.KeyType == nil || info.ValueType == nil {
			return nil, fmt.Errorf("map type info is missing key or value type")
		}
		keyType, err := TypeInfoToType(*info.KeyType)
		if err != nil {
			return nil, fmt.Errorf("map key: %w", err)
		}
		valueType, err := TypeInfoToType(*info.ValueType)
		if err != nil {
			return nil, fmt.Errorf("map value: %w", err)
		}
		return reflect.MapOf(keyType, valueType), nil

	case "slice", "array":
		if info.Elem == nil {
			return nil, fmt.Errorf("%s type info is missing element type", info.Kind)
		}
		elemType, err := TypeInfoToType(*info.Elem)
		if err != nil {
			return nil, fmt.Errorf("element: %w", err)
		}
		// Array length isn't recorded in TypeInfo, so arrays are rebuilt as slices.
		return reflect.SliceOf(elemType), nil

	case "ptr":
		if info.Elem == nil {
			return nil, fmt.Errorf("pointer type info is missing element type")
		}
		elemType, err := TypeInfoToType(*info.Elem)
		if err != nil {
			return nil, fmt.Errorf("pointer element: %w", err)
		}
		return reflect.PointerTo(elemType), nil

	case "interface":
		return reflect.TypeOf((*interface{})(nil)).Elem(), nil

	case "chan":
		if info.Elem == nil {
			return nil, fmt.Errorf("chan type info is missing element type")
		}
		elemType, err := TypeInfoToType(*info.Elem)
		if err != nil {
			return nil, fmt.Errorf("chan element: %w", err)
		}
		return reflect.ChanOf(reflect.BothDir, elemType), nil

	default:
		return nil, fmt.Errorf("unsupported kind %q", info.Kind)
	}
}
