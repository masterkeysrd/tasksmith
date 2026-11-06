package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// Authentication scheme names accepted by spec.auth.type. They alias the wire
// values so the daemon, the API, and the CLI cannot drift apart on what a
// manifest may declare.
const (
	authTypeBearer           = api.MCPAuthBearer
	authTypeAPIKey           = api.MCPAuthAPIKey
	authTypeBasic            = api.MCPAuthBasic
	authTypeOAuth2           = api.MCPAuthOAuth2
	authTypeClientCredential = api.MCPAuthClientCredentials
	authTypeEnterprise       = api.MCPAuthEnterprise
)

// authorizationTimeout bounds how long an interactive browser authorization may
// stay pending before the flow is abandoned.
const authorizationTimeout = 5 * time.Minute

// ErrAuthRequired marks a failure whose cause is authentication rather than a
// broken server. Connection attempts wrap their credential-resolution and
// handshake failures with it so [Manager.failureStatus] can park the server in
// api.MCPStatusAuthRequired, which is what makes the CLI offer the user an
// actionable "authenticate this server" path instead of a generic error.
var ErrAuthRequired = errors.New("mcp authentication required")

// credentialsFileName is the file, under the application data directory, holding
// persisted MCP OAuth tokens and dynamically registered clients.
const credentialsFileName = "credentials.json"

// isOAuthScheme reports whether an authentication scheme needs the OAuth handler
// rather than a static header.
func isOAuthScheme(scheme string) bool {
	return api.MCPSchemeUsesOAuth(scheme)
}

// expandPlaceholders expands ${VAR} and $VAR references in s using env, consulting
// the host environment only for names that appear explicitly in s. Unreferenced
// host variables therefore never leak into a rendered value. A name that resolves
// nowhere is left in place as ${VAR}, so a missing credential surfaces as a
// visible misconfiguration instead of an empty header that fails with a confusing
// authentication error.
func expandPlaceholders(s string, env map[string]string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	var builder strings.Builder
	for index := 0; index < len(s); {
		if s[index] != '$' || index+1 >= len(s) {
			builder.WriteByte(s[index])
			index++
			continue
		}
		brace := s[index+1] == '{'
		start := index + 1
		if brace {
			start++
		}
		end := start
		for end < len(s) && isNameByte(s[end], brace) {
			end++
		}
		name := s[start:end]
		if name == "" {
			builder.WriteByte(s[index])
			index++
			continue
		}
		spanEnd := end
		if brace && end < len(s) {
			spanEnd = end + 1
		}
		if value, ok := env[name]; ok {
			builder.WriteString(value)
		} else if value, ok := os.LookupEnv(name); ok {
			builder.WriteString(value)
		} else {
			builder.WriteString(s[index:spanEnd])
		}
		index = spanEnd
	}
	return builder.String()
}

// isNameByte reports whether b continues a variable name. A braced reference allows
// any character up to its closing brace, matching shell behaviour for names like
// ${MY-VAR}.
func isNameByte(b byte, brace bool) bool {
	if brace {
		return b != '}'
	}
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// expandEnvMap expands every value in m.
func expandEnvMap(m map[string]string, env map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[expandPlaceholders(k, env)] = expandPlaceholders(v, env)
	}
	return out
}

// resolveCredential reads a credential from the workspace env map, then the host
// environment for the named variable, then an explicit file, mirroring how model
// provider credentials resolve.
func resolveCredential(a *warp.ProviderAuth, env map[string]string) (string, error) {
	if a == nil {
		return "", nil
	}
	if name := strings.TrimSpace(a.Env); name != "" {
		val := env[name]
		if strings.TrimSpace(val) == "" {
			val = os.Getenv(name)
		}
		if strings.TrimSpace(val) == "" {
			return "", requireAuth(fmt.Errorf("missing credential environment variable %q", name))
		}
		return strings.TrimSpace(val), nil
	}
	if file := strings.TrimSpace(a.File); file != "" {
		data, err := os.ReadFile(expandPlaceholders(file, env))
		if err != nil {
			return "", requireAuth(fmt.Errorf("read credential file %q: %w", file, err))
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", requireAuth(fmt.Errorf("credential file %q is empty", file))
		}
		return token, nil
	}
	return "", nil
}

