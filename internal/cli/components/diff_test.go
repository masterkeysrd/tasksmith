package components

import (
	"strings"
	"testing"
)

func TestDiffView(t *testing.T) {
	render := renderComponent(t, 80)

	// Empty diff renders empty
	gotEmpty := render(DiffView(DiffProps{Text: ""}))
	if gotEmpty != "" {
		t.Fatalf("expected empty string for empty diff, got %q", gotEmpty)
	}

	// Unified diff with added and deleted lines
	diffText := `--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 package main
-import "fmt"
+import "os"
 func main() {}
`
	got := render(DiffView(DiffProps{Text: diffText, Path: "main.go"}))
	if !strings.Contains(got, "@@ -1,3 +1,3 @@") {
		t.Fatalf("expected hunk header, got: %q", got)
	}
	if !strings.Contains(got, "import \"fmt\"") || !strings.Contains(got, "import \"os\"") {
		t.Fatalf("expected diff code lines, got: %q", got)
	}
	if !strings.Contains(got, "│") {
		t.Fatalf("expected separator column, got: %q", got)
	}

	// Automatic language extraction from +++ b/...
	gotAuto := render(DiffView(DiffProps{Text: diffText}))
	if !strings.Contains(gotAuto, "import \"fmt\"") {
		t.Fatalf("expected diff lines with auto-extracted path, got: %q", gotAuto)
	}
}
