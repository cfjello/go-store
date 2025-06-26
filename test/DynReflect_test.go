package test

import (
	"reflect"
	"testing"

	"github.com/cfjello/go-store/pkg/dynReflect"
	// Adjust the import path as necessary
)

type SimpleStruct struct {
	Name  string
	Age   int
	Email string
}

type NestedStruct struct {
	ID     int
	Person SimpleStruct
	Active bool
}

type ComplexStruct struct {
	ID        int
	Names     []string
	Data      map[string]interface{}
	NestedPtr *SimpleStruct
	Channel   chan string
}

func TestBuildTypeInfo_BasicTypes(t *testing.T) {
	// Test integer
	intVal := 42
	intInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(intVal))
	if intInfo.Type != "int" || intInfo.Kind != "int" {
		t.Errorf("Expected type int, got %s with kind %s", intInfo.Type, intInfo.Kind)
	}

	// Test string
	strVal := "test"
	strInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(strVal))
	if strInfo.Type != "string" || strInfo.Kind != "string" {
		t.Errorf("Expected type string, got %s with kind %s", strInfo.Type, strInfo.Kind)
	}

	// Test bool
	boolVal := true
	boolInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(boolVal))
	if boolInfo.Type != "bool" || boolInfo.Kind != "bool" {
		t.Errorf("Expected type bool, got %s with kind %s", boolInfo.Type, boolInfo.Kind)
	}
}

func TestBuildTypeInfo_Struct(t *testing.T) {
	// Test simple struct
	simple := SimpleStruct{Name: "John", Age: 30, Email: "john@example.com"}
	simpleInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(simple))

	if simpleInfo.Type != "test.SimpleStruct" || simpleInfo.Kind != "struct" {
		t.Errorf("Expected type test.SimpleStruct, got %s with kind %s", simpleInfo.Type, simpleInfo.Kind)
	}

	if len(simpleInfo.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(simpleInfo.Fields))
	}

	if field, ok := simpleInfo.Fields["Name"]; !ok || field.Type != "string" {
		t.Errorf("Field 'Name' not found or wrong type: %+v", field)
	}

	// Test nested struct
	nested := NestedStruct{ID: 1, Person: simple, Active: true}
	nestedInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(nested))

	if nestedInfo.Type != "test.NestedStruct" || nestedInfo.Kind != "struct" {
		t.Errorf("Expected type test.NestedStruct, got %s with kind %s", nestedInfo.Type, nestedInfo.Kind)
	}

	if personField, ok := nestedInfo.Fields["Person"]; !ok || personField.Type != "test.SimpleStruct" {
		t.Errorf("Field 'Person' not found or wrong type: %+v", personField)
	}
}

func TestBuildTypeInfo_Map(t *testing.T) {
	// Test string->int map
	mapStringInt := map[string]int{"one": 1, "two": 2}
	mapInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(mapStringInt))

	if mapInfo.Type != "map[string]int" || mapInfo.Kind != "map" {
		t.Errorf("Expected type map[string]int, got %s with kind %s", mapInfo.Type, mapInfo.Kind)
	}

	if mapInfo.KeyType == nil || mapInfo.KeyType.Type != "string" {
		t.Errorf("Expected key type string, got %+v", mapInfo.KeyType)
	}

	if mapInfo.ValueType == nil || mapInfo.ValueType.Type != "int" {
		t.Errorf("Expected value type int, got %+v", mapInfo.ValueType)
	}

	// Test map with complex values
	mapComplex := map[string]SimpleStruct{
		"person": {Name: "John", Age: 30, Email: "john@example.com"},
	}
	mapComplexInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(mapComplex))

	if mapComplexInfo.ValueType == nil || mapComplexInfo.ValueType.Type != "test.SimpleStruct" {
		t.Errorf("Expected value type test.SimpleStruct, got %+v", mapComplexInfo.ValueType)
	}
}

func TestBuildTypeInfo_SliceAndArray(t *testing.T) {
	// Test slice
	slice := []string{"one", "two", "three"}
	sliceInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(slice))

	if sliceInfo.Type != "[]string" || sliceInfo.Kind != "slice" {
		t.Errorf("Expected type []string, got %s with kind %s", sliceInfo.Type, sliceInfo.Kind)
	}

	if sliceInfo.Elem == nil || sliceInfo.Elem.Type != "string" {
		t.Errorf("Expected element type string, got %+v", sliceInfo.Elem)
	}

	// Test array
	array := [3]int{1, 2, 3}
	arrayInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(array))

	if arrayInfo.Type != "[3]int" || arrayInfo.Kind != "array" {
		t.Errorf("Expected type [3]int, got %s with kind %s", arrayInfo.Type, arrayInfo.Kind)
	}

	if arrayInfo.Elem == nil || arrayInfo.Elem.Type != "int" {
		t.Errorf("Expected element type int, got %+v", arrayInfo.Elem)
	}
}

