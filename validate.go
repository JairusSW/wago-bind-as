package bindas

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JairusSW/wago-bind-as/schema"
)

// ValidationLimits bound complete semantic validation of untrusted images.
// Zero fields select conservative defaults.
type ValidationLimits struct {
	MaxDepth          uint32
	MaxObjects        uint32
	MaxVectorElements uint32
}

type validationState struct {
	region   Region
	types    map[string]schema.ResolvedType
	seen     map[validationKey]struct{}
	limits   ValidationLimits
	objects  uint32
	elements uint32
}

type validationKey struct {
	typ    string
	offset Offset
}

// Validate performs a complete semantic walk from root. Ordinary generated
// field access does not call this; use it for detached, persisted, or otherwise
// untrusted images before accepting them into a trusted live region.
func Validate(region Region, manifest schema.Manifest, rootType string, root Offset, limits ValidationLimits) error {
	if limits.MaxDepth == 0 {
		limits.MaxDepth = 256
	}
	if limits.MaxObjects == 0 {
		limits.MaxObjects = 1 << 20
	}
	if limits.MaxVectorElements == 0 {
		limits.MaxVectorElements = 1 << 24
	}
	types := make(map[string]schema.ResolvedType, len(manifest.Types))
	for _, typ := range manifest.Types {
		types[typ.Name] = typ
	}
	if _, ok := types[rootType]; !ok {
		return fmt.Errorf("%w: unknown root type %q", ErrSchema, rootType)
	}
	state := validationState{region: region, types: types, seen: make(map[validationKey]struct{}), limits: limits}
	return state.record(rootType, root, 0)
}

func (s *validationState) record(name string, offset Offset, depth uint32) error {
	if depth > s.limits.MaxDepth {
		return ErrValidationLimit
	}
	key := validationKey{name, offset}
	if _, ok := s.seen[key]; ok {
		return nil
	}
	if s.objects >= s.limits.MaxObjects {
		return ErrValidationLimit
	}
	s.objects++
	s.seen[key] = struct{}{}
	typ, ok := s.types[name]
	if !ok {
		return fmt.Errorf("%w: unknown type %q", ErrSchema, name)
	}
	record, err := s.region.Record(offset, typ.Size, typ.Align)
	if err != nil {
		return fmt.Errorf("%s at %d: %w", name, offset, err)
	}
	for _, field := range typ.Fields {
		if err := s.field(record, field, depth); err != nil {
			return fmt.Errorf("%s.%s: %w", name, field.Name, err)
		}
	}
	return nil
}

func (s *validationState) field(record Record, field schema.ResolvedField, depth uint32) error {
	switch field.Type {
	case "bool":
		_, err := record.Bool(field.Offset)
		return err
	case "utf8":
		view, err := record.RegionSpan(field.Offset)
		if err != nil {
			return err
		}
		_, err = view.UTF8()
		return err
	case "bytes":
		_, err := record.RegionSpan(field.Offset)
		return err
	case "i8", "u8", "i16", "u16", "i32", "u32", "f32", "i64", "u64", "f64", "timestamp_ns", "duration_ns", "uuid", "digest256":
		return nil
	}
	if inner, ok := validationWrapped(field.Type, "ref"); ok {
		offset, err := record.Uint32(field.Offset)
		if err != nil {
			return err
		}
		return s.record(inner, Offset(offset), depth+1)
	}
	if inner, ok := validationWrapped(field.Type, "vec"); ok {
		return s.vector(record, field, inner, depth)
	}
	if inner, count, ok := validationArray(field.Type); ok {
		return s.array(record, field, inner, count, depth)
	}
	if _, ok := s.types[field.Type]; ok {
		return s.record(field.Type, record.Offset()+Offset(field.Offset), depth+1)
	}
	return fmt.Errorf("%w: unsupported type %q", ErrSchema, field.Type)
}

func (s *validationState) vector(record Record, field schema.ResolvedField, inner string, depth uint32) error {
	stride, align, ok := s.shape(inner)
	if !ok {
		return fmt.Errorf("%w: unknown vector element %q", ErrSchema, inner)
	}
	vector, err := record.RegionVector(field.Offset, stride, align)
	if err != nil {
		return err
	}
	if uint64(s.elements)+uint64(vector.length) > uint64(s.limits.MaxVectorElements) {
		return ErrValidationLimit
	}
	s.elements += vector.length
	for i := uint32(0); i < vector.length; i++ {
		delta, _ := checkedProduct(i, stride)
		offset := vector.offset + Offset(delta)
		if err := s.element(inner, offset, stride, align, depth+1); err != nil {
			return fmt.Errorf("element %d: %w", i, err)
		}
	}
	return nil
}

func (s *validationState) array(record Record, field schema.ResolvedField, inner string, count uint32, depth uint32) error {
	stride, align, ok := s.shape(inner)
	if !ok {
		return fmt.Errorf("%w: unknown array element %q", ErrSchema, inner)
	}
	if uint64(s.elements)+uint64(count) > uint64(s.limits.MaxVectorElements) {
		return ErrValidationLimit
	}
	s.elements += count
	base := record.Offset() + Offset(field.Offset)
	for i := uint32(0); i < count; i++ {
		delta, _ := checkedProduct(i, stride)
		if err := s.element(inner, base+Offset(delta), stride, align, depth+1); err != nil {
			return fmt.Errorf("element %d: %w", i, err)
		}
	}
	return nil
}

func (s *validationState) element(inner string, offset Offset, size, align uint32, depth uint32) error {
	if _, ok := s.types[inner]; ok {
		return s.record(inner, offset, depth)
	}
	record, err := s.region.Record(offset, size, align)
	if err != nil {
		return err
	}
	switch inner {
	case "bool":
		_, err = record.Bool(0)
		return err
	case "utf8":
		view, e := record.RegionSpan(0)
		if e != nil {
			return e
		}
		_, e = view.UTF8()
		return e
	case "bytes":
		_, err = record.RegionSpan(0)
		return err
	default:
		return nil
	}
}

func (s *validationState) shape(name string) (uint32, uint32, bool) {
	if typ, ok := s.types[name]; ok {
		return typ.Size, typ.Align, true
	}
	switch name {
	case "bool", "i8", "u8":
		return 1, 1, true
	case "i16", "u16":
		return 2, 2, true
	case "i32", "u32", "f32":
		return 4, 4, true
	case "i64", "u64", "f64", "timestamp_ns", "duration_ns":
		return 8, 8, true
	case "utf8", "bytes":
		return 8, 4, true
	case "uuid":
		return 16, 8, true
	case "digest256":
		return 32, 8, true
	default:
		return 0, 0, false
	}
}

func validationWrapped(value, kind string) (string, bool) {
	prefix := kind + "<"
	if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, ">") {
		inner := strings.TrimSpace(value[len(prefix) : len(value)-1])
		return inner, inner != ""
	}
	return "", false
}
func validationArray(value string) (string, uint32, bool) {
	if !strings.HasPrefix(value, "array<") || !strings.HasSuffix(value, ">") {
		return "", 0, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "array<"), ">"), ",")
	if len(parts) != 2 {
		return "", 0, false
	}
	count, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil || count == 0 {
		return "", 0, false
	}
	return strings.TrimSpace(parts[0]), uint32(count), true
}
