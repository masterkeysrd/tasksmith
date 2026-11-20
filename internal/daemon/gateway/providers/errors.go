package providers

import "errors"

// ErrInvalidConfig indicates that a provider rejected its configuration.
var ErrInvalidConfig = errors.New("gateway: invalid configuration")
