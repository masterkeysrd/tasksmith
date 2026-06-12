package colorscheme

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestDiscovery(t *testing.T) {
	origHome := os.Getenv("HOME")
	origConfig := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("HOME", origHome)
	defer os.Setenv("XDG_CONFIG_HOME", origConfig)

	tempDir, _ := os.MkdirTemp("", "tasksmith-config-*")
	defer os.RemoveAll(tempDir)

	os.Setenv("HOME", tempDir)
	os.Unsetenv("XDG_CONFIG_HOME")

	// Reset XDG cache to pick up new HOME
	xdg.ClearCache()

	t.Run("ListBuiltin", func(t *testing.T) {
		names := ListBuiltin()
		sort.Strings(names)
		expected := []string{"dark", "light"}
		sort.Strings(expected)
		if !reflect.DeepEqual(names, expected) {
			t.Errorf("ListBuiltin() = %v, want %v", names, expected)
		}
	})

	t.Run("ListUser", func(t *testing.T) {
		configDir := filepath.Join(tempDir, ".config", "tasksmith", "colorschemes")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("failed to create config dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "custom.json"), []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write custom colorscheme: %v", err)
		}

		names := ListUser()
		expected := []string{"custom"}
		if !reflect.DeepEqual(names, expected) {
			t.Errorf("ListUser() = %v, want %v", names, expected)
		}
	})

	t.Run("ListCombined", func(t *testing.T) {
		names := List()
		sort.Strings(names)
		expected := []string{"custom", "dark", "light"}
		sort.Strings(expected)
		if !reflect.DeepEqual(names, expected) {
			t.Errorf("List() = %v, want %v", names, expected)
		}
	})

	t.Run("FindUserShadowing", func(t *testing.T) {
		configDir := filepath.Join(tempDir, ".config", "tasksmith", "colorschemes")
		// Shadow the "dark" theme
		if err := os.WriteFile(filepath.Join(configDir, "dark.json"), []byte(`{"name": "user-dark"}`), 0644); err != nil {
			t.Fatalf("failed to write shadowed dark colorscheme: %v", err)
		}

		cs, err := Find("dark")
		if err != nil {
			t.Fatalf("Find() failed: %v", err)
		}
		if cs.Name != "user-dark" {
			t.Errorf("Find() returned %q, want user-dark (shadowing failed)", cs.Name)
		}
	})
}
