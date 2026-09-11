package bindas

import (
	"math"
	"unicode/utf8"
)

// Offset is a byte offset relative to the beginning of a region. Offset zero
// is valid; optional references use a separate presence bit or sentinel.
type Offset uint32

// Arena allocates wire values directly in WebAssembly linear memory. Arena is
// deliberately not concurrency-safe: ownership transfers at call boundaries.
type Arena struct {
	memory     []byte
	start      uint32
	limit      uint32
	cursor     uint32
	generation uint64
	zeroReset  bool
}

// ArenaOption changes arena behavior.
type ArenaOption func(*Arena)

// Checkpoint identifies a rollback point in one arena generation.
type Checkpoint struct {
	arena      *Arena
	cursor     uint32
	generation uint64
}

// WithoutResetZeroing retains initialized bytes on Reset. It is useful only
// when the caller can prove that recycled data cannot cross a trust boundary.
func WithoutResetZeroing() ArenaOption { return func(a *Arena) { a.zeroReset = false } }

// NewArena binds a bounded part of memory. start and length are absolute byte
// positions in the memory slice; references allocated by the arena are region
// relative. Reset invalidates every previously opened Region and view.
func NewArena(memory []byte, start, length uint32, options ...ArenaOption) (*Arena, error) {
	end := uint64(start) + uint64(length)
	if end > uint64(len(memory)) || end > math.MaxUint32 {
		return nil, ErrBounds
	}
	a := &Arena{memory: memory, start: start, limit: uint32(end), cursor: start, generation: 1, zeroReset: true}
	for _, option := range options {
		if option != nil {
			option(a)
		}
	}
	return a, nil
}

func (a *Arena) Capacity() uint32   { return a.limit - a.start }
func (a *Arena) Used() uint32       { return a.cursor - a.start }
func (a *Arena) Remaining() uint32  { return a.limit - a.cursor }
func (a *Arena) Generation() uint64 { return a.generation }

// Refresh replaces the host slice after a call or memory growth and invalidates
// all outstanding views without reclaiming wire data.
func (a *Arena) Refresh(memory []byte) error {
	if uint64(a.limit) > uint64(len(memory)) {
		return ErrBounds
	}
	a.memory = memory
	a.invalidate()
	return nil
}

// Region opens a checked mutable view of the arena's current generation.
func (a *Arena) Region() Region {
	return Region{memory: a.memory, start: a.start, limit: a.limit, owner: a, generation: a.generation, writable: true}
}

func (a *Arena) Checkpoint() Checkpoint {
	return Checkpoint{arena: a, cursor: a.cursor, generation: a.generation}
}

// Rollback clears and reclaims allocations made after checkpoint. Views opened
// before the checkpoint remain valid; failed builders do not expose later ones.
func (a *Arena) Rollback(checkpoint Checkpoint) error {
	if checkpoint.arena != a || checkpoint.generation != a.generation {
		return ErrStale
	}
	if checkpoint.cursor < a.start || checkpoint.cursor > a.cursor {
		return ErrMalformed
	}
	if a.zeroReset {
		clear(a.memory[checkpoint.cursor:a.cursor])
	}
	a.cursor = checkpoint.cursor
	return nil
}

// Reset reclaims all values at once and invalidates outstanding views.
func (a *Arena) Reset() {
	if a.zeroReset {
		clear(a.memory[a.start:a.cursor])
	}
	a.cursor = a.start
	a.invalidate()
}

func (a *Arena) invalidate() {
	a.generation++
	if a.generation == 0 {
		a.generation = 1
	}
}

// Alloc reserves zero-initialized storage and returns a region-relative offset.
func (a *Arena) Alloc(size, align uint32) (Offset, error) {
	if !validAlign(align) {
		return 0, ErrInvalidAlign
	}
	relative := uint64(a.cursor - a.start)
	alignedRelative := (relative + uint64(align-1)) &^ uint64(align-1)
	aligned := uint64(a.start) + alignedRelative
	end := aligned + uint64(size)
	if end > uint64(a.limit) || end > math.MaxUint32 {
		return 0, ErrOutOfSpace
	}
	// Clear alignment padding as well as the allocation so detached images never
	// expose bytes left by a prior tenant of the backing memory.
	clear(a.memory[int(a.cursor):int(end)])
	a.cursor = uint32(end)
	return Offset(uint32(aligned) - a.start), nil
}

