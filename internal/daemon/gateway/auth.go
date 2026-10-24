package gateway

import (
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
)

// resolveCredential strictly extracts credentials from the injected in-memory env map
// or an explicit file path. It NEVER inspects os.Getenv or the global process environment.
func resolveCredential(auth *AuthConfig, env map[string]string) (string, error) {
	if auth == nil {
		return "", nil
	}

	if auth.Env != "" {
		val, ok := env[auth.Env]
		if !ok || strings.TrimSpace(val) == "" {
			return "", fmt.Errorf("%w: missing required environment key %q", ErrMissingCredential, auth.Env)
		}
		return strings.TrimSpace(val), nil
	}

	if auth.File != "" {
		data, err := os.ReadFile(auth.File)
		if err != nil {
			return "", fmt.Errorf("%w: failed to read credential file %q: %w", ErrMissingCredential, auth.File, err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("%w: credential file %q is empty", ErrMissingCredential, auth.File)
		}
		return token, nil
	}

	return "", nil
}

// buildHTTPTransport creates an http.RoundTripper that attaches configured headers
// and authentication tokens to all outgoing requests.
func buildHTTPTransport(base http.RoundTripper, auth *AuthConfig, token string, customHeaders map[string]string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	headers := make(map[string]string)
	maps.Copy(headers, customHeaders)

	if token != "" && auth != nil {
		switch auth.Type {
		case AuthTypeBearer, "":
			headerName := auth.Header
			if headerName == "" {
				headerName = "Authorization"
			}
			headers[headerName] = "Bearer " + token
		case AuthTypeAPIKey:
			headerName := auth.Header
			if headerName == "" {
				headerName = "Authorization"
			}
			headers[headerName] = token
		case AuthTypeBasic:
			headerName := auth.Header
			if headerName == "" {
				headerName = "Authorization"
			}
			headers[headerName] = "Basic " + base64.StdEncoding.EncodeToString([]byte(token))
		}
	}

	if len(headers) == 0 {
		return base
	}

	return &headerTransport{
		base:    base,
		headers: headers,
	}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqCopy := req.Clone(req.Context())
	for k, v := range t.headers {
		if reqCopy.Header.Get(k) == "" {
			reqCopy.Header.Set(k, v)
		}
	}
	return t.base.RoundTrip(reqCopy)
}
