package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
)

// resolve reads the credential from env (falling back to os.Getenv) or from File.
func (c *CredentialSource) resolve(env map[string]string) (string, error) {
	if c == nil {
		return "", nil
	}
	if key := strings.TrimSpace(c.Env); key != "" {
		val, ok := env[key]
		if !ok || strings.TrimSpace(val) == "" {
			val = os.Getenv(key)
		}
		if strings.TrimSpace(val) == "" {
			return "", fmt.Errorf("%w: missing required environment key %q", ErrMissingCredential, key)
		}
		return strings.TrimSpace(val), nil
	}
	if path := strings.TrimSpace(c.File); path != "" {
		data, err := os.ReadFile(fsutils.ResolveConfigPath(path, ""))
		if err != nil {
			return "", fmt.Errorf("%w: failed to read credential file %q: %w", ErrMissingCredential, path, err)
		}
		secret := strings.TrimSpace(string(data))
		if secret == "" {
			return "", fmt.Errorf("%w: credential file %q is empty", ErrMissingCredential, path)
		}
		return secret, nil
	}
	return "", nil
}

// resolveCredential extracts credentials from the injected in-memory env map,
// falling back to os.Getenv, or reading from an explicit file path. Providers
// declaring the "none" scheme never carry a credential.
func resolveCredential(auth *AuthConfig, env map[string]string) (string, error) {
	if auth == nil {
		return "", nil
	}

	scheme := auth.Type.normalized()
	if scheme == AuthTypeNone {
		return "", nil
	}

	if scheme == AuthTypeOAuth2 {
		if auth.Token != nil && !auth.Token.IsZero() {
			token, err := auth.Token.resolve(env)
			if err == nil {
				return token, nil
			}
		}
		return "", nil
	}

	if scheme == AuthTypeBasic && auth.Token.IsZero() && (auth.Username != "" || !auth.Password.IsZero()) {
		if auth.Password.IsZero() {
			return "", fmt.Errorf("%w: auth type %q requires a password or token source", ErrMissingCredential, scheme)
		}
		pass, err := auth.Password.resolve(env)
		if err != nil {
			return "", err
		}
		return auth.Username + ":" + pass, nil
	}

	if auth.Token.IsZero() {
		return "", fmt.Errorf("%w: auth type %q requires an environment variable or credential file", ErrMissingCredential, scheme)
	}
	return auth.Token.resolve(env)
}

// credentialDigest returns a short hex digest of a credential file's contents so
// configuration fingerprints change when a token is rotated, without ever
// placing the secret itself into the hashed payload. A missing or unreadable
// file digests its error text instead, so creating the file also invalidates
// any cached configuration.
func credentialDigest(path string) string {
	path = fsutils.ResolveConfigPath(path, "")
	data, err := os.ReadFile(path)
	if err != nil {
		return "unreadable:" + err.Error()
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// authHeaderName returns the header a credential is sent in, defaulting to
// Authorization.
func authHeaderName(auth *AuthConfig) string {
	if auth != nil && strings.TrimSpace(auth.Header) != "" {
		return strings.TrimSpace(auth.Header)
	}
	return "Authorization"
}

// scheme returns the configured auth scheme, defaulting to "bearer" when
// auth is nil or auth.type is omitted.
func (a *AuthConfig) scheme() AuthType {
	if a == nil {
		return AuthTypeBearer
	}
	return a.Type.normalized()
}

// placeholderSDKCredential is what a provider SDK receives when auth.type is
// "none" but the SDK refuses to build a client without any key at all. The
// transport deletes the header it produces, so it never leaves the process.
const placeholderSDKCredential = "auth-none"

// sdkAuthHeaders lists the authentication headers a provider SDK attaches on its
// own, before the gateway transport runs.
func sdkAuthHeaders(providerType string) []string {
	switch providerType {
	case "openai", "ollama", "typesafe", "antigravity":
		return []string{"Authorization"}
	case "anthropic":
		return []string{"X-Api-Key", "Authorization"}
	case "genai", "google-genai", "gemini":
		return []string{"x-goog-api-key", "Authorization"}
	default:
		return nil
	}
}

// redundantSDKAuthHeaders reports which SDK-native auth headers to strip from
// every request. Because configured auth and custom headers are placed into the
// transport header map, any SDK header not present in configured is dropped so
// conflicting headers do not both reach upstream. Managed OAuth2 providers with
// no custom header rely on the SDK's own token source and keep all SDK headers.
func redundantSDKAuthHeaders(auth *AuthConfig, providerType string, configured map[string]string) []string {
	if auth == nil {
		return nil
	}

	norm := auth.Type.normalized()
	customHeader := strings.TrimSpace(auth.Header) != ""
	if norm == AuthTypeOAuth2 && !customHeader {
		return nil
	}

	var drop []string
	for _, name := range sdkAuthHeaders(providerType) {
		if _, ok := configured[http.CanonicalHeaderKey(name)]; ok {
			continue
		}
		drop = append(drop, name)
	}
	return drop
}

// buildHTTPTransport creates an http.RoundTripper that attaches configured headers
// and authentication tokens to all outgoing requests. Configured headers win over
// any the provider SDK sets, since the transport is the last layer before the wire.
func buildHTTPTransport(base http.RoundTripper, auth *AuthConfig, token string, customHeaders map[string]string, providerType string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	headers := make(map[string]string)
	for k, v := range customHeaders {
		if key := http.CanonicalHeaderKey(strings.TrimSpace(k)); key != "" {
			headers[key] = v
		}
	}

	if token != "" && auth != nil {
		headerName := http.CanonicalHeaderKey(authHeaderName(auth))
		switch auth.Type.normalized() {
		case AuthTypeBearer, AuthTypeOAuth2:
			headers[headerName] = "Bearer " + token
		case AuthTypeAPIKey:
			headers[headerName] = token
		case AuthTypeBasic:
			headers[headerName] = "Basic " + base64.StdEncoding.EncodeToString([]byte(token))
		}
	}

	drop := redundantSDKAuthHeaders(auth, providerType, headers)

	if len(headers) == 0 && len(drop) == 0 {
		return base
	}

	return &headerTransport{
		base:    base,
		headers: headers,
		drop:    drop,
	}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
	drop    []string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqCopy := req.Clone(req.Context())
	for _, k := range t.drop {
		reqCopy.Header.Del(k)
	}
	for k, v := range t.headers {
		reqCopy.Header.Set(k, v)
	}
	return t.base.RoundTrip(reqCopy)
}