// requireAuth marks err as an authentication failure so a failed connection
// reports api.MCPStatusAuthRequired instead of a generic error.
func requireAuth(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrAuthRequired, err)
}

// authStatusMarkers are failure texts that only a rejected or missing credential
// produces. The MCP SDK reports HTTP status rejections as text rather than typed
// errors, so the reason has to be recognised from the message.
var authStatusMarkers = []string{
	"401 unauthorized",
	"403 forbidden",
	"status code 401",
	"status code 403",
	"unauthorized",
	"forbidden",
	"invalid_token",
	"invalid client",
	"invalid_client",
	"invalid grant",
	"invalid_grant",
}

// authHandshakeMarkers identify failures of the OAuth handshake itself: metadata
// discovery, dynamic client registration, and token exchange. They are only
// consulted for servers that declare spec.auth, so an unreachable server stays a
// plain connection error rather than being mislabelled as needing a login.
var authHandshakeMarkers = []string{
	"failed to get auth handler",
	"oauth",
	"authorization server",
	"well-known",
	"client registration",
	"client id",
	"client secret",
	"authorization code",
	"authorization was rejected",
	"waiting for mcp authorization",
	"did not include an authorization code",
	"rejected a stored credential",
}

// isAuthError reports whether a connection failure means the server needs
// credentials the user has not supplied yet, either because the failure was
// raised by credential resolution ([ErrAuthRequired]) or because its text can
// only come from a rejected credential or a failed authorization handshake.
func isAuthError(def *warp.MCP, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthRequired) {
		return true
	}
	text := strings.ToLower(err.Error())
	if containsMarker(text, authStatusMarkers) {
		return true
	}
	if def == nil || def.Spec.Auth == nil {
		return false
	}
	return containsMarker(text, authHandshakeMarkers)
}

// containsMarker reports whether text includes any of markers.
func containsMarker(text string, markers []string) bool {
	return slices.ContainsFunc(markers, func(marker string) bool {
		return strings.Contains(text, marker)
	})
}

// credentialSource names, in user terms, where a manifest reads its credential.
func credentialSource(a *warp.ProviderAuth) string {
	if a == nil {
		return ""
	}
	if name := strings.TrimSpace(a.Env); name != "" {
		return "environment variable " + name
	}
	if file := strings.TrimSpace(a.File); file != "" {
		return "credential file " + file
	}
	return ""
}

// staticAuthInstruction explains how to give a non-interactive MCP server the
// credential it needs, so asking for an interactive login still produces useful
// advice. It returns false when the server has no authentication configured and
// no unresolved credential placeholder, where no credential could help.
func staticAuthInstruction(def *warp.MCP, env ...map[string]string) (string, bool) {
	if def == nil {
		return "", false
	}
	name := def.Metadata.Name
	if def.Spec.Auth == nil {
		var runtimeEnv map[string]string
		if len(env) > 0 {
			runtimeEnv = env[0]
		}
		if missing := unexpandedCredentialPlaceholders(def, runtimeEnv); len(missing) > 0 {
			varName := placeholderVarName(missing[0])
			return fmt.Sprintf("MCP server %q requires environment variable %s; set it and run /mcp restart %s", name, varName, name), true
		}
		return "", false
	}
	a := def.Spec.Auth
	scheme := strings.ToLower(strings.TrimSpace(a.Type))
	if scheme == "" {
		scheme = authTypeBearer
	}
	source := credentialSource(a)

	if scheme == authTypeEnterprise {
		return fmt.Sprintf("MCP server %q uses enterprise authorization, which TaskSmith cannot complete yet; switch spec.auth to oauth2 or client-credentials and run /mcp restart %s", name, name), true
	}
	if source == "" {
		if scheme == authTypeClientCredential {
			return fmt.Sprintf("MCP server %q authenticates with client credentials, but its manifest sets neither spec.auth.env (client ID) nor spec.auth.file (client secret); configure them and run /mcp restart %s", name, name), true
		}
		return fmt.Sprintf("MCP server %q authenticates via %s, but its manifest sets neither spec.auth.env nor spec.auth.file; configure a credential and run /mcp restart %s", name, scheme, name), true
	}
	if scheme == authTypeClientCredential {
		return fmt.Sprintf("MCP server %q authenticates with client credentials from the %s; provide it and run /mcp restart %s", name, source, name), true
	}
	return fmt.Sprintf("MCP server %q authenticates via %s (%s); set it and run /mcp restart %s", name, source, scheme, name), true
}

