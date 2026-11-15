package sandbox

import (
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
)

// availability caches the full host backend diagnostic. It reuses the cached
// nativeAvailable probe, so the check costs the same one-shot probe run, plus
// a platform-specific classification of any failure into actionable
// remediation guidance for clients.
var availability = sync.OnceValue(func() api.SandboxAvailability {
	err := nativeAvailable()
	avail := api.SandboxAvailability{
		Available: err == nil,
		Backend:   nativeBackendName(),
	}
	if err != nil {
		avail.Error = err.Error()
		avail.Remediation = nativeAvailabilityRemediation(err)
	}
	return avail
})

// Availability reports the health of this host's OS-native kernel sandbox
// backend: the backend name ("seatbelt" on macOS, "bubblewrap" on Linux, ""
// on unsupported platforms), whether the cached probe succeeded, the probe
// error detail, and actionable remediation guidance when the backend is
// unavailable (e.g. installing bubblewrap or lifting Ubuntu's AppArmor
// unprivileged-userns restriction).
func Availability() api.SandboxAvailability {
	return availability()
}
