//go:build !darwin && !linux

package sandbox

import (
	"fmt"
	"runtime"
)

// nativeBackendName is empty on platforms without a kernel sandbox backend.
func nativeBackendName() string { return "" }

// nativeAvailabilityRemediation returns no guidance on unsupported platforms:
// there is no kernel backend to fix, so sessions run with local isolation.
func nativeAvailabilityRemediation(_ error) string { return "" }

// checkNativeAvailable always returns ErrNativeUnavailable on unsupported platforms.
func checkNativeAvailable() error {
	return fmt.Errorf("%w: native sandbox mode is not supported on OS %q", ErrNativeUnavailable, runtime.GOOS)
}

// wrapNativeCommand always returns ErrNativeUnavailable on unsupported platforms.
func wrapNativeCommand(_ *session, _, _ string, _ []string, _ bool) (string, []string, error) {
	return "", nil, fmt.Errorf("%w: native sandbox mode is not supported on OS %q", ErrNativeUnavailable, runtime.GOOS)
}
