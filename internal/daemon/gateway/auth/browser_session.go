package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"golang.org/x/oauth2"
)

type browserRegistration[C any] struct {
	Revision string `json:"revision"`
	Account  C      `json:"account"`
}

// BrowserSessionOption configures optional behaviors on [BrowserSessionStore].
type BrowserSessionOption[C any] func(*BrowserSessionStore[C])

// WithBrowserSessionExpire supplies a callback that mutates a copy of stored
// credentials so newSource treats the access token as expired and performs a
// refresh_token grant on the next Token call.
func WithBrowserSessionExpire[C any](fn func(*C)) BrowserSessionOption[C] {
	return func(s *BrowserSessionStore[C]) {
		s.expire = fn
	}
}

// WithBrowserSessionExpiredCheck supplies a predicate reporting whether stored
// credentials on disk are statically expired with no refresh path.
func WithBrowserSessionExpiredCheck[C any](fn func(C) bool) BrowserSessionOption[C] {
	return func(s *BrowserSessionStore[C]) {
		s.isStaticExpired = fn
	}
}

// BrowserSessionStore keeps one rotating source per active registration. Its
// lock serializes token refresh with sign-in and sign-out.
type BrowserSessionStore[C any] struct {
	provider        string
	accountName     func(C) string
	newSource       func(context.Context, *C, func(C) error) (oauth2.TokenSource, error)
	expire          func(*C)
	isStaticExpired func(C) bool

	mu     sync.Mutex
	active *browserSession[C]
}

// NewBrowserSessionStore manages registrations for a provider. The token source
// must save refreshed credentials synchronously during Token.
func NewBrowserSessionStore[C any](
	provider string,
	accountName func(C) string,
	newSource func(context.Context, *C, func(C) error) (oauth2.TokenSource, error),
	opts ...BrowserSessionOption[C],
) *BrowserSessionStore[C] {
	s := &BrowserSessionStore[C]{
		provider:    provider,
		accountName: accountName,
		newSource:   newSource,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *BrowserSessionStore[C]) path() (string, error) {
	return xdg.SubDataDir("credentials", s.provider, "registration.json")
}

func (s *BrowserSessionStore[C]) load() (string, browserRegistration[C], error) {
	var record browserRegistration[C]
	path, err := s.path()
	if err != nil {
		return "", record, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return path, record, fmt.Errorf("decode browser registration for %s: %w", s.provider, err)
	}
	if record.Revision == "" {
		return path, record, fmt.Errorf("browser registration for %s has no revision", s.provider)
	}
	return path, record, nil
}

// LoadAccount returns the stored credentials for the active registration.
func (s *BrowserSessionStore[C]) LoadAccount() (C, error) {
	_, record, err := s.load()
	return record.Account, err
}

// Save replaces the active registration and revokes its previous token source.
func (s *BrowserSessionStore[C]) Save(account C) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create browser credential directory: %w", err)
	}
	record := browserRegistration[C]{Revision: uuid.NewString(), Account: account}
	if err := writeBrowserRegistration(path, record); err != nil {
		return err
	}
	s.active = nil
	return nil
}

func writeBrowserRegistration[C any](path string, record browserRegistration[C]) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode browser registration: %w", err)
	}
	if err := fsutils.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("save browser registration: %w", err)
	}
	return nil
}

// Account returns the name of the active account and whether it exists.
func (s *BrowserSessionStore[C]) Account() (string, bool) {
	_, record, err := s.load()
	if err != nil {
		return "", false
	}
	return s.accountName(record.Account), true
}

// CredentialRevision changes when the active registration changes.
func (s *BrowserSessionStore[C]) CredentialRevision() string {
	_, record, err := s.load()
	if err != nil {
		return ""
	}
	return record.Revision
}

// Logout removes the active registration and revokes its token source.
func (s *BrowserSessionStore[C]) Logout() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path()
	if err != nil {
		return false, err
	}
	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove browser registration for %s: %w", s.provider, err)
	}
	s.active = nil
	return err == nil, nil
}

