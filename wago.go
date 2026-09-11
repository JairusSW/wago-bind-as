package bindas

import (
	"fmt"
	"math"

	wago "github.com/wago-org/wago"
)

// Bind resolves export once and binds it to memory zero using the checked
// (regionBase, regionLength, rootOffset) ABI.
func Bind(instance *wago.Instance, export string, start, length uint32, options ...ArenaOption) (*Prepared, error) {
	if instance == nil {
		return nil, fmt.Errorf("wago-bind-as: nil Wago instance")
	}
	function, err := instance.PrepareFunction(export)
	if err != nil {
		return nil, fmt.Errorf("wago-bind-as: prepare %q: %w", export, err)
	}
	memory := instance.Memory()
	if memory == nil {
		return nil, fmt.Errorf("wago-bind-as: instance has no memory zero")
	}
	return NewPrepared(memory, function, start, length, options...)
}

// BindPointer resolves a property-shaped @bind export and validates the root's
// generated size and alignment before every call.
func BindPointer(instance *wago.Instance, export string, start, length, rootSize, rootAlign uint32, options ...ArenaOption) (*Prepared, error) {
	if instance == nil {
		return nil, fmt.Errorf("wago-bind-as: nil Wago instance")
	}
	function, err := instance.PrepareFunction(export)
	if err != nil {
		return nil, fmt.Errorf("wago-bind-as: prepare %q: %w", export, err)
	}
	memory := instance.Memory()
	if memory == nil {
		return nil, fmt.Errorf("wago-bind-as: instance has no memory zero")
	}
	return NewPreparedPointer(memory, function, start, length, rootSize, rootAlign, options...)
}

// WithGuestRegion opens a callback-scoped Exact32 region through Wago's safe
// GuestStorage API. It may only be called with the HostModule handed to a live
// synchronous host import. Views and slices obtained by fn expire on return.
func WithGuestRegion(module wago.HostModule, memoryIndex uint32, offset, length uint64, writable bool, fn func(Region) error) error {
	if fn == nil {
		return fmt.Errorf("wago-bind-as: nil guest-region callback")
	}
	storageModule, ok := module.(wago.GuestStorageHostModule)
	if !ok {
		return fmt.Errorf("wago-bind-as: host module does not provide callback-scoped guest storage")
	}
	if length > math.MaxUint32 {
		return ErrBounds
	}
	return storageModule.WithGuestStorage(func(storage wago.GuestStorage) error {
		access := wago.GuestStorageRead
		if writable {
			access = wago.GuestStorageWrite
		}
		bytes, err := storage.MemoryRange(memoryIndex, offset, length, access)
		if err != nil {
			return err
		}
		arena, err := NewArena(bytes, 0, uint32(length), WithoutResetZeroing())
		if err != nil {
			return err
		}
		region := arena.Region()
		region.writable = writable
		defer arena.invalidate()
		return fn(region)
	})
}
