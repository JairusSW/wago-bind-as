package bindas

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

type fakeMemory []byte

func (m fakeMemory) UnsafeBytes() []byte { return m }

type fakeInspect struct {
	memory  []byte
	results []uint64
}

type fakePointerInspect struct{ memory []byte }

func (f fakePointerInspect) Invoke1(pointer uint64) ([]uint64, error) {
	bits := binary.LittleEndian.Uint32(f.memory[pointer+8:])
	binary.LittleEndian.PutUint32(f.memory[pointer+8:], math.Float32bits(math.Float32frombits(bits)+10))
	return nil, nil
}

func (f fakeInspect) Invoke3(base, length, root uint64) ([]uint64, error) {
	if root+12 > length {
		return []uint64{uint64(math.MaxUint32)}, nil
	}
	address := base + root + 8
	bits := binary.LittleEndian.Uint32(f.memory[address:])
	binary.LittleEndian.PutUint32(f.memory[address:], math.Float32bits(math.Float32frombits(bits)+10))
	f.results[0] = 0
	return f.results, nil
}

func TestPreparedTransactionReacquiresAfterGuestMutation(t *testing.T) {
	memory := make(fakeMemory, 4096)
	prepared, err := NewPrepared(memory, fakeInspect{memory: memory, results: make([]uint64, 1)}, 512, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var escaped Record
	err = prepared.Transaction(func(arena *Arena) (Offset, error) {
		offset, err := arena.Alloc(16, 8)
		if err != nil {
			return 0, err
		}
		escaped, err = arena.Region().Record(offset, 16, 8)
		if err != nil {
			return 0, err
		}
		return offset, escaped.SetFloat32(8, 2.5)
	}, func(region Region, root Offset, status int32) error {
		if status != 0 {
			t.Fatalf("status = %d", status)
		}
		if _, err := escaped.Float32(8); !errors.Is(err, ErrStale) {
			t.Fatalf("escaped view error = %v", err)
		}
		record, err := region.Record(root, 16, 8)
		if err != nil {
			return err
		}
		value, err := record.Float32(8)
		if err != nil {
			return err
		}
		if value != 12.5 {
			t.Fatalf("score = %f", value)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreparedPointerValidatesRootBeforeInvocation(t *testing.T) {
	memory := make(fakeMemory, 256)
	prepared, err := NewPreparedPointer(memory, fakePointerInspect{memory}, 64, 128, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Call(120); !errors.Is(err, ErrBounds) {
		t.Fatalf("short root error = %v", err)
	}
	if _, err := prepared.Call(4); !errors.Is(err, ErrMalformed) {
		t.Fatalf("misaligned root error = %v", err)
	}
	if _, err := prepared.Call(8); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkPreparedCall(b *testing.B) {
	memory := make(fakeMemory, 64<<10)
	prepared, _ := NewPrepared(memory, fakeInspect{memory: memory, results: make([]uint64, 1)}, 1024, 32<<10, WithoutResetZeroing())
	var root Offset
	_ = prepared.WithArena(func(arena *Arena) error { root, _ = arena.Alloc(16, 8); return nil })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := prepared.Call(root); err != nil {
			b.Fatal(err)
		}
	}
}
