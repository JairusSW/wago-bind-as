package schema

import (
	"reflect"
	"testing"
)

func TestCompileRequestLayoutDeterministically(t *testing.T) {
	input := Schema{Package: "example", Types: []Type{
		{Name: "User", Fields: []Field{{Name: "id", Type: "u64"}, {Name: "name", Type: "utf8"}}},
		{Name: "Header", Fields: []Field{{Name: "name", Type: "utf8"}, {Name: "value", Type: "utf8"}}},
		{Name: "Request", Fields: []Field{{Name: "id", Type: "u64"}, {Name: "score", Type: "f32"}, {Name: "flags", Type: "u32"}, {Name: "name", Type: "utf8"}, {Name: "body", Type: "bytes"}, {Name: "headers", Type: "vec<Header>"}, {Name: "user", Type: "ref<User>"}, {Name: "timestamp", Type: "timestamp_ns"}}},
	}}
	first, err := Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(Schema{Package: input.Package, Types: []Type{input.Types[2], input.Types[0], input.Types[1]}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic manifests\n%#v\n%#v", first, second)
	}
	var request ResolvedType
	for _, typ := range first.Types {
		if typ.Name == "Request" {
			request = typ
		}
	}
	if request.Size != 56 || request.Align != 8 {
		t.Fatalf("Request size/align = %d/%d", request.Size, request.Align)
	}
	wantOffsets := []uint32{0, 8, 12, 16, 24, 32, 44, 48}
	for i, field := range request.Fields {
		if field.Offset != wantOffsets[i] {
			t.Fatalf("%s offset = %d, want %d", field.Name, field.Offset, wantOffsets[i])
		}
	}
	if len(first.Fingerprint) != 32 {
		t.Fatalf("fingerprint = %q", first.Fingerprint)
	}
}

func TestCompileRejectsInlineCycles(t *testing.T) {
	_, err := Compile(Schema{Package: "bad", Types: []Type{{Name: "Node", Fields: []Field{{Name: "next", Type: "Node"}}}}})
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if _, err := Compile(Schema{Package: "good", Types: []Type{{Name: "Node", Fields: []Field{{Name: "next", Type: "ref<Node>"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(Schema{Package: "tree", Types: []Type{{Name: "Node", Fields: []Field{{Name: "children", Type: "vec<Node>"}}}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCompileFixedArrays(t *testing.T) {
	manifest, err := Compile(Schema{Package: "arrays", Types: []Type{{Name: "Value", Fields: []Field{{Name: "words", Type: "array<u32, 3>"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	field := manifest.Types[0].Fields[0]
	if field.Size != 12 || field.Align != 4 {
		t.Fatalf("array size/align = %d/%d, want 12/4", field.Size, field.Align)
	}

	for _, fieldType := range []string{"array<ref<Value>, 2>", "array<vec<Value>, 2>", "array<array<u8, 2>, 2>"} {
		_, err := Compile(Schema{Package: "bad", Types: []Type{{Name: "Value", Fields: []Field{{Name: "items", Type: fieldType}}}}})
		if err == nil {
			t.Fatalf("Compile accepted unsupported %s", fieldType)
		}
	}
}

func TestCompileRejectsEmptyAndReservedTypes(t *testing.T) {
	for _, typ := range []Type{{Name: "Empty"}, {Name: "u32", Fields: []Field{{Name: "value", Type: "u32"}}}} {
		if _, err := Compile(Schema{Package: "bad", Types: []Type{typ}}); err == nil {
			t.Fatalf("Compile accepted invalid type %#v", typ)
		}
	}
}

func TestCompileFunctionSurfaceDeterministically(t *testing.T) {
	types := []Type{{Name: "Vec3", Fields: []Field{{Name: "x", Type: "f32"}, {Name: "y", Type: "f32"}, {Name: "z", Type: "f32"}}}}
	functions := []Function{
		{Name: "length", Parameters: []Parameter{{Name: "value", Type: "Vec3"}}, Result: "Vec3"},
		{Name: "add", Parameters: []Parameter{{Name: "left", Type: "Vec3"}, {Name: "right", Type: "Vec3"}}, Result: "Vec3"},
	}
	first, err := Compile(Schema{Package: "vectors", Types: types, Functions: functions})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(Schema{Package: "vectors", Types: types, Functions: []Function{functions[1], functions[0]}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("function order changed the resolved manifest")
	}
	if len(first.Functions) != 2 || first.Functions[0].Name != "add" {
		t.Fatalf("resolved functions = %#v", first.Functions)
	}
	if _, err := Compile(Schema{Package: "bad", Types: types, Functions: []Function{{Name: "bad", Parameters: []Parameter{{Name: "value", Type: "Missing"}}}}}); err == nil {
		t.Fatal("Compile accepted an unknown boundary record")
	}
}
