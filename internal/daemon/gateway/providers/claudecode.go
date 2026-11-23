package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"os/exec"
	"strings"

	loomclaudecode "github.com/masterkeysrd/loom/providers/claudecode"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

const ClaudeCodeName = "claudecode"

var claudeCodeOAuth = &subscriptionDriver[*loomclaudecode.Provider]{
	name:  ClaudeCodeName,
	label: "Continue with Claude",
	newCLI: func(dir string) (vendorCLI, error) {
		return newVendorCLI("claude", "CLAUDE_CONFIG_DIR", dir)
	},
	login:   claudeLogin,
	account: claudeAccount,
	logout:  claudeLogout,
	build:   newClaudeBackend,
}

func init() {
	auth.RegisterBrowserOAuthDriver(ClaudeCodeName, claudeCodeOAuth)
}

func NewClaudeCode(name string, credentials *auth.Config, hasOverrides bool) (*loomclaudecode.Provider, error) {
	return claudeCodeOAuth.open(name, credentials, hasOverrides)
}

func claudeRuntime(cli vendorCLI) loomclaudecode.SpawnedStdio {
	return loomclaudecode.SpawnedStdio{
		Path:             cli.executable,
		Args:             []string{"--setting-sources", "", "--strict-mcp-config", "--no-chrome"},
		Env:              cli.env,
		WorkingDirectory: cli.workDir,
		ConfigDir:        cli.profileDir,
	}
}

func newClaudeBackend(cli vendorCLI) (*loomclaudecode.Provider, error) {
	return loomclaudecode.NewProvider(&loomclaudecode.Config{
		Runtime: claudeRuntime(cli),
		Session: loomclaudecode.SessionOptions{PermissionMode: "default"},
	})
}

type claudeAuthStatus struct {
	LoggedIn    *bool  `json:"loggedIn"`
	AuthMethod  string `json:"authMethod"`
	APIProvider string `json:"apiProvider"`
	Email       string `json:"email"`
}

func (s claudeAuthStatus) subscription() bool {
	return s.LoggedIn != nil && *s.LoggedIn && s.AuthMethod == "claude.ai" && s.APIProvider == "firstParty"
}

func claudeLogin(ctx context.Context, cli vendorCLI) error {
	if err := cli.command(ctx, "auth", "login", "--claudeai").Run(); err != nil {
		return vendorAuthError(ctx, "Claude Code", "login")
	}
	return nil
}

func claudeAccount(ctx context.Context, cli vendorCLI) (string, bool, error) {
	status, err := readClaudeAuthStatus(ctx, cli)
	if err != nil || !status.subscription() {
		return "", false, err
	}
	address, err := mail.ParseAddress(status.Email)
	if err != nil || len(status.Email) > 254 || address.Name != "" || address.Address != status.Email ||
		strings.ContainsFunc(status.Email, func(r rune) bool { return r <= ' ' || r >= 0x7f }) {
		return "", false, errors.New("invalid account email returned by Claude Code")
	}
	return status.Email, true, nil
}

func claudeLogout(ctx context.Context, cli vendorCLI) (bool, error) {
	status, err := readClaudeAuthStatus(ctx, cli)
	if err != nil || !status.subscription() {
		return false, err
	}
	if err := cli.command(ctx, "auth", "logout").Run(); err != nil {
		return false, vendorAuthError(ctx, "Claude Code", "logout")
	}
	return true, nil
}

func readClaudeAuthStatus(ctx context.Context, cli vendorCLI) (claudeAuthStatus, error) {
	output, err := cli.command(ctx, "auth", "status", "--json").Output()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return claudeAuthStatus{}, vendorAuthError(ctx, "Claude Code", "auth status")
		}
	}
	return parseClaudeAuthStatus(output, err != nil)
}

func parseClaudeAuthStatus(output []byte, signedOutExit bool) (claudeAuthStatus, error) {
	var status claudeAuthStatus
	if err := json.Unmarshal(output, &status); err != nil || status.LoggedIn == nil ||
		status.AuthMethod == "" || status.APIProvider == "" || *status.LoggedIn == signedOutExit {
		return claudeAuthStatus{}, errors.New("invalid auth status returned by Claude Code")
	}
	return status, nil
}