// unexpandedPlaceholderPattern matches a placeholder that survived expansion,
// which means the variable it names resolved nowhere.
var unexpandedPlaceholderPattern = regexp.MustCompile(`\$\{[^}]+\}|\$[A-Za-z_][A-Za-z0-9_]*`)

// placeholderVarName strips `$` and `${...}` from a placeholder token so error
// messages and instructions can name the environment variable directly.
func placeholderVarName(placeholder string) string {
	trimmed := strings.TrimPrefix(placeholder, "$")
	trimmed = strings.TrimPrefix(trimmed, "{")
	trimmed = strings.TrimSuffix(trimmed, "}")
	if name, _, ok := strings.Cut(trimmed, ":-"); ok {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(trimmed)
}

// unexpandedPlaceholders returns the placeholder references still present in the
// given values, in the order they appear and without duplicates.
func unexpandedPlaceholders(values ...string) []string {
	var found []string
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, match := range unexpandedPlaceholderPattern.FindAllString(value, -1) {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			found = append(found, match)
		}
	}
	return found
}

// unexpandedCredentialPlaceholders returns any unresolved `${VAR}` or `$VAR`
// references in def.Spec.Headers or def.Spec.Env after expanding against env.
func unexpandedCredentialPlaceholders(def *warp.MCP, env map[string]string) []string {
	if def == nil {
		return nil
	}
	values := make([]string, 0, 2*(len(def.Spec.Headers)+len(def.Spec.Env)))
	for _, name := range slices.Sorted(maps.Keys(def.Spec.Headers)) {
		values = append(values, name, def.Spec.Headers[name])
	}
	for _, name := range slices.Sorted(maps.Keys(def.Spec.Env)) {
		values = append(values, name, def.Spec.Env[name])
	}
	if len(values) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, expandPlaceholders(value, env))
	}
	return unexpandedPlaceholders(rendered...)
}

// unexpandedCredentialFailure reports whether a failed connection was rendered
// from placeholders that never resolved. A surviving `${VAR}` in an
// authentication header or subprocess environment means the variable is unset, so
// the server could not have authenticated even if it were reachable, and the
// failure is reported as needing authentication rather than as a broken server.
func unexpandedCredentialFailure(def *warp.MCP, env map[string]string) bool {
	return len(unexpandedCredentialPlaceholders(def, env)) > 0
}

// staticAuthHeaders renders the headers a static authentication scheme sends on
// every request. Custom spec.headers are applied first so authentication cannot
// silently overwrite an explicitly configured header.
func staticAuthHeaders(def *warp.MCP, env map[string]string) (map[string]string, error) {
	headers := expandEnvMap(def.Spec.Headers, env)
	a := def.Spec.Auth
	if a == nil || isOAuthScheme(a.Type) {
		return headers, nil
	}
	token, err := resolveCredential(a, env)
	if err != nil {
		return nil, fmt.Errorf("MCP server %q: %w", def.Metadata.Name, err)
	}
	if strings.TrimSpace(token) == "" {
		return headers, nil
	}
	if headers == nil {
		headers = make(map[string]string, 1)
	}
	name := strings.TrimSpace(a.Header)
	if name == "" {
		name = "Authorization"
	}
	switch strings.ToLower(strings.TrimSpace(a.Type)) {
	case authTypeBearer, "":
		headers[name] = "Bearer " + token
	case authTypeAPIKey:
		headers[name] = token
	case authTypeBasic:
		headers[name] = "Basic " + base64.StdEncoding.EncodeToString([]byte(token))
	default:
		return nil, fmt.Errorf("MCP server %q: unsupported auth type %q", def.Metadata.Name, a.Type)
	}
	return headers, nil
}

