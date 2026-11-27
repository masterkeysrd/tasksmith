package styles_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestStylesFollowScheme(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := xdg.SubConfigDir("themes")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	load := func(hex string) {
		t.Helper()
		data := fmt.Sprintf(`{"name":"custom","extends":"dark","colors":{"text":{"primary":%q}}}`, hex)
		if err := os.WriteFile(filepath.Join(dir, "custom.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := theme.Set("custom"); err != nil {
			t.Fatal(err)
		}
	}
	load("#ff0000")
	builds := 0
	probe := trail.FC("StylesProbe", func(frame int) trail.Node {
		name := styles.Use(func(s *theme.Scheme) string {
			builds++
			return s.Name
		})
		color := styles.UseCommon().Primary.ForegroundOpt().Value()
		r, g, b, _ := color.RGBA()
		return trail.Text(fmt.Sprintf("%s %d %04x%04x%04x", name, frame, r, g, b))
	})
	env := testutils.NewEnv(t, 80, 10)
	for frame := range 2 {
		want := fmt.Sprintf("custom %d ffff00000000", frame)
		if got := env.Render(probe(frame)); got != want || builds != 1 {
			t.Fatalf("frame %d: got %q, builds %d; want %q, one build", frame, got, builds, want)
		}
	}
	load("#0000ff")
	if got := env.Render(probe(2)); got != "custom 2 00000000ffff" || builds != 2 {
		t.Fatalf("after same-name reload: got %q, builds %d", got, builds)
	}
}
