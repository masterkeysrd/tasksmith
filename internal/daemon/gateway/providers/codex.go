package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	loomcodex "github.com/masterkeysrd/loom/providers/codex"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

// CodexName identifies the ChatGPT plan provider and its shared registration.
const CodexName = "codex"

var codexOAuth = newCodexDriver()

func init() {
	auth.RegisterBrowserOAuthDriver(CodexName, codexOAuth)
}

func newCodexDriver() *codexDriver {
	return &codexDriver{
		BrowserSessionStore: auth.NewBrowserSessionStore(
			CodexName,
			func(account loomcodex.Credentials) string { return account.Email },
			loomcodex.NewTokenSource,
			auth.WithBrowserSessionExpire(func(c *loomcodex.Credentials) {
				c.Token.Expiry = time.Unix(1, 0).UTC()
			}),
			auth.WithBrowserSessionExpiredCheck(func(c loomcodex.Credentials) bool {
				return !c.Token.Expiry.IsZero() && c.Token.Expiry.Before(time.Now()) && c.Token.RefreshToken == ""
			}),
		),
	}
}

// codexDriver manages the daemon's ChatGPT registration.
type codexDriver struct {
	*auth.BrowserSessionStore[loomcodex.Credentials]
	hostMu sync.Mutex
}

func (*codexDriver) LoginLabel() string { return "Continue with ChatGPT" }

func (d *codexDriver) Authorize(ctx context.Context) (func() (string, error), error) {
	hostID, err := d.hostID()
	if err != nil {
		return nil, fmt.Errorf("prepare ChatGPT host ID: %w", err)
	}
	opts := loomcodex.LoginOptions{AgentName: "TaskSmith", HostID: hostID}
	if previous, err := d.LoadAccount(); err == nil {
		opts.Previous = &previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	account, err := loomcodex.Login(ctx, opts)
	if err != nil {
		return nil, err
	}
	return func() (string, error) {
		return account.Email, d.Save(*account)
	}, nil
}

func (d *codexDriver) hostID() (string, error) {
	d.hostMu.Lock()
	defer d.hostMu.Unlock()
	path, err := xdg.SubDataDir("credentials", CodexName, "host-id")
	if err != nil {
		return "", err
	}
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if id == "" {
			return "", errors.New("ChatGPT host ID is empty")
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	id := loomcodex.NewHostID()
	if err := fsutils.WriteFileAtomic(path, []byte(id), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// NewCodex builds a Loom client using the active ChatGPT registration.
func NewCodex(name string, credentials *auth.Config, hasOverrides bool) (*loomcodex.Provider, error) {
	if err := requireBrowserOAuth(CodexName, name, credentials, hasOverrides); err != nil {
		return nil, err
	}
	source, err := codexOAuth.TokenSource()
	if err != nil {
		return nil, err
	}
	baseClient := *http.DefaultClient
	baseClient.Transport = auth.NewRefreshOnUnauthorizedTransport(baseClient.Transport, codexOAuth.ForceRefresh)
	provider, err := loomcodex.NewProvider(source, &baseClient)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize Codex: %w", ErrInvalidConfig, err)
	}
	return provider, nil
}
