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

// Equal reports whether a and b describe the same type structure.
func (a TypeInfo) Equal(b TypeInfo) bool {
	if a.Type != b.Type || a.Kind != b.Kind {
		return false
	}
	if !typeInfoPtrEqual(a.Elem, b.Elem) || !typeInfoPtrEqual(a.KeyType, b.KeyType) || !typeInfoPtrEqual(a.ValueType, b.ValueType) {
		return false
	}
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for name, field := range a.Fields {
		other, ok := b.Fields[name]
		if !ok || !field.Equal(other) {
			return false
		}
	}
	return true
}

func typeInfoPtrEqual(a, b *TypeInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
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

// BuildTypeInfo records the declared type of v. The result does not depend on
// whether collections are empty, pointers are nil, or interface values hold a
// concrete type — those are value details, not the type used for Set checks.
func BuildTypeInfo(v reflect.Value) TypeInfo {
	if !v.IsValid() {
		return TypeInfo{}
	}
	return BuildTypeInfoFromType(v.Type())
}

// BuildTypeInfoFromType recursively describes t. Cyclic types are truncated to
// Type and Kind on the second visit so recursive structs cannot overflow.
func BuildTypeInfoFromType(t reflect.Type) TypeInfo {
	if t == nil {
		return TypeInfo{}
	}
	return buildTypeInfoFromType(t, make(map[reflect.Type]struct{}))
}

func buildTypeInfoFromType(t reflect.Type, seen map[reflect.Type]struct{}) TypeInfo {
	info := TypeInfo{
		Type: t.String(),
		Kind: t.Kind().String(),
	}
	if _, ok := seen[t]; ok {
		return info
	}
	seen[t] = struct{}{}

	switch t.Kind() {
	case reflect.Struct:
		info.Fields = make(map[string]TypeInfo, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			info.Fields[field.Name] = buildTypeInfoFromType(field.Type, seen)
		}

	case reflect.Map:
		keyInfo := buildTypeInfoFromType(t.Key(), seen)
		info.KeyType = &keyInfo
		valueInfo := buildTypeInfoFromType(t.Elem(), seen)
		info.ValueType = &valueInfo

	case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Chan:
		elemInfo := buildTypeInfoFromType(t.Elem(), seen)
		info.Elem = &elemInfo
	}

	return info
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
