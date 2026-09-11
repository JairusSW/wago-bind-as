package bindas

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

func TestArenaRecordSpanVectorAndReset(t *testing.T) {
	memory := make([]byte, 4096)
	arena, err := NewArena(memory, 128, 2048)
	if err != nil {
		t.Fatal(err)
	}
	request, err := arena.Alloc(32, 8)
	if err != nil {
		t.Fatal(err)
	}
	region := arena.Region()
	record, err := region.Record(request, 32, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := record.SetUint64(0, 42); err != nil {
		t.Fatal(err)
	}
	if err := record.SetFloat32(8, 3.5); err != nil {
		t.Fatal(err)
	}
	name, err := arena.PutUTF8("administrator")
	if err != nil {
		t.Fatal(err)
	}
	if err := region.PutSpan(request, 12, name); err != nil {
		t.Fatal(err)
	}
	text, err := region.Span(request, 12)
	if err != nil {
		t.Fatal(err)
	}
	utf, err := text.UTF8()
	if err != nil {
		t.Fatal(err)
	}
	if !utf.StartsWithString("admin") || utf.EqualString("user") {
		t.Fatal("unexpected UTF-8 comparison")
	}

	items, err := arena.Alloc(3*8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := region.PutVector(request, 20, VectorDesc{Offset: items, Capacity: 3}); err != nil {
		t.Fatal(err)
	}
	vector, err := region.Vector(request, 20, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	item, err := vector.Append()
	if err != nil {
		t.Fatal(err)
	}
	if err := item.SetUint64(0, 99); err != nil {
		t.Fatal(err)
	}
	if vector.Len() != 1 {
		t.Fatalf("length = %d", vector.Len())
	}
	got, err := vector.At(0)
	if err != nil {
		t.Fatal(err)
	}
	value, err := got.Uint64(0)
	if err != nil || value != 99 {
		t.Fatalf("value = %d, err = %v", value, err)
	}

	used := arena.Used()
	arena.Reset()
	if _, err := record.Uint64(0); !errors.Is(err, ErrStale) {
		t.Fatalf("stale read error = %v", err)
	}
	for i, b := range memory[128 : 128+int(used)] {
		if b != 0 {
			t.Fatalf("byte %d not cleared", i)
		}
	}
}

func TestRandomSpanValidationMatchesWidenedReference(t *testing.T) {
	memory := make([]byte, 4096)
	region, err := OpenRegion(memory, 0, uint32(len(memory)), true)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := region.Record(0, 8, 4)
	rng := rand.New(rand.NewSource(0x57424153))
	for i := 0; i < 20_000; i++ {
		offset, length := rng.Uint32(), rng.Uint32()
		_ = record.SetUint32(0, offset)
		_ = record.SetUint32(4, length)
		_, gotErr := region.Span(0, 0)
		valid := uint64(offset)+uint64(length) <= uint64(len(memory))
		if (gotErr == nil) != valid {
			t.Fatalf("offset=%d length=%d valid=%v err=%v", offset, length, valid, gotErr)
		}
	}
}

func TestValidationRejectsOverflowAndMalformedDescriptors(t *testing.T) {
	memory := make([]byte, 64)
	region, err := OpenRegion(memory, 0, uint32(len(memory)), true)
	if err != nil {
		t.Fatal(err)
	}
	record, err := region.Record(0, 16, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := record.SetUint32(0, math.MaxUint32-3); err != nil {
		t.Fatal(err)
	}
	if err := record.SetUint32(4, 16); err != nil {
		t.Fatal(err)
	}
	if _, err := region.Span(0, 0); !errors.Is(err, ErrMalformed) {
		t.Fatalf("span error = %v", err)
	}
	if err := record.SetUint32(0, 16); err != nil {
		t.Fatal(err)
	}
	if err := record.SetUint32(4, 3); err != nil {
		t.Fatal(err)
	}
	if err := record.SetUint32(8, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := region.Vector(0, 0, math.MaxUint32, 1); !errors.Is(err, ErrMalformed) {
		t.Fatalf("vector error = %v", err)
	}
}

func TestGrowVectorPreservesElements(t *testing.T) {
	arena, err := NewArena(make([]byte, 1024), 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := arena.Alloc(16, 8)
	payload, _ := arena.Alloc(8, 4)
	r := arena.Region()
	if err := r.PutVector(root, 0, VectorDesc{Offset: payload, Length: 1, Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	item, _ := r.Record(payload, 4, 4)
	_ = item.SetUint32(0, 77)
	v, err := arena.GrowVector(root, 0, 4, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	if v.Cap() != 8 || v.Len() != 1 {
		t.Fatalf("len/cap = %d/%d", v.Len(), v.Cap())
	}
	item, err = v.At(0)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := item.Uint32(0)
	if got != 77 {
		t.Fatalf("preserved value = %d", got)
	}
}

func TestArenaAlignmentIsRegionRelative(t *testing.T) {
	arena, err := NewArena(make([]byte, 64), 3, 32)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = arena.Alloc(1, 1)
	offset, err := arena.Alloc(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if offset != 8 {
		t.Fatalf("offset = %d, want 8", offset)
	}
	if _, err := arena.Region().Record(offset, 8, 8); err != nil {
		t.Fatal(err)
	}
}

func TestDirectIngressAndDetachedRelocation(t *testing.T) {
	memory := make([]byte, 512)
	for i := range memory {
		memory[i] = 0xa5
	}
	arena, err := NewArena(memory, 3, 400)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.BeginImage(); err != nil {
		t.Fatal(err)
	}
	_, _ = arena.Alloc(1, 1)
	root, _ := arena.Alloc(16, 8)
	span, destination, err := arena.ReserveBytes(uint32(len("hello 世界")), 1)
	if err != nil {
		t.Fatal(err)
	}
	copy(destination, "hello 世界")
	if err := arena.ValidateUTF8(span); err != nil {
		t.Fatal(err)
	}
	r := arena.Region()
	if err := r.PutSpan(root, 0, span); err != nil {
		t.Fatal(err)
	}
	var fingerprint Fingerprint
	copy(fingerprint[:], "exact32-example!")
	image, err := arena.SealImage(root, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := ReadEnvelope(image, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	relocated := append(make([]byte, 17), image...)
	region, err := OpenRegion(relocated, 17, envelope.Extent, false)
	if err != nil {
		t.Fatal(err)
	}
	textSpan, err := region.Span(envelope.Root, 0)
	if err != nil {
		t.Fatal(err)
	}
	text, err := textSpan.UTF8()
	if err != nil {
		t.Fatal(err)
	}
	if !text.EqualString("hello 世界") {
		t.Fatal("relocated span did not preserve its target")
	}
	// The seven alignment bytes between the envelope and root are initialized.
	for i, value := range image[EnvelopeSize:uint32(root)] {
		if value != 0 {
			t.Fatalf("padding byte %d leaked %#x", i, value)
		}
	}
}

func TestEnvelope(t *testing.T) {
	var fp Fingerprint
	copy(fp[:], "0123456789abcdef")
	buf := make([]byte, 80)
	want := Envelope{Extent: 80, Root: 32, Fingerprint: fp}
	if err := WriteEnvelope(buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvelope(buf, fp)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	wrong := fp
	wrong[0]++
	if _, err := ReadEnvelope(buf, wrong); !errors.Is(err, ErrSchema) {
		t.Fatalf("schema error = %v", err)
	}
}

func TestUTF8WireOperations(t *testing.T) {
	arena, _ := NewArena(make([]byte, 256), 0, 256)
	root, _ := arena.Alloc(8, 4)
	value, _ := arena.PutUTF8("Admin/世界")
	r := arena.Region()
	_ = r.PutSpan(root, 0, value)
	span, _ := r.Span(root, 0)
	text, err := span.UTF8()
	if err != nil {
		t.Fatal(err)
	}
	if !text.EndsWithString("世界") || !text.EqualFoldASCII("admin/世界") {
		t.Fatal("text comparison failed")
	}
	index, _ := text.IndexByte('/')
	if index != 5 {
		t.Fatalf("index = %d", index)
	}
	hash, _ := text.FNV1a32()
	if hash == 0 {
		t.Fatal("zero hash")
	}
	slice, err := text.SliceText(6, uint32(len("Admin/世界")))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := slice.CopyString(); value != "世界" {
		t.Fatalf("slice = %q", value)
	}
	if _, err := text.SliceText(7, 8); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("boundary error = %v", err)
	}
	number, _ := arena.PutUTF8("-7f")
	_ = r.PutSpan(root, 0, number)
	numberView, _ := r.Span(root, 0)
	numberText, _ := numberView.UTF8()
	parsed, err := numberText.ParseInt(16, 32)
	if err != nil || parsed != -127 {
		t.Fatalf("parsed=%d err=%v", parsed, err)
	}
}