func TestBuildTypeInfo_Pointer(t *testing.T) {
	// Test pointer to simple type
	intPtr := new(int)
	*intPtr = 42
	ptrInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(intPtr))

	if ptrInfo.Type != "*int" || ptrInfo.Kind != "ptr" {
		t.Errorf("Expected type *int, got %s with kind %s", ptrInfo.Type, ptrInfo.Kind)
	}

	if ptrInfo.Elem == nil || ptrInfo.Elem.Type != "int" {
		t.Errorf("Expected element type int, got %+v", ptrInfo.Elem)
	}

	// Test pointer to struct
	structPtr := &SimpleStruct{Name: "John", Age: 30}
	structPtrInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(structPtr))

	if structPtrInfo.Type != "*test.SimpleStruct" || structPtrInfo.Kind != "ptr" {
		t.Errorf("Expected type *test.SimpleStruct, got %s with kind %s", structPtrInfo.Type, structPtrInfo.Kind)
	}

	if structPtrInfo.Elem == nil || structPtrInfo.Elem.Type != "test.SimpleStruct" {
		t.Errorf("Expected element type DynReflect.SimpleStruct, got %+v", structPtrInfo.Elem)
	}
}

func TestBuildTypeInfo_Interface(t *testing.T) {
	// Test interface with string value
	var iface interface{} = "test string"
	ifaceInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(iface))

	// reflect.ValueOf unwraps the interface, so we expect the underlying type
	if ifaceInfo.Type != "string" || ifaceInfo.Kind != "string" {
		t.Errorf("Expected type string, got %s with kind %s", ifaceInfo.Type, ifaceInfo.Kind)
	}

	// Test interface with struct value
	var ifaceStruct interface{} = SimpleStruct{Name: "John"}
	ifaceStructInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(ifaceStruct))

	if ifaceStructInfo.Type != "test.SimpleStruct" || ifaceStructInfo.Kind != "struct" {
		t.Errorf("Expected type test.SimpleStruct, got %s with kind %s", ifaceStructInfo.Type, ifaceStructInfo.Kind)
	}
}

func TestBuildTypeInfo_Channel(t *testing.T) {
	// Test channel
	ch := make(chan int)
	chInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(ch))

	if chInfo.Type != "chan int" || chInfo.Kind != "chan" {
		t.Errorf("Expected type chan int, got %s with kind %s", chInfo.Type, chInfo.Kind)
	}

	if chInfo.Elem == nil || chInfo.Elem.Type != "int" {
		t.Errorf("Expected element type int, got %+v", chInfo.Elem)
	}

	// Test buffered channel
	bufCh := make(chan string, 5)
	bufChInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(bufCh))

	if bufChInfo.Type != "chan string" || bufChInfo.Kind != "chan" {
		t.Errorf("Expected type chan string, got %s with kind %s", bufChInfo.Type, bufChInfo.Kind)
	}

	if bufChInfo.Elem == nil || bufChInfo.Elem.Type != "string" {
		t.Errorf("Expected element type string, got %+v", bufChInfo.Elem)
	}
}

func TestBuildTypeInfo_ComplexStruct(t *testing.T) {
	// Test a complex struct with multiple types
	complex := ComplexStruct{
		ID:        1,
		Names:     []string{"John", "Doe"},
		Data:      map[string]interface{}{"age": 30, "active": true},
		NestedPtr: &SimpleStruct{Name: "Jane", Age: 28},
		Channel:   make(chan string),
	}

	complexInfo := dynReflect.BuildTypeInfo(reflect.ValueOf(complex))

	if complexInfo.Type != "test.ComplexStruct" || complexInfo.Kind != "struct" {
		t.Errorf("Expected type test.ComplexStruct, got %s with kind %s", complexInfo.Type, complexInfo.Kind)
	}

	if len(complexInfo.Fields) != 5 {
		t.Errorf("Expected 5 fields, got %d", len(complexInfo.Fields))
	}

	// Check slice field
	if namesField, ok := complexInfo.Fields["Names"]; !ok || namesField.Type != "[]string" {
		t.Errorf("Field 'Names' not found or wrong type: %+v", namesField)
	}

	// Check map field
	if dataField, ok := complexInfo.Fields["Data"]; !ok || dataField.Type != "map[string]interface {}" {
		t.Errorf("Field 'Data' not found or wrong type: %+v", dataField)
	}

	// Check pointer field
	if ptrField, ok := complexInfo.Fields["NestedPtr"]; !ok || ptrField.Type != "*test.SimpleStruct" {
		t.Errorf("Field 'NestedPtr' not found or wrong type: %+v", ptrField)
	}

	// Check channel field
	if chanField, ok := complexInfo.Fields["Channel"]; !ok || chanField.Type != "chan string" {
		t.Errorf("Field 'Channel' not found or wrong type: %+v", chanField)
	}
}
