package bindas

import (
	"fmt"
	"sync"
)

// LinearMemory is implemented by *wago.Memory. UnsafeBytes must remain valid
// for the duration of a call and must not race Close.
type LinearMemory interface{ UnsafeBytes() []byte }

// PreparedFunction3 is implemented by *wago.PreparedFunction for the standard
// (regionBase, regionLength, rootOffset) Exact32 signature.
type PreparedFunction3 interface {
	Invoke3(uint64, uint64, uint64) ([]uint64, error)
}

// PreparedFunction1 is implemented by pointer-shaped @bind exports. The one
// argument is the absolute linear-memory address of the unmanaged wire record.
type PreparedFunction1 interface {
	Invoke1(uint64) ([]uint64, error)
}

// Prepared owns the safe default call path. Calls and transactions through one
// Prepared are serialized. The underlying Wago instance must not be invoked or
// closed concurrently through another handle.
type Prepared struct {
	mu              sync.Mutex
	memory          LinearMemory
	function        PreparedFunction3
	pointerFunction PreparedFunction1
	rootSize        uint32
	rootAlign       uint32
	arena           *Arena
}

// NewPreparedPointer binds an ergonomic @bind export whose argument is a
// scalar-only unmanaged record. Prefer NewPrepared's three-argument checked
// ABI for records containing spans, vectors, or untrusted references.
func NewPreparedPointer(memory LinearMemory, function PreparedFunction1, start, length, rootSize, rootAlign uint32, options ...ArenaOption) (*Prepared, error) {
	if memory == nil || function == nil {
		return nil, fmt.Errorf("wago-bind-as: nil memory or prepared function")
	}
	if rootSize == 0 || !validAlign(rootAlign) {
		return nil, ErrMalformed
	}
	bytes := memory.UnsafeBytes()
	if bytes == nil {
		return nil, fmt.Errorf("wago-bind-as: memory is closed")
	}
	arena, err := NewArena(bytes, start, length, options...)
	if err != nil {
		return nil, err
	}
	return &Prepared{memory: memory, pointerFunction: function, rootSize: rootSize, rootAlign: rootAlign, arena: arena}, nil
}

func NewPrepared(memory LinearMemory, function PreparedFunction3, start, length uint32, options ...ArenaOption) (*Prepared, error) {
	if memory == nil || function == nil {
		return nil, fmt.Errorf("wago-bind-as: nil memory or prepared function")
	}
	bytes := memory.UnsafeBytes()
	if bytes == nil {
		return nil, fmt.Errorf("wago-bind-as: memory is closed")
	}
	arena, err := NewArena(bytes, start, length, options...)
	if err != nil {
		return nil, err
	}
	return &Prepared{memory: memory, function: function, arena: arena}, nil
}

// WithArena gives exclusive access for building or reading retained wire data.
// Values and slices obtained by fn must not escape the callback.
func (p *Prepared) WithArena(fn func(*Arena) error) error {
	if fn == nil {
		return fmt.Errorf("wago-bind-as: nil arena callback")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.refresh(); err != nil {
		return err
	}
	return fn(p.arena)
}

// Call invokes the prebound guest over existing wire-native data. It does not
// reset the arena, so repeated calls over the same object are allocation-free.
// Guest mutation invalidates all views opened before the call.
func (p *Prepared) Call(root Offset) (int32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.call(root)
}

// Transaction is the hands-off request path: reset, build, invoke, reacquire,
// read, and reclaim. No callback-scoped view may escape.
func (p *Prepared) Transaction(build func(*Arena) (Offset, error), read func(Region, Offset, int32) error) error {
	if build == nil {
		return fmt.Errorf("wago-bind-as: nil build callback")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.refresh(); err != nil {
		return err
	}
	p.arena.Reset()
	defer p.arena.Reset()
	root, err := build(p.arena)
	if err != nil {
		return err
	}
	status, err := p.call(root)
	if err != nil {
		return err
	}
	if read != nil {
		return read(p.arena.Region(), root, status)
	}
	return nil
}

func (p *Prepared) call(root Offset) (int32, error) {
	if err := p.refresh(); err != nil {
		return 0, err
	}
	if uint32(root) >= p.arena.Capacity() {
		return 0, ErrBounds
	}
	if p.pointerFunction != nil {
		if _, err := p.arena.Region().resolve(root, p.rootSize, p.rootAlign, false); err != nil {
			return 0, err
		}
	}
	var results []uint64
	var invokeErr error
	if p.pointerFunction != nil {
		results, invokeErr = p.pointerFunction.Invoke1(uint64(p.arena.start + uint32(root)))
	} else {
		results, invokeErr = p.function.Invoke3(uint64(p.arena.start), uint64(p.arena.Capacity()), uint64(root))
	}
	refreshErr := p.refresh()
	if invokeErr != nil {
		return 0, invokeErr
	}
	if refreshErr != nil {
		return 0, refreshErr
	}
	if len(results) == 0 {
		return 0, nil
	}
	return int32(uint32(results[0])), nil
}

func (p *Prepared) refresh() error {
	bytes := p.memory.UnsafeBytes()
	if bytes == nil {
		return fmt.Errorf("wago-bind-as: memory is closed")
	}
	return p.arena.Refresh(bytes)
}
