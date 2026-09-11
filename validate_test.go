package bindas

import (
	"errors"
	"testing"

	"github.com/JairusSW/wago-bind-as/schema"
)

func validationManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := schema.Compile(schema.Schema{Package: "validation", Types: []schema.Type{
		{Name: "Node", Fields: []schema.Field{{Name: "enabled", Type: "bool"}, {Name: "name", Type: "utf8"}, {Name: "children", Type: "vec<Node>"}, {Name: "parent", Type: "ref<Node>"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestValidateCompleteGraphAndCycles(t *testing.T) {
	manifest := validationManifest(t)
	var node schema.ResolvedType
	for _, typ := range manifest.Types {
		if typ.Name == "Node" {
			node = typ
		}
	}
	arena, _ := NewArena(make([]byte, 2048), 0, 2048)
	root, _ := arena.Alloc(node.Size, node.Align)
	children, _ := arena.Alloc(node.Size, node.Align)
	r := arena.Region()
	rootRecord, _ := r.Record(root, node.Size, node.Align)
	childRecord, _ := r.Record(children, node.Size, node.Align)
	name, _ := arena.PutUTF8("root")
	_ = rootRecord.SetBool(0, true)
	_ = r.PutSpan(root, 4, name)
	_ = r.PutVector(root, 12, VectorDesc{Offset: children, Length: 1, Capacity: 1})
	_ = rootRecord.SetUint32(24, uint32(root))
	childName, _ := arena.PutUTF8("child")
	_ = childRecord.SetBool(0, false)
	_ = r.PutSpan(children, 4, childName)
	_ = r.PutVector(children, 12, VectorDesc{})
	_ = childRecord.SetUint32(24, uint32(root))
	if err := Validate(r, manifest, "Node", root, ValidationLimits{}); err != nil {
		t.Fatal(err)
	}
	_ = childRecord.SetUint8(0, 2)
	if err := Validate(r, manifest, "Node", root, ValidationLimits{}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("bool error = %v", err)
	}
}

func TestValidateHonorsWorkLimits(t *testing.T) {
	manifest := validationManifest(t)
	var node schema.ResolvedType
	for _, typ := range manifest.Types {
		node = typ
	}
	arena, _ := NewArena(make([]byte, 2048), 0, 2048)
	root, _ := arena.Alloc(node.Size, node.Align)
	children, _ := arena.Alloc(node.Size*2, node.Align)
	r := arena.Region()
	record, _ := r.Record(root, node.Size, node.Align)
	_ = record.SetBool(0, false)
	empty, _ := arena.PutUTF8("")
	_ = r.PutSpan(root, 4, empty)
	_ = r.PutVector(root, 12, VectorDesc{Offset: children, Length: 2, Capacity: 2})
	_ = record.SetUint32(24, uint32(root))
	if err := Validate(r, manifest, "Node", root, ValidationLimits{MaxVectorElements: 1}); !errors.Is(err, ErrValidationLimit) {
		t.Fatalf("limit error = %v", err)
	}
}