// TokenSource returns the shared source for the active registration.
func (s *BrowserSessionStore[C]) TokenSource() (oauth2.TokenSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, record, err := s.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil, s.signInRequired()
	}
	if err != nil {
		return nil, err
	}
	if active := s.active; active != nil && active.path == path && active.revision == record.Revision {
		return active, nil
	}

	// Refresh is shared, so its HTTP deadline is independent of any inference
	// request or gateway lifetime.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, browserRefreshHTTPClient())
	source, err := s.newSource(ctx, &record.Account, func(account C) error {
		record.Account = account
		return writeBrowserRegistration(path, record)
	})
	if err != nil {
		return nil, fmt.Errorf("load browser registration for %s: %w", s.provider, err)
	}
	s.active = &browserSession[C]{store: s, path: path, revision: record.Revision, source: source}
	return s.active, nil
}

// IsExpired reports whether stored credentials on disk are statically expired
// with no refresh path.
func (s *BrowserSessionStore[C]) IsExpired() bool {
	if s.isStaticExpired == nil {
		return false
	}
	_, record, err := s.load()
	if err != nil {
		return false
	}
	return s.isStaticExpired(record.Account)
}

// ForceRefresh exchanges the stored refresh token for a fresh access token
// after an upstream 401 response. Concurrent callers presenting the same failed
// access token share a single refresh and reuse the rotated token.
func (s *BrowserSessionStore[C]) ForceRefresh(ctx context.Context, failedAccessToken string) (*oauth2.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path, record, err := s.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil, s.signInRequired()
	}
	if err != nil {
		return nil, err
	}

	if active := s.active; active != nil &&
		active.path == path &&
		active.revision == record.Revision &&
		active.lastAccessToken != "" &&
		failedAccessToken != "" &&
		active.lastAccessToken != failedAccessToken {
		tok, err := active.source.Token()
		if err == nil && tok != nil {
			active.lastAccessToken = tok.AccessToken
		}
		return tok, err
	}

	if s.expire == nil {
		return nil, loomauth.ErrAuthRequired
	}

	expired := record.Account
	s.expire(&expired)

	refreshCtx := context.WithValue(context.Background(), oauth2.HTTPClient, browserRefreshHTTPClient())
	source, err := s.newSource(refreshCtx, &expired, func(account C) error {
		record.Account = account
		return writeBrowserRegistration(path, record)
	})
	if err != nil {
		return nil, err
	}

	tok, err := source.Token()
	if err != nil {
		return nil, err
	}

	if s.active != nil && s.active.path == path && s.active.revision == record.Revision {
		s.active.source = source
		if tok != nil {
			s.active.lastAccessToken = tok.AccessToken
		}
	} else {
		active := &browserSession[C]{store: s, path: path, revision: record.Revision, source: source}
		if tok != nil {
			active.lastAccessToken = tok.AccessToken
		}
		s.active = active
	}
	return tok, nil
}

func (s *BrowserSessionStore[C]) signInRequired() error {
	return fmt.Errorf("%w: run tasksmith auth login %s", ErrMissingCredential, s.provider)
}

func browserRefreshHTTPClient() *http.Client {
	client := &http.Client{Timeout: 15 * time.Second}
	if http.DefaultClient != nil && http.DefaultClient.Transport != nil {
		client.Transport = http.DefaultClient.Transport
	}
	return client
}

type browserSession[C any] struct {
	store           *BrowserSessionStore[C]
	path            string
	revision        string
	source          oauth2.TokenSource
	lastAccessToken string
}

func (s *browserSession[C]) Token() (*oauth2.Token, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.active != s {
		return nil, s.store.signInRequired()
	}
	tok, err := s.source.Token()
	if err == nil && tok != nil {
		s.lastAccessToken = tok.AccessToken
	}
	return tok, err
}
