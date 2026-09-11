// Package schema compiles the shared source schema into deterministic Exact32
// record layouts. Both language generators consume the resulting Manifest.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const ManifestVersion = 1

type Schema struct {
	Package   string     `json:"package"`
	Types     []Type     `json:"types"`
	Functions []Function `json:"functions,omitempty"`
}

type Function struct {
	Name       string      `json:"name"`
	Parameters []Parameter `json:"parameters,omitempty"`
	Result     string      `json:"result,omitempty"`
	OwnsResult bool        `json:"owns_result,omitempty"`
}

type Parameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Type struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Manifest struct {
	Version     uint32             `json:"version"`
	Package     string             `json:"package"`
	Fingerprint string             `json:"fingerprint"`
	Types       []ResolvedType     `json:"types"`
	Functions   []ResolvedFunction `json:"functions,omitempty"`
}

type ResolvedFunction struct {
	Name       string      `json:"name"`
	Parameters []Parameter `json:"parameters,omitempty"`
	Result     string      `json:"result,omitempty"`
	OwnsResult bool        `json:"owns_result,omitempty"`
}

type ResolvedType struct {
	Name   string          `json:"name"`
	Size   uint32          `json:"size"`
	Align  uint32          `json:"align"`
	Fields []ResolvedField `json:"fields"`
}

type ResolvedField struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Offset uint32 `json:"offset"`
	Size   uint32 `json:"size"`
	Align  uint32 `json:"align"`
}

type shape struct{ size, align uint32 }

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var primitives = map[string]shape{
	"bool": {1, 1}, "i8": {1, 1}, "u8": {1, 1},
	"i16": {2, 2}, "u16": {2, 2},
	"i32": {4, 4}, "u32": {4, 4}, "f32": {4, 4},
	"i64": {8, 8}, "u64": {8, 8}, "f64": {8, 8},
	"utf8": {8, 4}, "bytes": {8, 4},
	"timestamp_ns": {8, 8}, "duration_ns": {8, 8},
	"uuid": {16, 8}, "digest256": {32, 8},
}

