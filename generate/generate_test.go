package generate

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func TestAssemblyScriptHasSingleFinalNewline(t *testing.T) {
	manifest, err := schema.Compile(schema.Schema{Package: "newline"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := AssemblyScript(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(output, []byte("\n")) || bytes.HasSuffix(output, []byte("\n\n")) {
		t.Fatalf("generated AssemblyScript must end in exactly one newline")
	}
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
		{Package: "collision", Types: []schema.Type{{Name: "Module", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}, Functions: []schema.Function{{Name: "use", Parameters: []schema.Parameter{{Name: "value", Type: "Module"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Bind", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}, Functions: []schema.Function{{Name: "use", Parameters: []schema.Parameter{{Name: "value", Type: "Bind"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}, Functions: []schema.Function{{Name: "mu", Parameters: []schema.Parameter{{Name: "value", Type: "Value"}}}}},
		{Package: "collision", Types: []schema.Type{{Name: "Value", Fields: []schema.Field{{Name: "id", Type: "u8"}}}}, Functions: []schema.Function{{Name: "memory", Parameters: []schema.Parameter{{Name: "value", Type: "Value"}}}}},
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

func TestGeneratedFacadeSignatureMatrixCompiles(t *testing.T) {
	value := schema.Type{Name: "Value", Fields: []schema.Field{{Name: "n", Type: "u32"}}}
	parameter := func(name string) schema.Parameter { return schema.Parameter{Name: name, Type: "Value"} }
	functions := []schema.Function{
		{Name: "void1", Parameters: []schema.Parameter{parameter("a")}},
		{Name: "void2", Parameters: []schema.Parameter{parameter("a"), parameter("b")}},
		{Name: "void3", Parameters: []schema.Parameter{parameter("a"), parameter("b"), parameter("c")}},
		{Name: "void4", Parameters: []schema.Parameter{parameter("a"), parameter("b"), parameter("c"), parameter("d")}},
		{Name: "result0", Result: "Value"},
		{Name: "result1", Parameters: []schema.Parameter{parameter("a")}, Result: "Value"},
		{Name: "result2", Parameters: []schema.Parameter{parameter("a"), parameter("b")}, Result: "Value"},
		{Name: "result3", Parameters: []schema.Parameter{parameter("a"), parameter("b"), parameter("c")}, Result: "Value"},
	}
	manifest, err := schema.Compile(schema.Schema{Package: "matrix", Types: []schema.Type{value}, Functions: functions})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Go(manifest, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	compileGeneratedPackage(t, output)
}

func compileGeneratedPackage(t *testing.T, generated []byte) {
	t.Helper()
	directory := t.TempDir()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	module := fmt.Sprintf("module generated.test\n\ngo 1.22\n\nrequire github.com/JairusSW/wago-bind-as v0.0.0\nreplace github.com/JairusSW/wago-bind-as => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bindings.go"), generated, 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-mod=mod", "./...")
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated package does not compile: %v\n%s", err, output)
	}
}