func (a *Arena) PutBytes(value []byte, align uint32) (Span, error) {
	if uint64(len(value)) > math.MaxUint32 {
		return Span{}, ErrOutOfSpace
	}
	span, destination, err := a.ReserveBytes(uint32(len(value)), align)
	if err != nil {
		return Span{}, err
	}
	copy(destination, value)
	return span, nil
}

// ReserveBytes returns storage a producer can fill directly, avoiding an
// intermediate staging buffer. The slice expires at the next ownership
// transition just like every other arena view.
func (a *Arena) ReserveBytes(length, align uint32) (Span, []byte, error) {
	offset, err := a.Alloc(length, align)
	if err != nil {
		return Span{}, nil, err
	}
	bytes, err := a.Region().resolve(offset, length, align, true)
	if err != nil {
		return Span{}, nil, err
	}
	return Span{Offset: offset, Length: length}, bytes, nil
}

// ValidateUTF8 verifies a directly-filled span before assigning it to an
// `utf8` field.
func (a *Arena) ValidateUTF8(span Span) error {
	bytes, err := a.Region().resolve(span.Offset, span.Length, 1, false)
	if err != nil {
		return err
	}
	if !utf8.Valid(bytes) {
		return ErrInvalidUTF8
	}
	return nil
}

// BeginImage reserves the detached Exact32 envelope at region offset zero.
// Call it before allocating the root or any payload.
func (a *Arena) BeginImage() error {
	if a.Used() != 0 {
		return ErrMalformed
	}
	offset, err := a.Alloc(EnvelopeSize, 8)
	if err != nil {
		return err
	}
	if offset != 0 {
		return ErrMalformed
	}
	return nil
}

// SealImage writes the envelope and returns a dense owned copy of initialized
// bytes. Superseded arena allocations remain part of the image; compaction is a
// separate traversal operation.
func (a *Arena) SealImage(root Offset, fingerprint Fingerprint) ([]byte, error) {
	used := a.Used()
	if used < EnvelopeSize || uint32(root) >= used {
		return nil, ErrMalformed
	}
	image := a.memory[a.start : a.start+used]
	if err := WriteEnvelope(image, Envelope{Extent: used, Root: root, Fingerprint: fingerprint}); err != nil {
		return nil, err
	}
	return append([]byte(nil), image...), nil
}

func (a *Arena) PutUTF8(value string) (Span, error) {
	if !utf8.ValidString(value) {
		return Span{}, ErrInvalidUTF8
	}
	if uint64(len(value)) > math.MaxUint32 {
		return Span{}, ErrOutOfSpace
	}
	span, destination, err := a.ReserveBytes(uint32(len(value)), 1)
	if err != nil {
		return Span{}, err
	}
	copy(destination, value)
	return span, nil
}

// GrowVector replaces a vector payload with a larger same-region allocation.
// Existing views still refer to the old allocation and must be reacquired.
func (a *Arena) GrowVector(record Offset, field, stride, align, minimumCapacity uint32) (Vector, error) {
	r := a.Region()
	v, err := r.Vector(record, field, stride, align)
	if err != nil {
		return Vector{}, err
	}
	if minimumCapacity <= v.capacity {
		return v, nil
	}
	capacity := v.capacity
	if capacity == 0 {
		capacity = 1
	}
	for capacity < minimumCapacity {
		next := uint64(capacity) * 2
		if next > math.MaxUint32 {
			capacity = minimumCapacity
			break
		}
		capacity = uint32(next)
	}
	bytes, ok := checkedProduct(capacity, stride)
	if !ok {
		return Vector{}, ErrOutOfSpace
	}
	off, err := a.Alloc(bytes, align)
	if err != nil {
		return Vector{}, err
	}
	old, err := v.storage()
	if err != nil {
		return Vector{}, err
	}
	dst, err := r.resolve(off, bytes, align, true)
	if err != nil {
		return Vector{}, err
	}
	copy(dst, old[:v.length*stride])
	if err := r.PutVector(record, field, VectorDesc{Offset: off, Length: v.length, Capacity: capacity}); err != nil {
		return Vector{}, err
	}
	return r.Vector(record, field, stride, align)
}

func validAlign(align uint32) bool { return align != 0 && align&(align-1) == 0 }

func checkedProduct(a, b uint32) (uint32, bool) {
	n := uint64(a) * uint64(b)
	return uint32(n), n <= math.MaxUint32
}