func Compile(input Schema) (Manifest, error) {
	if input.Package == "" || !identifier.MatchString(input.Package) {
		return Manifest{}, fmt.Errorf("package %q is not an identifier", input.Package)
	}
	definitions := make(map[string]Type, len(input.Types))
	for _, typ := range input.Types {
		if !identifier.MatchString(typ.Name) {
			return Manifest{}, fmt.Errorf("type %q is not an identifier", typ.Name)
		}
		if _, reserved := primitives[typ.Name]; reserved {
			return Manifest{}, fmt.Errorf("type %q conflicts with a wire primitive", typ.Name)
		}
		if len(typ.Fields) == 0 {
			return Manifest{}, fmt.Errorf("type %q has no fields", typ.Name)
		}
		if _, exists := definitions[typ.Name]; exists {
			return Manifest{}, fmt.Errorf("duplicate type %q", typ.Name)
		}
		definitions[typ.Name] = typ
	}
	resolved := make(map[string]ResolvedType, len(input.Types))
	visiting := make(map[string]bool, len(input.Types))
	var compileType func(string) (ResolvedType, error)
	var resolveShape func(string) (shape, error)
	resolveShape = func(name string) (shape, error) {
		if value, ok := primitives[name]; ok {
			return value, nil
		}
		if inner, ok := wrapped(name, "ref"); ok {
			if _, exists := definitions[inner]; !exists {
				return shape{}, fmt.Errorf("unknown ref type %q", inner)
			}
			return shape{4, 4}, nil
		}
		if inner, ok := wrapped(name, "vec"); ok {
			if _, exists := definitions[inner]; !exists {
				if _, primitive := primitives[inner]; !primitive {
					return shape{}, fmt.Errorf("unknown element type %q", inner)
				}
			}
			return shape{12, 4}, nil
		}
		if n, ok := fixedArray(name); ok {
			element, count := n.element, n.count
			// Fixed arrays are inline sequences. Descriptor-bearing element kinds
			// need dedicated typed code generation so their nested references can
			// be constructed safely; reject them until that contract exists.
			if _, ok := wrapped(element, "ref"); ok {
				return shape{}, fmt.Errorf("fixed array element %q cannot be a reference", element)
			}
			if _, ok := wrapped(element, "vec"); ok {
				return shape{}, fmt.Errorf("fixed array element %q cannot be a vector", element)
			}
			if _, ok := fixedArray(element); ok {
				return shape{}, fmt.Errorf("nested fixed array element %q is unsupported", element)
			}
			es, err := resolveShape(element)
			if err != nil {
				return shape{}, err
			}
			size := uint64(es.size) * uint64(count)
			if size > uint64(^uint32(0)) {
				return shape{}, fmt.Errorf("array %q is too large", name)
			}
			return shape{uint32(size), es.align}, nil
		}
		resolvedType, err := compileType(name)
		if err != nil {
			return shape{}, err
		}
		return shape{resolvedType.Size, resolvedType.Align}, nil
	}
	compileType = func(name string) (ResolvedType, error) {
		if typ, ok := resolved[name]; ok {
			return typ, nil
		}
		definition, ok := definitions[name]
		if !ok {
			return ResolvedType{}, fmt.Errorf("unknown type %q", name)
		}
		if visiting[name] {
			return ResolvedType{}, fmt.Errorf("inline type cycle at %q; use ref<%s>", name, name)
		}
		visiting[name] = true
		defer delete(visiting, name)
		result := ResolvedType{Name: name, Align: 1, Fields: make([]ResolvedField, 0, len(definition.Fields))}
		seen := make(map[string]struct{}, len(definition.Fields))
		for _, field := range definition.Fields {
			if !identifier.MatchString(field.Name) {
				return ResolvedType{}, fmt.Errorf("%s field %q is not an identifier", name, field.Name)
			}
			if _, ok := seen[field.Name]; ok {
				return ResolvedType{}, fmt.Errorf("%s has duplicate field %q", name, field.Name)
			}
			seen[field.Name] = struct{}{}
			s, err := resolveShape(field.Type)
			if err != nil {
				return ResolvedType{}, fmt.Errorf("%s.%s: %w", name, field.Name, err)
			}
			aligned, ok := alignUp(result.Size, s.align)
			if !ok {
				return ResolvedType{}, fmt.Errorf("type %q is too large", name)
			}
			result.Size = aligned
			result.Fields = append(result.Fields, ResolvedField{Name: field.Name, Type: field.Type, Offset: result.Size, Size: s.size, Align: s.align})
			if uint64(result.Size)+uint64(s.size) > uint64(^uint32(0)) {
				return ResolvedType{}, fmt.Errorf("type %q is too large", name)
			}
			result.Size += s.size
			if s.align > result.Align {
				result.Align = s.align
			}
		}
		finalSize, fits := alignUp(result.Size, result.Align)
		if !fits {
			return ResolvedType{}, fmt.Errorf("type %q is too large", name)
		}
		result.Size = finalSize
		resolved[name] = result
		return result, nil
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	manifest := Manifest{Version: ManifestVersion, Package: input.Package, Types: make([]ResolvedType, 0, len(names))}
	for _, name := range names {
		typ, err := compileType(name)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Types = append(manifest.Types, typ)
	}
	seenFunctions := make(map[string]struct{}, len(input.Functions))
	manifest.Functions = make([]ResolvedFunction, 0, len(input.Functions))
	for _, function := range input.Functions {
		if !identifier.MatchString(function.Name) {
			return Manifest{}, fmt.Errorf("function %q is not an identifier", function.Name)
		}
		if _, exists := seenFunctions[function.Name]; exists {
			return Manifest{}, fmt.Errorf("duplicate function %q", function.Name)
		}
		seenFunctions[function.Name] = struct{}{}
		resolvedFunction := ResolvedFunction{Name: function.Name, Result: function.Result, OwnsResult: function.OwnsResult, Parameters: append([]Parameter(nil), function.Parameters...)}
		if function.OwnsResult && function.Result == "" {
			return Manifest{}, fmt.Errorf("%s cannot own an empty result", function.Name)
		}
		seenParameters := make(map[string]struct{}, len(function.Parameters))
		for _, parameter := range function.Parameters {
			if !identifier.MatchString(parameter.Name) {
				return Manifest{}, fmt.Errorf("%s parameter %q is not an identifier", function.Name, parameter.Name)
			}
			if _, exists := seenParameters[parameter.Name]; exists {
				return Manifest{}, fmt.Errorf("%s has duplicate parameter %q", function.Name, parameter.Name)
			}
			seenParameters[parameter.Name] = struct{}{}
			if _, exists := definitions[parameter.Type]; !exists {
				return Manifest{}, fmt.Errorf("%s.%s: boundary parameters currently require a record, got %q", function.Name, parameter.Name, parameter.Type)
			}
		}
		if function.Result != "" {
			if _, exists := definitions[function.Result]; !exists {
				return Manifest{}, fmt.Errorf("%s result currently requires a record, got %q", function.Name, function.Result)
			}
		}
		manifest.Functions = append(manifest.Functions, resolvedFunction)
	}
	sort.Slice(manifest.Functions, func(i, j int) bool { return manifest.Functions[i].Name < manifest.Functions[j].Name })
	canonical, err := json.Marshal(struct {
		Version   uint32             `json:"version"`
		Package   string             `json:"package"`
		Types     []ResolvedType     `json:"types"`
		Functions []ResolvedFunction `json:"functions,omitempty"`
	}{manifest.Version, manifest.Package, manifest.Types, manifest.Functions})
	if err != nil {
		return Manifest{}, err
	}
	sum := sha256.Sum256(canonical)
	manifest.Fingerprint = hex.EncodeToString(sum[:16])
	return manifest, nil
}

func alignUp(n, align uint32) (uint32, bool) {
	value := (uint64(n) + uint64(align-1)) &^ uint64(align-1)
	return uint32(value), value <= uint64(^uint32(0))
}
func wrapped(value, kind string) (string, bool) {
	prefix := kind + "<"
	if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, ">") {
		inner := strings.TrimSpace(value[len(prefix) : len(value)-1])
		return inner, inner != ""
	}
	return "", false
}

type arrayType struct {
	element string
	count   uint32
}

func fixedArray(value string) (arrayType, bool) {
	if !strings.HasPrefix(value, "array<") || !strings.HasSuffix(value, ">") {
		return arrayType{}, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "array<"), ">"), ",")
	if len(parts) != 2 {
		return arrayType{}, false
	}
	element := strings.TrimSpace(parts[0])
	count, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil || element == "" || count == 0 {
		return arrayType{}, false
	}
	return arrayType{element, uint32(count)}, true
}
