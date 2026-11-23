package providers

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	loomcopilot "github.com/masterkeysrd/loom/providers/copilot"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	copilot "github.com/microsoft/copilot-sdk/go/v2"
	"github.com/microsoft/copilot-sdk/go/v2/rpc"
)

const GitHubCopilotName = "github-copilot"

var copilotOAuth = &subscriptionDriver[*loomcopilot.Provider]{
	name:  GitHubCopilotName,
	label: "Continue with GitHub",
	newCLI: func(dir string) (vendorCLI, error) {
		return newVendorCLI("copilot", "COPILOT_HOME", dir, "COPILOT_DISABLE_KEYTAR=1", "COPILOT_PLUGIN_DIR_ONLY=true", "USE_TGREP=false")
	},
	login:   copilotLoginCompatible,
	account: copilotAccount,
	logout:  copilotLogout,
	build:   newCopilotBackend,
}

func init() {
	auth.RegisterBrowserOAuthDriver(GitHubCopilotName, copilotOAuth)
}

func NewCopilot(name string, credentials *auth.Config, hasOverrides bool) (*loomcopilot.Provider, error) {
	return copilotOAuth.open(name, credentials, hasOverrides)
}

func copilotRuntime(cli vendorCLI) copilot.SpawnedStdioConnection {
	return copilot.SpawnedStdioConnection{
		Path:             cli.executable,
		Args:             []string{"--disable-builtin-mcps", "--no-custom-instructions", "--no-bash-env", "--sync-session=off"},
		Env:              cli.env,
		BaseDirectory:    cli.profileDir,
		WorkingDirectory: cli.workDir,
	}
}

func newCopilotBackend(cli vendorCLI) (*loomcopilot.Provider, error) {
	return loomcopilot.NewProvider(&loomcopilot.Config{
		Connection: copilotRuntime(cli), WorkingDir: cli.workDir,
		ClientMode: copilot.ModeEmpty, ClientName: "TaskSmith", LogLevel: "error",
	})
}

func copilotLoginCompatible(ctx context.Context, cli vendorCLI) error {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	client, err := startCopilotAuthClient(probeCtx, cli)
	cancel()
	if err != nil {
		return err
	}
	client.ForceStop()
	return copilotLogin(ctx, cli)
}

func copilotLogin(ctx context.Context, cli vendorCLI) error {
	if err := cli.command(ctx, "--no-auto-update", "login", "--web-flow").Run(); err != nil {
		return vendorAuthError(ctx, "GitHub Copilot", "login")
	}
	return nil
}

func copilotAccount(ctx context.Context, cli vendorCLI) (string, bool, error) {
	client, err := startCopilotAuthClient(ctx, cli)
	if err != nil {
		return "", false, err
	}
	defer client.ForceStop()
	account, _, err := readCopilotAccount(ctx, client)
	return account, account != "", err
}

func copilotLogout(ctx context.Context, cli vendorCLI) (bool, error) {
	client, err := startCopilotAuthClient(ctx, cli)
	if err != nil {
		return false, err
	}
	defer client.ForceStop()
	_, identity, err := readCopilotAccount(ctx, client)
	if err != nil || identity == nil {
		return false, err
	}
	if _, err := client.RPC.Account.Logout(ctx, &rpc.AccountLogoutRequest{AuthInfo: identity}); err != nil {
		return false, vendorAuthError(ctx, "GitHub Copilot", "logout")
	}
	return true, nil
}

func startCopilotAuthClient(ctx context.Context, cli vendorCLI) (*copilot.Client, error) {
	client := copilot.NewClient(&copilot.ClientOptions{
		Runtime:       copilotRuntime(cli),
		Observability: copilot.ClientObservabilityOptions{LogLevel: "none"},
		Mode:          copilot.ModeEmpty,
	})
	if err := client.Start(ctx); err != nil {
		client.ForceStop()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("GitHub Copilot auth runtime failed to start; check that the installed Copilot CLI is compatible with the TaskSmith SDK")
	}
	return client, nil
}

var copilotLoginPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:_[A-Za-z0-9]+)?$`)

func readCopilotAccount(ctx context.Context, client *copilot.Client) (string, rpc.AuthInfo, error) {
	status, err := client.GetAuthStatus(ctx)
	if err != nil {
		return "", nil, vendorAuthError(ctx, "GitHub Copilot", "auth status")
	}
	return parseCopilotAccount(*status)
}

func parseCopilotAccount(status copilot.GetAuthStatusResponse) (string, rpc.AuthInfo, error) {
	if !status.IsAuthenticated {
		return "", nil, nil
	}
	if status.AuthType == nil {
		return "", nil, errors.New("GitHub Copilot returned an invalid authentication type")
	}
	kind := rpc.AuthInfoType(*status.AuthType)
	if kind != rpc.AuthInfoTypeUser && kind != rpc.AuthInfoTypeAccount {
		return "", nil, nil
	}
	if status.Login == nil || len(*status.Login) > 39 || !copilotLoginPattern.MatchString(*status.Login) {
		return "", nil, errors.New("GitHub Copilot returned an invalid account login")
	}
	if status.Host == nil || !validCopilotAccountHost(*status.Host) {
		return "", nil, errors.New("GitHub Copilot returned an invalid account host")
	}
	login, host := *status.Login, *status.Host
	if kind == rpc.AuthInfoTypeAccount {
		return login, rpc.AccountAuthInfo{Host: host, Login: login}, nil
	}
	return login, rpc.UserAuthInfo{Host: host, Login: login}, nil
}

func validCopilotAccountHost(host string) bool {
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		(u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}
