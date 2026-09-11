package generate

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JairusSW/wago-bind-as/schema"
)

func TestCommittedRequestBindingsAreCurrent(t *testing.T) {
	root := filepath.Join("..", "examples", "request")
	raw, err := os.ReadFile(filepath.Join(root, "wago-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source schema.Schema
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	manifest, err := schema.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	goOutput, err := Go(manifest, "request")
	if err != nil {
		t.Fatal(err)
	}
	asOutput, err := AssemblyScript(manifest)
	if err != nil {
		t.Fatal(err)
	}
	assertFile := func(name string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is stale; rerun the generator", name)
		}
	}
	assertFile("bindings_gen.go", goOutput)
	assertFile("bindings_gen.ts", asOutput)
}

func TestFixedArrayBuilderRequiresExactBytes(t *testing.T) {
	manifest, err := schema.Compile(schema.Schema{Package: "arrays", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "words", Type: "array<u32, 3>"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Go(manifest, "arrays")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte("if len(value.Words) != 12")) {
		t.Fatalf("generated builder does not enforce the exact array extent:\n%s", output)
	}
}

func TestAssemblyScriptFingerprintUsesWireEndianness(t *testing.T) {
	manifest, err := schema.Compile(schema.Schema{Package: "fingerprint", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "value", Type: "u8"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := AssemblyScript(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(manifest.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("SCHEMA_FINGERPRINT_0: u64 = 0x%016x", binary.LittleEndian.Uint64(raw[:8]))
	if !bytes.Contains(output, []byte(want)) {
		t.Fatalf("generated fingerprint is not little-endian: want %q in\n%s", want, output)
	}
}

func TestGoRejectsGeneratedIdentifierCollisions(t *testing.T) {
	for _, source := range []schema.Schema{
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "id", Type: "u8"}, {Name: "Id", Type: "u8"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "offset", Type: "u32"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "score", Type: "f32"}, {Name: "setScore", Type: "u32"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "id", Type: "u8"}}}, {Name: "value", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Fingerprint", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}},
	} {
		manifest, err := schema.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Go(manifest, "bindings"); err == nil {
			t.Fatalf("Go accepted colliding schema %#v", source)
		}
	}
	if _, err := Go(schema.Manifest{}, "package"); err == nil {
		t.Fatal("Go accepted a keyword package name")
	}
}

func TestGoScalarFacadeUsesOnlyNeededAllocationFreeImports(t *testing.T) {
	manifest, err := schema.Compile(schema.Schema{
		Package: "flags",
		Types:   []schema.Type{{Name: "Flag", Fields: []schema.Field{{Name: "enabled", Type: "bool"}}}},
		Functions: []schema.Function{{
			Name:       "toggle",
			Parameters: []schema.Parameter{{Name: "value", Type: "Flag"}},
			Result:     "Flag",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Go(manifest, "flags")
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range [][]byte{[]byte("\"encoding/binary\""), []byte("\"math\""), []byte("arena, err := bindas.NewArena(memory")} {
		if bytes.Contains(output, unwanted) {
			t.Fatalf("generated bool facade contains %q:\n%s", unwanted, output)
		}
	}
	if !bytes.Contains(output, []byte("if memory[address+0] > 1")) {
		t.Fatalf("generated bool facade does not validate its wire value:\n%s", output)
	}
}