// storedServer holds everything persisted for one MCP server so a daemon restart
// reuses an acquired token and a dynamically registered client instead of forcing
// a browser re-login.
type storedServer struct {
	Token        *oauth2.Token `json:"token,omitempty"`
	ClientID     string        `json:"clientId,omitempty"`
	ClientSecret string        `json:"clientSecret,omitempty"`
	Issuer       string        `json:"issuer,omitempty"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// CredentialStore persists MCP OAuth tokens and client credentials in the
// application data directory with owner-only permissions.
type CredentialStore struct {
	path string

	mu    sync.Mutex
	cache map[string]storedServer
}

// NewCredentialStore returns a store backed by
// $XDG_DATA_HOME/tasksmith/mcp/credentials.json.
func NewCredentialStore() (*CredentialStore, error) {
	dir, err := xdg.SubDataDir("mcp")
	if err != nil {
		return nil, fmt.Errorf("resolve MCP credential directory: %w", err)
	}
	return &CredentialStore{path: filepath.Join(dir, credentialsFileName)}, nil
}

// NewCredentialStoreAt returns a store backed by an explicit file, used by tests.
func NewCredentialStoreAt(path string) *CredentialStore {
	return &CredentialStore{path: path}
}

// Path returns the file backing the store.
func (s *CredentialStore) Path() string { return s.path }

// serverKey identifies one server by name and endpoint, so a renamed endpoint
// never reuses another server's token.
func serverKey(name, endpoint string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name) + "\x00" + strings.TrimSpace(endpoint)))
	return hex.EncodeToString(sum[:])
}

// load reads the store file into memory, tolerating a missing or unreadable file
// so a corrupt credential file degrades to "not signed in" rather than breaking
// every server.
func (s *CredentialStore) loadLocked() map[string]storedServer {
	if s.cache != nil {
		return s.cache
	}
	s.cache = make(map[string]storedServer)
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s.cache
	}
	var onDisk map[string]storedServer
	if err := json.Unmarshal(data, &onDisk); err != nil {
		return s.cache
	}
	maps.Copy(s.cache, onDisk)
	return s.cache
}

// Get returns the persisted credentials for key.
func (s *CredentialStore) Get(key string) (storedServer, bool) {
	if s == nil {
		return storedServer{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.loadLocked()[key]
	if !ok {
		return storedServer{}, false
	}
	return entry, true
}

// Put persists credentials for key, writing the whole store atomically with
// owner-only permissions.
func (s *CredentialStore) Put(key string, entry storedServer) error {
	if s == nil {
		return errors.New("credential store is nil")
	}
	entry.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.loadLocked()
	entries[key] = entry

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create MCP credential directory: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, credentialsFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create MCP credential temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restrict MCP credential file: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write MCP credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write MCP credentials: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace MCP credentials: %w", err)
	}
	return nil
}

// Forget removes persisted credentials for key.
func (s *CredentialStore) Forget(key string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.loadLocked()
	if _, ok := entries[key]; !ok {
		return nil
	}
	delete(entries, key)
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP credentials: %w", err)
	}
	if err := os.WriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write MCP credentials: %w", err)
	}
	return nil
}

// authEvents receives the URL the user must open and reports completion.
type authEvents struct {
	// onAuthorize is called with the authorization URL as soon as it is known.
	onAuthorize func(url string)
	// onToken is called when an access token has been acquired.
	onToken func(*oauth2.Token)
}

// oauthProvider implements the MCP client's auth provider interface for one
// server, persisting acquired tokens and dynamically registered clients.
type oauthProvider struct {
	name        string
	key         string
	scheme      string
	endpoint    string
	auth        *warp.ProviderAuth
	env         map[string]string
	store       *CredentialStore
	interactive bool
	events      authEvents

	mu       sync.Mutex
	lastAuth string
	// receiver is the callback listener of the interactive flow currently in
	// flight, if any. It is what lets a URL pasted into the terminal reach the
	// waiting flow when the browser could not reach the loopback port.
	receiver *callbackReceiver
}

// GetHandler builds the OAuth handler the MCP transport uses. Pre-registered
// client credentials, when configured, resolve from spec.auth the same way static
// credentials do: spec.auth.env names the variable holding the client ID and
// spec.auth.file names a file holding the client secret. Otherwise credentials
// previously obtained through dynamic registration are reused, and a fresh
// registration happens when none exist.
func (p *oauthProvider) GetHandler(ctx context.Context) (auth.OAuthHandler, error) {
	if p.store != nil {
		if entry, ok := p.store.Get(p.key); ok {
			if tok := storedToken(entry.Token); tok != nil {
				return &staticTokenHandler{
					token: tok,
					clearToken: func() {
						entry.Token = nil
						_ = p.store.Put(p.key, entry)
					},
				}, nil
			}
		}
	}

	switch p.scheme {
	case authTypeClientCredential:
		return p.clientCredentialsHandler()
	case authTypeEnterprise:
		return nil, requireAuth(errors.New("MCP enterprise authorization requires an identity provider issuer, which spec.auth cannot express yet; use oauth2 or client-credentials"))
	default:
		if !p.interactive {
			if _, _, err := p.preregisteredCredentials(); err != nil {
				return nil, err
			}
			return &nonInteractiveOAuthHandler{name: p.name}, nil
		}
		return p.authorizationCodeHandler(ctx)
	}
}

// storedToken normalizes a persisted token so expiry is evaluated against the
// current clock.
func storedToken(tok *oauth2.Token) *oauth2.Token {
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil
	}
	return tok
}

// clientCredentialsHandler builds a machine-to-machine handler from resolved
// client credentials.
func (p *oauthProvider) clientCredentialsHandler() (auth.OAuthHandler, error) {
	clientID, clientSecret, err := p.preregisteredCredentials()
	if err != nil {
		return nil, err
	}
	if clientID == "" || clientSecret == "" {
		return nil, requireAuth(errors.New("MCP client-credentials authorization needs a client ID and secret (spec.auth.env and spec.auth.file)"))
	}
	return extauth.NewClientCredentialsHandler(&extauth.ClientCredentialsHandlerConfig{
		Credentials: &oauthex.ClientCredentials{
			ClientID:         clientID,
			ClientSecretAuth: &oauthex.ClientSecretAuth{ClientSecret: clientSecret},
		},
	})
}

// preregisteredCredentials resolves pre-registered client credentials from
// spec.auth, returning empty strings when unconfigured.
func (p *oauthProvider) preregisteredCredentials() (clientID, clientSecret string, err error) {
	if p.auth == nil {
		return "", "", nil
	}
	if name := strings.TrimSpace(p.auth.Env); name != "" {
		clientID = strings.TrimSpace(p.env[name])
		if clientID == "" {
			clientID = strings.TrimSpace(os.Getenv(name))
		}
		if clientID == "" {
			return "", "", requireAuth(fmt.Errorf("missing MCP client ID environment variable %q", name))
		}
	}
	if file := strings.TrimSpace(p.auth.File); file != "" {
		data, readErr := os.ReadFile(expandPlaceholders(file, p.env))
		if readErr != nil {
			return "", "", requireAuth(fmt.Errorf("read MCP client secret file %q: %w", file, readErr))
		}
		clientSecret = strings.TrimSpace(string(data))
	}
	return clientID, clientSecret, nil
}

// authorizationCodeHandler builds the Authorization Code + PKCE handler, wiring
// the local callback listener, token persistence, and client-credential capture.
func (p *oauthProvider) authorizationCodeHandler(_ context.Context) (auth.OAuthHandler, error) {
	clientID, clientSecret, err := p.preregisteredCredentials()
	if err != nil {
		return nil, err
	}

	receiver := newCallbackReceiver(p.name)
	p.setReceiver(receiver)
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              receiver.redirectURL(),
		AuthorizationCodeFetcher: p.fetchCode(receiver),
		RequestRefreshToken:      true,
		Client:                   &http.Client{Transport: &registrationRecorder{store: p.store, key: p.key}},
		NewTokenSource: func(ctx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			p.persistToken(tok)
			if p.events.onToken != nil {
				p.events.onToken(tok)
			}
			return oc.TokenSource(ctx, tok), nil
		},
	}

	if clientID != "" {
		prereg := &oauthex.ClientCredentials{ClientID: clientID}
		if clientSecret != "" {
			prereg.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: clientSecret}
		}
		cfg.PreregisteredClient = prereg
	} else if p.store != nil {
		if entry, ok := p.store.Get(p.key); ok && entry.ClientID != "" {
			prereg := &oauthex.ClientCredentials{ClientID: entry.ClientID, Issuer: entry.Issuer}
			if entry.ClientSecret != "" {
				prereg.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: entry.ClientSecret}
			}
			cfg.PreregisteredClient = prereg
		}
	}
	if cfg.PreregisteredClient == nil {
		cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   fmt.Sprintf("TaskSmith (%s)", p.name),
				RedirectURIs: []string{receiver.redirectURL()},
				GrantTypes:   []string{"authorization_code", "refresh_token"},
			},
		}
	}

	handler, err := auth.NewAuthorizationCodeHandler(cfg)
	if err != nil {
		receiver.close()
		p.clearReceiver(receiver)
		return nil, requireAuth(fmt.Errorf("prepare MCP authorization flow: %w", err))
	}
	return &trackingHandler{
		inner:    handler,
		receiver: receiver,
		onURL:    p.recordAuthorize,
		onDone:   func() { p.clearReceiver(receiver) },
	}, nil
}

// setReceiver marks a callback listener as the flow currently in flight.
func (p *oauthProvider) setReceiver(receiver *callbackReceiver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.receiver = receiver
}

// clearReceiver forgets a finished flow, but only if it is still the active one,
// so a slow flow can never discard a newer one that replaced it.
func (p *oauthProvider) clearReceiver(receiver *callbackReceiver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.receiver == receiver {
		p.receiver = nil
	}
}

// submitCallbackURL feeds a manually pasted redirect URL to the flow in flight.
func (p *oauthProvider) submitCallbackURL(raw string) error {
	p.mu.Lock()
	receiver := p.receiver
	name := p.name
	p.mu.Unlock()
	if receiver == nil {
		return fmt.Errorf("no authorization is in progress for %s; start one with /mcp auth %s", name, name)
	}
	return receiver.SubmitCallbackURL(raw)
}

// recordAuthorize publishes a pending authorization URL.
func (p *oauthProvider) recordAuthorize(authorizeURL string) {
	p.mu.Lock()
	p.lastAuth = authorizeURL
	p.mu.Unlock()
	if p.events.onAuthorize != nil {
		p.events.onAuthorize(authorizeURL)
	}
}

// pendingAuthURL returns the authorization URL of an in-flight or last failed
// interactive flow.
func (p *oauthProvider) pendingAuthURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastAuth
}

// persistToken writes an acquired token to the credential store.
func (p *oauthProvider) persistToken(tok *oauth2.Token) {
	if p.store == nil || tok == nil {
		return
	}
	entry := storedServer{Token: tok}
	if existing, ok := p.store.Get(p.key); ok {
		entry.ClientID = existing.ClientID
		entry.ClientSecret = existing.ClientSecret
		entry.Issuer = existing.Issuer
	}
	if err := p.store.Put(p.key, entry); err != nil {
		return
	}
}

// fetchCode returns the fetcher that drives the browser flow. It publishes the
// authorization URL, waits for the local callback, and never blocks longer than
// the flow timeout.
func (p *oauthProvider) fetchCode(receiver *callbackReceiver) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if err := receiver.start(); err != nil {
			return nil, err
		}
		// The listener binds to an ephemeral port, so the redirect URL is only
		// final after start; the authorization URL embeds it, so publish after.
		p.recordAuthorize(args.URL)

		code, state, iss, err := receiver.wait(ctx, authorizationTimeout)
		if err != nil {
			return nil, err
		}
		return &auth.AuthorizationResult{Code: code, State: state, Iss: iss}, nil
	}
}

// callbackReceiver listens on a loopback port for the OAuth redirect. The port is
// ephemeral so concurrent authorizations never collide, and the redirect URL is
// reported only after the listener is bound.
type callbackReceiver struct {
	mu         sync.Mutex
	serverName string
	listener   net.Listener
	server     *http.Server
	codes      chan callbackResult
	url        string
	started    bool
	closed     bool
}

type callbackResult struct {
	code  string
	state string
	iss   string
	err   error
}

func newCallbackReceiver(serverName string) *callbackReceiver {
	return &callbackReceiver{serverName: serverName, codes: make(chan callbackResult, 1)}
}

// start binds the listener. It is safe to call more than once.
func (r *callbackReceiver) start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for MCP OAuth callback: %w", err)
	}
	r.listener = listener
	addr := listener.Addr().String()
	r.url = "http://" + addr + "/callback"

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		if failure := query.Get("error"); failure != "" {
			select {
			case r.codes <- callbackResult{err: requireAuth(fmt.Errorf("authorization was rejected: %s", failure))}:
			default:
			}
			writeCallbackPage(w, http.StatusBadRequest, callbackErrorHTML(r.name(), failure, query.Get("error_description")))
			return
		}
		select {
		case r.codes <- callbackResult{code: query.Get("code"), state: query.Get("state"), iss: query.Get("iss")}:
		default:
		}
		writeCallbackPage(w, http.StatusOK, callbackSuccessHTML(r.name()))
	})

	r.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	r.started = true
	go func() {
		if serveErr := r.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case r.codes <- callbackResult{err: serveErr}:
			default:
			}
		}
	}()
	return nil
}

// name returns the server name the receiver shows on its branded pages.
func (r *callbackReceiver) name() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.serverName
}

// redirectURL returns the callback URL, binding the listener if needed.
func (r *callbackReceiver) redirectURL() string {
	if err := r.start(); err != nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.url
}

// wait blocks for the redirect, honoring cancellation and the flow timeout.
func (r *callbackReceiver) wait(ctx context.Context, timeout time.Duration) (code, state, iss string, err error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-r.codes:
		r.close()
		if res.err != nil {
			return "", "", "", res.err
		}
		if strings.TrimSpace(res.code) == "" {
			return "", "", "", requireAuth(errors.New("MCP OAuth callback did not include an authorization code"))
		}
		return res.code, res.state, res.iss, nil
	case <-ctx.Done():
		r.close()
		return "", "", "", ctx.Err()
	case <-timer.C:
		r.close()
		return "", "", "", requireAuth(fmt.Errorf("timed out waiting for MCP authorization after %s", timeout))
	}
}

// SubmitCallbackURL completes a waiting flow with a redirect URL the user pasted
// by hand. It accepts the full URL from the browser's address bar, a bare query
// string, or a bare authorization code, because a browser that cannot reach the
// daemon's loopback port still shows all three in readable form.
func (r *callbackReceiver) SubmitCallbackURL(raw string) error {
	result, err := parseCallbackSubmission(raw)
	if err != nil {
		return err
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return fmt.Errorf("MCP authorization for %s is no longer waiting for a callback", r.name())
	}
	select {
	case r.codes <- result:
		return nil
	default:
		return fmt.Errorf("MCP authorization for %s already received a callback", r.name())
	}
}

// parseCallbackSubmission decodes whatever the user pasted into a callback result.
func parseCallbackSubmission(raw string) (callbackResult, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return callbackResult{}, errors.New("paste the redirect URL shown by your browser, or the authorization code alone")
	}

	query := ""
	switch idx, hash := strings.Index(value, "?"), strings.Index(value, "#"); {
	case idx >= 0:
		query = value[idx+1:]
	case hash >= 0 && strings.Contains(value[hash+1:], "="):
		// A response that carries its parameters in the fragment still names the
		// same code, so read them from there rather than rejecting the paste.
		query = value[hash+1:]
	case strings.HasPrefix(value, "code=") || strings.HasPrefix(value, "state=") || strings.HasPrefix(value, "error="):
		query = value
	default:
		// Anything else is read as the authorization code itself, which is what an
		// identity provider that shows the code on its own confirmation page yields.
		// A value that still looks like a URL is not a code, so say what went wrong
		// instead of handing the identity provider a nonsense credential.
		if strings.Contains(value, "://") {
			return callbackResult{}, errors.New("the pasted URL carries no authorization code; copy the full address from your browser's address bar after authorizing")
		}
		return callbackResult{code: value}, nil
	}

	params, err := url.ParseQuery(query)
	if err != nil {
		return callbackResult{}, fmt.Errorf("could not read the pasted callback: %w", err)
	}
	if failure := params.Get("error"); failure != "" {
		return callbackResult{err: requireAuth(fmt.Errorf("authorization was rejected: %s", failure))}, nil
	}
	code := params.Get("code")
	if strings.TrimSpace(code) == "" {
		return callbackResult{}, errors.New("the pasted value carries no authorization code; copy the full URL from your browser's address bar")
	}
	return callbackResult{code: code, state: params.Get("state"), iss: params.Get("iss")}, nil
}

// close stops the listener.
func (r *callbackReceiver) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.server.Shutdown(shutdownCtx)
		_ = r.server.Close()
	}
	if r.listener != nil {
		_ = r.listener.Close()
	}
}

// trackingHandler records the authorization URL the inner handler is about to
// open and releases the callback listener once the flow finishes.
type trackingHandler struct {
	inner    auth.OAuthHandler
	receiver *callbackReceiver
	onURL    func(string)
	onDone   func()
}

func (t *trackingHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return t.inner.TokenSource(ctx)
}

func (t *trackingHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	err := t.inner.Authorize(ctx, req, resp)
	if t.receiver != nil {
		t.receiver.close()
	}
	if t.onDone != nil {
		t.onDone()
	}
	if err != nil && strings.Contains(err.Error(), "no configured client registration methods are supported") {
		return requireAuth(errors.New("server does not support OAuth dynamic client registration; set spec.auth.env to a pre-registered OAuth client ID or use spec.auth.type: bearer"))
	}
	return err
}

// nonInteractiveOAuthHandler is used during background Sync/Restart when an
// oauth2 server has no stored token yet, so background connection attempts fail
// fast into auth_required instead of blocking on a local callback listener when
// no browser was opened.
type nonInteractiveOAuthHandler struct {
	name string
}

func (h *nonInteractiveOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return nil, nil
}

func (h *nonInteractiveOAuthHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}
	return requireAuth(fmt.Errorf("interactive OAuth authorization required (run /mcp auth %s)", h.name))
}

// staticTokenHandler authenticates requests with an already-acquired token.
type staticTokenHandler struct {
	token      *oauth2.Token
	clearToken func()
}

func (h *staticTokenHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return oauth2.StaticTokenSource(h.token), nil
}

func (h *staticTokenHandler) Authorize(_ context.Context, req *http.Request, resp *http.Response) error {
	if h.clearToken != nil {
		h.clearToken()
	}
	if resp != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}
	if req != nil && req.URL != nil {
		return requireAuth(fmt.Errorf("MCP server %s rejected a stored credential; run authentication again", req.URL.Host))
	}
	return requireAuth(errors.New("MCP server rejected a stored credential; run authentication again"))
}

// registrationRecorder observes a dynamic client registration response so the
// issued client credentials can be reused after a restart.
type registrationRecorder struct {
	store *CredentialStore
	key   string
	base  http.RoundTripper
}

func (t *registrationRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || t.store == nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(req.URL.Path, "register") {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr == nil {
			var registration struct {
				ClientID     string `json:"client_id"`
				ClientSecret string `json:"client_secret"`
			}
			if json.Unmarshal(body, &registration) == nil && registration.ClientID != "" {
				entry, _ := t.store.Get(t.key)
				entry.ClientID = registration.ClientID
				if registration.ClientSecret != "" {
					entry.ClientSecret = registration.ClientSecret
				}
				if issuer := registrationIssuer(req); issuer != "" {
					entry.Issuer = issuer
				}
				_ = t.store.Put(t.key, entry)
			}
			resp.Body = io.NopCloser(strings.NewReader(string(body)))
			return resp, nil
		}
	}
	return resp, nil
}

// registrationIssuer derives the authorization server issuer from a registration
// request URL, which is the registration endpoint minus its well-known prefix.
func registrationIssuer(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	u := *req.URL
	u.Path = strings.TrimSuffix(u.Path, "/register")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/")
}
