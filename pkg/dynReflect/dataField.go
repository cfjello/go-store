package dynReflect

import (
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DataValue returns the stored payload of obj: obj[name] for a string-keyed map,
// or the exported struct field named name / its exported form (data → Data) /
// a field tagged json:"name". Pointers and interfaces are dereferenced first.
func DataValue(obj any, name string) (any, bool) {
	fv, ok := dataField(obj, name)
	if !ok || !fv.CanInterface() {
		return nil, false
	}
	return fv.Interface(), true
}

// DataFieldPtr returns a pointer suitable for json.Unmarshal into the payload
// field identified by DataValue. A nil pointer field is allocated when it can
// be set.
func DataFieldPtr(obj any, name string) (any, bool) {
	fv, ok := dataField(obj, name)
	if !ok {
		return nil, false
	}
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			if !fv.CanSet() {
				return nil, false
			}
			n := reflect.New(fv.Type().Elem())
			fv.Set(n)
			return n.Interface(), true
		}
		return fv.Interface(), true
	}
	if !fv.CanAddr() {
		return nil, false
	}
	return fv.Addr().Interface(), true
}

func dataField(obj any, name string) (reflect.Value, bool) {
	if obj == nil || name == "" {
		return reflect.Value{}, false
	}
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		return mapIndex(v, name)
	case reflect.Struct:
		return structField(v, name)
	default:
		return reflect.Value{}, false
	}
}

func mapIndex(v reflect.Value, name string) (reflect.Value, bool) {
	if v.Type().Key().Kind() != reflect.String {
		return reflect.Value{}, false
	}
	key := reflect.ValueOf(name)
	if kt := v.Type().Key(); key.Type() != kt {
		if !key.CanConvert(kt) {
			return reflect.Value{}, false
		}
		key = key.Convert(kt)
	}
	mv := v.MapIndex(key)
	if !mv.IsValid() {
		return reflect.Value{}, false
	}
	return mv, true
}

func structField(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()
	if fv, ok := exportedField(v, t, name); ok {
		return fv, true
	}
	if exported := exportName(name); exported != name {
		if fv, ok := exportedField(v, t, exported); ok {
			return fv, true
		}
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if jsonName, _, _ := strings.Cut(tag, ","); jsonName == name {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func exportedField(v reflect.Value, t reflect.Type, name string) (reflect.Value, bool) {
	f, ok := t.FieldByName(name)
	if !ok || !f.IsExported() {
		return reflect.Value{}, false
	}
	return v.FieldByIndex(f.Index), true
}

func exportName(name string) string {
	r, size := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError || !unicode.IsLower(r) {
		return name
	}
	return string(unicode.ToUpper(r)) + name[size:]
}
