package bindas

import (
	"encoding/binary"
	"fmt"
)

const (
	EnvelopeSize  = 32
	FormatVersion = 1
)

var envelopeMagic = [4]byte{'W', 'B', 'A', 'S'}

type Fingerprint [16]byte

type Envelope struct {
	Flags       uint16
	Extent      uint32
	Root        Offset
	Fingerprint Fingerprint
}

func WriteEnvelope(dst []byte, e Envelope) error {
	if len(dst) < EnvelopeSize {
		return ErrBounds
	}
	if uint64(e.Root) >= uint64(e.Extent) && e.Extent != 0 {
		return ErrMalformed
	}
	copy(dst[:4], envelopeMagic[:])
	binary.LittleEndian.PutUint16(dst[4:6], FormatVersion)
	binary.LittleEndian.PutUint16(dst[6:8], e.Flags)
	binary.LittleEndian.PutUint32(dst[8:12], e.Extent)
	binary.LittleEndian.PutUint32(dst[12:16], uint32(e.Root))
	copy(dst[16:32], e.Fingerprint[:])
	return nil
}

func ReadEnvelope(src []byte, expected Fingerprint) (Envelope, error) {
	if len(src) < EnvelopeSize {
		return Envelope{}, ErrBounds
	}
	if string(src[:4]) != string(envelopeMagic[:]) {
		return Envelope{}, fmt.Errorf("%w: bad magic", ErrMalformed)
	}
	if version := binary.LittleEndian.Uint16(src[4:6]); version != FormatVersion {
		return Envelope{}, fmt.Errorf("%w: format version %d", ErrSchema, version)
	}
	e := Envelope{Flags: binary.LittleEndian.Uint16(src[6:8]), Extent: binary.LittleEndian.Uint32(src[8:12]), Root: Offset(binary.LittleEndian.Uint32(src[12:16]))}
	copy(e.Fingerprint[:], src[16:32])
	if expected != (Fingerprint{}) && e.Fingerprint != expected {
		return Envelope{}, ErrSchema
	}
	if e.Extent > uint32(len(src)) || (e.Extent != 0 && uint32(e.Root) >= e.Extent) {
		return Envelope{}, ErrMalformed
	}
	return e, nil
}
