package providers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type vendorCLI struct {
	executable string
	profileDir string
	workDir    string
	env        []string
}

func newVendorCLI(binary, configKey, dir string, extraEnv ...string) (vendorCLI, error) {
	executable, err := exec.LookPath(binary)
	if err != nil || !filepath.IsAbs(executable) {
		return vendorCLI{}, fmt.Errorf("install %s on the TaskSmith daemon host and add it to PATH", binary)
	}
	work := filepath.Join(dir, "work")
	home := filepath.Join(dir, "home")
	tmp := filepath.Join(dir, "tmp")
	for _, path := range []string{work, home, tmp} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return vendorCLI{}, fmt.Errorf("prepare isolated %s directories: %w", binary, err)
		}
	}
	var pathDirs []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(entry) {
			pathDirs = append(pathDirs, entry)
		}
	}
	env := []string{
		"PATH=" + strings.Join(pathDirs, string(os.PathListSeparator)),
		"HOME=" + home,
		"TMPDIR=" + tmp,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		configKey + "=" + dir,
	}
	for _, name := range []string{
		"LANG", "LC_ALL", "LC_CTYPE", "TERM",
		"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_CURRENT_DESKTOP",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return vendorCLI{executable: executable, profileDir: dir, workDir: work, env: append(env, extraEnv...)}, nil
}

func (c vendorCLI) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.executable, args...)
	cmd.Dir = c.workDir
	cmd.Env = c.env
	cmd.WaitDelay = time.Second
	return cmd
}

func vendorAuthError(ctx context.Context, vendor, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s %s failed", vendor, action)
}
