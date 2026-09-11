package bindas

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"unicode/utf8"
	"unsafe"
)

// Region is a cheap, generation-checked view over one wire arena. Use
// OpenRegion for detached/read-only images and Arena.Region for live mutation.
type Region struct {
	memory     []byte
	start      uint32
	limit      uint32
	owner      *Arena
	generation uint64
	writable   bool
}

func OpenRegion(memory []byte, start, length uint32, writable bool) (Region, error) {
	end := uint64(start) + uint64(length)
	if end > uint64(len(memory)) || end > math.MaxUint32 {
		return Region{}, ErrBounds
	}
	return Region{memory: memory, start: start, limit: uint32(end), writable: writable}, nil
}

func (r Region) Length() uint32 { return r.limit - r.start }

func (r Region) checkLive() error {
	if r.owner != nil && r.owner.generation != r.generation {
		return ErrStale
	}
	return nil
}

func (r Region) resolve(offset Offset, size, align uint32, write bool) ([]byte, error) {
	if err := r.checkLive(); err != nil {
		return nil, err
	}
	if write && !r.writable {
		return nil, ErrReadOnly
	}
	if !validAlign(align) || uint32(offset)&(align-1) != 0 {
		return nil, ErrMalformed
	}
	start := uint64(r.start) + uint64(offset)
	end := start + uint64(size)
	if start > uint64(r.limit) || end > uint64(r.limit) || end < start {
		return nil, ErrBounds
	}
	return r.memory[int(start):int(end):int(end)], nil
}

func (r Region) Record(offset Offset, size, align uint32) (Record, error) {
	if _, err := r.resolve(offset, size, align, false); err != nil {
		return Record{}, err
	}
	return Record{region: r, offset: offset, size: size}, nil
}

type Record struct {
	region Region
	offset Offset
	size   uint32
}

func (v Record) Offset() Offset { return v.offset }

func (v Record) Bool(field uint32) (bool, error) {
	n, err := v.Uint8(field)
	if err != nil {
		return false, err
	}
	if n > 1 {
		return false, ErrMalformed
	}
	return n != 0, nil
}
func (v Record) SetBool(field uint32, value bool) error {
	if value {
		return v.SetUint8(field, 1)
	}
	return v.SetUint8(field, 0)
}

// RegionSpan resolves an inline span descriptor in this record.
func (v Record) RegionSpan(field uint32) (SpanView, error) { return v.region.Span(v.offset, field) }
func (v Record) SetRegionSpan(field uint32, span Span) error {
	return v.region.PutSpan(v.offset, field, span)
}
func (v Record) RegionVector(field, stride, align uint32) (Vector, error) {
	return v.region.Vector(v.offset, field, stride, align)
}

// FieldBytes returns an exact callback-scoped view of a fixed-size field.
func (v Record) FieldBytes(field, size, align uint32) ([]byte, error) {
	return v.field(field, size, align, false)
}

func (v Record) MutableFieldBytes(field, size, align uint32) ([]byte, error) {
	return v.field(field, size, align, true)
}

func (v Record) field(field, size, align uint32, write bool) ([]byte, error) {
	if uint64(field)+uint64(size) > uint64(v.size) {
		return nil, ErrBounds
	}
	return v.region.resolve(v.offset+Offset(field), size, align, write)
}

func (v Record) Uint8(field uint32) (uint8, error) {
	b, e := v.field(field, 1, 1, false)
	if e != nil {
		return 0, e
	}
	return b[0], nil
}
func (v Record) Uint16(field uint32) (uint16, error) {
	b, e := v.field(field, 2, 2, false)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint16(b), nil
}
func (v Record) Uint32(field uint32) (uint32, error) {
	b, e := v.field(field, 4, 4, false)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint32(b), nil
}
func (v Record) Uint64(field uint32) (uint64, error) {
	b, e := v.field(field, 8, 8, false)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint64(b), nil
}
func (v Record) Int8(field uint32) (int8, error)   { n, e := v.Uint8(field); return int8(n), e }
func (v Record) Int16(field uint32) (int16, error) { n, e := v.Uint16(field); return int16(n), e }
func (v Record) Int32(field uint32) (int32, error) { n, e := v.Uint32(field); return int32(n), e }
func (v Record) Int64(field uint32) (int64, error) { n, e := v.Uint64(field); return int64(n), e }
func (v Record) Float32(field uint32) (float32, error) {
	n, e := v.Uint32(field)
	return math.Float32frombits(n), e
}
func (v Record) Float64(field uint32) (float64, error) {
	n, e := v.Uint64(field)
	return math.Float64frombits(n), e
}

