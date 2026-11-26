package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestSetupXDG(t *testing.T) {
	outer := SetupXDG(t, "outer")
	var inner string
	t.Run("nested", func(t *testing.T) {
		inner = SetupXDG(t, "inner")
		for kind, dir := range map[xdg.VarType]string{
			xdg.VarTypeConfig: "config", xdg.VarTypeData: "data", xdg.VarTypeCache: "cache",
			xdg.VarTypeRuntime: "runtime", xdg.VarTypeState: "state",
		} {
			if got, err := xdg.Home(kind); err != nil || got != filepath.Join(inner, dir, "inner") {
				t.Fatalf("Home(%v) = %q, %v", kind, got, err)
			}
		}
	})
	if got := xdg.GetVar(xdg.VarTypeConfig); got != filepath.Join(outer, "config") || xdg.AppName() != "outer" || os.Getenv("HOME") != outer {
		t.Fatalf("environment or cache not restored: %q, %q, %q", got, xdg.AppName(), os.Getenv("HOME"))
	}
	if _, err := os.Stat(inner); !os.IsNotExist(err) {
		t.Fatalf("inner directory not cleaned: %v", err)
	}
}
