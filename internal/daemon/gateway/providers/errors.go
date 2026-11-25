package providers

import (
	"errors"
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

// ErrInvalidConfig indicates that a provider rejected its configuration.
var ErrInvalidConfig = errors.New("gateway: invalid configuration")

func requireBrowserOAuth(expectedName, name string, credentials *auth.Config, hasOverrides bool) error {
	hasAuthOverride := credentials != nil && (credentials.Token != nil || credentials.Header != "" || credentials.Username != "" || credentials.Password != nil)
	if name != expectedName || credentials.Scheme() != auth.TypeOAuth2 || hasOverrides || hasAuthOverride {
		return fmt.Errorf("%w: %s requires the provider name %q and browser OAuth without endpoint, header, or config overrides", ErrInvalidConfig, expectedName, expectedName)
	}
	return nil
}