func (v Record) SetUint8(field uint32, n uint8) error {
	b, e := v.field(field, 1, 1, true)
	if e == nil {
		b[0] = n
	}
	return e
}
func (v Record) SetUint16(field uint32, n uint16) error {
	b, e := v.field(field, 2, 2, true)
	if e == nil {
		binary.LittleEndian.PutUint16(b, n)
	}
	return e
}
func (v Record) SetUint32(field uint32, n uint32) error {
	b, e := v.field(field, 4, 4, true)
	if e == nil {
		binary.LittleEndian.PutUint32(b, n)
	}
	return e
}
func (v Record) SetUint64(field uint32, n uint64) error {
	b, e := v.field(field, 8, 8, true)
	if e == nil {
		binary.LittleEndian.PutUint64(b, n)
	}
	return e
}
func (v Record) SetInt8(field uint32, n int8) error   { return v.SetUint8(field, uint8(n)) }
func (v Record) SetInt16(field uint32, n int16) error { return v.SetUint16(field, uint16(n)) }
func (v Record) SetInt32(field uint32, n int32) error { return v.SetUint32(field, uint32(n)) }
func (v Record) SetInt64(field uint32, n int64) error { return v.SetUint64(field, uint64(n)) }
func (v Record) SetFloat32(field uint32, n float32) error {
	return v.SetUint32(field, math.Float32bits(n))
}
func (v Record) SetFloat64(field uint32, n float64) error {
	return v.SetUint64(field, math.Float64bits(n))
}

type Span struct {
	Offset Offset
	Length uint32
}
type VectorDesc struct {
	Offset           Offset
	Length, Capacity uint32
}

func (r Region) Span(record Offset, field uint32) (SpanView, error) {
	descriptor, ok := addOffset(record, field)
	if !ok {
		return SpanView{}, ErrBounds
	}
	b, err := r.resolve(descriptor, 8, 4, false)
	if err != nil {
		return SpanView{}, err
	}
	d := Span{Offset: Offset(binary.LittleEndian.Uint32(b)), Length: binary.LittleEndian.Uint32(b[4:])}
	if _, err = r.resolve(d.Offset, d.Length, 1, false); err != nil {
		return SpanView{}, ErrMalformed
	}
	return SpanView{region: r, span: d}, nil
}

func (r Region) PutSpan(record Offset, field uint32, span Span) error {
	if _, err := r.resolve(span.Offset, span.Length, 1, false); err != nil {
		return ErrMalformed
	}
	descriptor, ok := addOffset(record, field)
	if !ok {
		return ErrBounds
	}
	b, err := r.resolve(descriptor, 8, 4, true)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b, uint32(span.Offset))
	binary.LittleEndian.PutUint32(b[4:], span.Length)
	return nil
}

type SpanView struct {
	region Region
	span   Span
}

func (v SpanView) Len() uint32    { return v.span.Length }
func (v SpanView) Offset() Offset { return v.span.Offset }
func (v SpanView) Bytes() ([]byte, error) {
	return v.region.resolve(v.span.Offset, v.span.Length, 1, false)
}
func (v SpanView) MutableBytes() ([]byte, error) {
	return v.region.resolve(v.span.Offset, v.span.Length, 1, true)
}
func (v SpanView) SliceBytes(start, end uint32) (SpanView, error) {
	if start > end || end > v.span.Length {
		return SpanView{}, ErrBounds
	}
	offset, ok := addOffset(v.span.Offset, start)
	if !ok {
		return SpanView{}, ErrBounds
	}
	return SpanView{region: v.region, span: Span{Offset: offset, Length: end - start}}, nil
}
func (v SpanView) UTF8() (UTF8View, error) {
	b, err := v.Bytes()
	if err != nil {
		return UTF8View{}, err
	}
	if !utf8.Valid(b) {
		return UTF8View{}, ErrInvalidUTF8
	}
	return UTF8View{SpanView: v}, nil
}

type UTF8View struct{ SpanView }

