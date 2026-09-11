package bindas

import "errors"

var (
	ErrBounds          = errors.New("wago-bind-as: region bounds exceeded")
	ErrInvalidAlign    = errors.New("wago-bind-as: alignment must be a power of two")
	ErrInvalidUTF8     = errors.New("wago-bind-as: invalid UTF-8")
	ErrMalformed       = errors.New("wago-bind-as: malformed descriptor")
	ErrOutOfSpace      = errors.New("wago-bind-as: region is out of space")
	ErrReadOnly        = errors.New("wago-bind-as: region is read-only")
	ErrStale           = errors.New("wago-bind-as: region view is stale")
	ErrSchema          = errors.New("wago-bind-as: schema mismatch")
	ErrValidationLimit = errors.New("wago-bind-as: validation work limit exceeded")
)
