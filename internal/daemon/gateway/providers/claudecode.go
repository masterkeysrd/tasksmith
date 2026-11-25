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

func newClaudeCLI(dir string) (auth.VendorCLI, error) {
	return auth.NewVendorCLI("claude", "CLAUDE_CONFIG_DIR", dir)
}

var claudeCodeOAuth = auth.NewCLISessionStore(auth.CLISessionConfig[*loomclaudecode.Provider]{
	Provider: ClaudeCodeName,
	Label:    "Continue with Claude",
	NewCLI:   newClaudeCLI,
	Login:    claudeLogin,
	Account:  claudeAccount,
	Logout:   claudeLogout,
	Build:    newClaudeBackend,
})

func init() {
	auth.RegisterBrowserOAuthDriver(ClaudeCodeName, claudeCodeOAuth)
}

func NewClaudeCode(name string, credentials *auth.Config, hasOverrides bool) (*loomclaudecode.Provider, error) {
	if err := requireBrowserOAuth(ClaudeCodeName, name, credentials, hasOverrides); err != nil {
		return nil, err
	}
	return claudeCodeOAuth.SharedRuntime()
}

func claudeRuntime(cli auth.VendorCLI) loomclaudecode.SpawnedStdio {
	return loomclaudecode.SpawnedStdio{
		Path:             cli.Executable,
		Args:             []string{"--setting-sources", "", "--strict-mcp-config", "--no-chrome"},
		Env:              cli.Env,
		WorkingDirectory: cli.WorkDir,
		ConfigDir:        cli.ProfileDir,
	}
}

func newClaudeBackend(cli auth.VendorCLI) (*loomclaudecode.Provider, error) {
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

func claudeLogin(ctx context.Context, cli auth.VendorCLI) error {
	if err := cli.Command(ctx, "auth", "login", "--claudeai").Run(); err != nil {
		return auth.VendorAuthError(ctx, "Claude Code", "login")
	}
	return nil
}

func claudeAccount(ctx context.Context, cli auth.VendorCLI) (string, bool, error) {
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

func claudeLogout(ctx context.Context, cli auth.VendorCLI) (bool, error) {
	status, err := readClaudeAuthStatus(ctx, cli)
	if err != nil || !status.subscription() {
		return false, err
	}
	if err := cli.Command(ctx, "auth", "logout").Run(); err != nil {
		return false, auth.VendorAuthError(ctx, "Claude Code", "logout")
	}
	return true, nil
}

func readClaudeAuthStatus(ctx context.Context, cli auth.VendorCLI) (claudeAuthStatus, error) {
	output, err := cli.Command(ctx, "auth", "status", "--json").Output()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return claudeAuthStatus{}, auth.VendorAuthError(ctx, "Claude Code", "auth status")
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