func (v UTF8View) EqualString(s string) bool {
	b, err := v.Bytes()
	return err == nil && stringView(b) == s
}
func (v UTF8View) StartsWithString(s string) bool {
	b, err := v.Bytes()
	return err == nil && len(b) >= len(s) && stringView(b[:len(s)]) == s
}
func (v UTF8View) EndsWithString(s string) bool {
	b, err := v.Bytes()
	return err == nil && len(b) >= len(s) && stringView(b[len(b)-len(s):]) == s
}
func (v UTF8View) IndexByte(value byte) (int, error) {
	b, err := v.Bytes()
	if err != nil {
		return -1, err
	}
	return bytes.IndexByte(b, value), nil
}
func (v UTF8View) EqualFoldASCII(s string) bool {
	b, err := v.Bytes()
	if err != nil || len(b) != len(s) {
		return false
	}
	for i, a := range b {
		c := s[i]
		if a >= 'A' && a <= 'Z' {
			a |= 0x20
		}
		if c >= 'A' && c <= 'Z' {
			c |= 0x20
		}
		if a != c {
			return false
		}
	}
	return true
}
func (v UTF8View) FNV1a32() (uint32, error) {
	b, err := v.Bytes()
	if err != nil {
		return 0, err
	}
	hash := uint32(2166136261)
	for _, value := range b {
		hash ^= uint32(value)
		hash *= 16777619
	}
	return hash, nil
}
func (v UTF8View) ParseInt(base, bitSize int) (int64, error) {
	b, err := v.Bytes()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(stringView(b), base, bitSize)
}
func (v UTF8View) SliceText(start, end uint32) (UTF8View, error) {
	b, err := v.Bytes()
	if err != nil {
		return UTF8View{}, err
	}
	if start > end || end > uint32(len(b)) || (start < uint32(len(b)) && !utf8.RuneStart(b[start])) || (end < uint32(len(b)) && !utf8.RuneStart(b[end])) {
		return UTF8View{}, ErrInvalidUTF8
	}
	slice, err := v.SliceBytes(start, end)
	if err != nil {
		return UTF8View{}, err
	}
	return UTF8View{SpanView: slice}, nil
}
func (v UTF8View) CopyString() (string, error) {
	b, err := v.Bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BorrowedString performs no copy. The result expires with the Region and must
// never outlive or race mutation of its backing bytes.
func (v UTF8View) BorrowedString() (string, error) {
	b, err := v.Bytes()
	if err != nil {
		return "", err
	}
	return stringView(b), nil
}

func stringView(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (r Region) PutVector(record Offset, field uint32, d VectorDesc) error {
	if d.Length > d.Capacity {
		return ErrMalformed
	}
	descriptor, ok := addOffset(record, field)
	if !ok {
		return ErrBounds
	}
	b, err := r.resolve(descriptor, 12, 4, true)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b, uint32(d.Offset))
	binary.LittleEndian.PutUint32(b[4:], d.Length)
	binary.LittleEndian.PutUint32(b[8:], d.Capacity)
	return nil
}

func (r Region) Vector(record Offset, field, stride, align uint32) (Vector, error) {
	if stride == 0 || !validAlign(align) {
		return Vector{}, ErrMalformed
	}
	descriptor, ok := addOffset(record, field)
	if !ok {
		return Vector{}, ErrBounds
	}
	b, err := r.resolve(descriptor, 12, 4, false)
	if err != nil {
		return Vector{}, err
	}
	d := VectorDesc{Offset: Offset(binary.LittleEndian.Uint32(b)), Length: binary.LittleEndian.Uint32(b[4:]), Capacity: binary.LittleEndian.Uint32(b[8:])}
	if d.Length > d.Capacity {
		return Vector{}, ErrMalformed
	}
	storage, ok := checkedProduct(d.Capacity, stride)
	if !ok {
		return Vector{}, ErrMalformed
	}
	if _, err := r.resolve(d.Offset, storage, align, false); err != nil {
		return Vector{}, ErrMalformed
	}
	return Vector{region: r, descriptor: descriptor, offset: d.Offset, length: d.Length, capacity: d.Capacity, stride: stride, align: align}, nil
}

func addOffset(offset Offset, delta uint32) (Offset, bool) {
	value := uint64(offset) + uint64(delta)
	return Offset(value), value <= math.MaxUint32
}

type Vector struct {
	region                          Region
	descriptor, offset              Offset
	length, capacity, stride, align uint32
}

func (v Vector) Len() uint32 { return v.length }
func (v Vector) Cap() uint32 { return v.capacity }
func (v Vector) storage() ([]byte, error) {
	size, ok := checkedProduct(v.capacity, v.stride)
	if !ok {
		return nil, ErrMalformed
	}
	return v.region.resolve(v.offset, size, v.align, false)
}
func (v Vector) At(index uint32) (Record, error) {
	if index >= v.length {
		return Record{}, ErrBounds
	}
	delta, ok := checkedProduct(index, v.stride)
	if !ok {
		return Record{}, ErrBounds
	}
	return v.region.Record(v.offset+Offset(delta), v.stride, v.align)
}
func (v *Vector) Append() (Record, error) {
	if v.length >= v.capacity {
		return Record{}, ErrOutOfSpace
	}
	delta, ok := checkedProduct(v.length, v.stride)
	if !ok {
		return Record{}, ErrBounds
	}
	record, err := v.region.Record(v.offset+Offset(delta), v.stride, v.align)
	if err != nil {
		return Record{}, err
	}
	slot, err := record.field(0, v.stride, v.align, true)
	if err != nil {
		return Record{}, err
	}
	clear(slot)
	v.length++
	b, err := v.region.resolve(v.descriptor+4, 4, 4, true)
	if err != nil {
		v.length--
		return Record{}, err
	}
	binary.LittleEndian.PutUint32(b, v.length)
	return record, nil
}
