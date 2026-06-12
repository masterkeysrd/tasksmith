package colorscheme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadValidJSON ensures that Load() correctly parses a valid colorscheme file.
func TestLoadValidJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "colorscheme-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	path := filepath.Join(tempDir, "test.json")
	jsonData := `{"name": "test-cs", "groups": {"Normal": {"fg": "#ffffff"}}}`
	if err := os.WriteFile(path, []byte(jsonData), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cs, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cs.Name != "test-cs" {
		t.Errorf("Name = %q, want %q", cs.Name, "test-cs")
	}
	if *cs.Groups["Normal"].Fg != "#ffffff" {
		t.Errorf("Normal.Fg = %q, want #ffffff", *cs.Groups["Normal"].Fg)
	}
}

// TestLoadInvalidJSON ensures that Load() returns an error for invalid JSON.
func TestLoadInvalidJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "colorscheme-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	path := filepath.Join(tempDir, "invalid.json")
	if err := os.WriteFile(path, []byte("{invalid}"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	_, err = Load(path)
	if err == nil {
		t.Fatal("Load() returned no error for invalid JSON")
	}
}

// TestLoadMissingFile ensures that Load() returns an error for missing files.
func TestLoadMissingFile(t *testing.T) {
	_, err := Load("non-existent.json")
	if err == nil {
		t.Fatal("Load() returned no error for missing file")
	}
}

// TestLoadDefault verifies that Find(Default) returns a colorscheme with
// Normal fg=#c8d0e0, bg=#1e1e2e.
func TestLoadDefault(t *testing.T) {
	cs, err := Find(Default)
	if err != nil {
		t.Fatalf("Find(Default) returned error: %v", err)
	}
	if cs.Name != "tasksmith-dark" {
		t.Errorf("Name = %q, want %q", cs.Name, "tasksmith-dark")
	}
	normal, ok := cs.Groups["Normal"]
	if !ok {
		t.Fatal("Missing Normal highlight")
	}
	if normal.Fg == nil || *normal.Fg != "#c8d0e0" {
		t.Errorf("Normal.Fg = %v, want #c8d0e0", normal.Fg)
	}
	if normal.Bg == nil || *normal.Bg != "#1e1e2e" {
		t.Errorf("Normal.Bg = %v, want #1e1e2e", normal.Bg)
	}
}

// TestIsValidHexColor tests the isValidHexColor helper with various inputs.
func TestIsValidHexColor(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"#ffffff", true},
		{"#000000", true},
		{"#ffffff00", true},
		{"ffffff", true},
		{"#fff", false},
		{"red", false},
		{"#gggggg", false},
	}

	for _, tc := range tests {
		if got := isValidHexColor(tc.input); got != tc.want {
			t.Errorf("isValidHexColor(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestResolveAllCircularLink detects circular links.
func TestResolveAllCircularLink(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("B")},
			"B": {Link: new("A")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() missed circular dependency A -> B -> A")
	}
}

func TestResolveAllCircularLinkDeep(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("B")},
			"B": {Link: new("C")},
			"C": {Link: new("A")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() missed circular dependency A -> B -> C -> A")
	}
}

func TestResolveAllMissingTarget(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("B")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() missed missing link target B")
	}
}

func TestResolveLinkToExisting(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Link: new("A")},
		},
	}
	resolved := Resolve(cs)
	if resolved["B"].Fg != "#111111" {
		t.Errorf("B.Fg = %q, want #111111", resolved["B"].Fg)
	}
}

func TestResolveLinkWithOverrides(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111"), Bg: new("#222222")},
			"B": {Link: new("A"), Fg: new("#333333")},
		},
	}
	resolved := Resolve(cs)
	if resolved["B"].Fg != "#333333" {
		t.Errorf("B.Fg = %q, want #333333", resolved["B"].Fg)
	}
	if resolved["B"].Bg != "#222222" {
		t.Errorf("B.Bg = %q, want #222222", resolved["B"].Bg)
	}
}

func TestResolveMultipleLinksInChain(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Link: new("A"), Bg: new("#222222")},
			"C": {Link: new("B"), Bold: new(true)},
		},
	}
	resolved := Resolve(cs)
	c := resolved["C"]
	if c.Fg != "#111111" || c.Bg != "#222222" || !c.Bold {
		t.Errorf("C = %+v, want Fg=#111111, Bg=#222222, Bold=true", c)
	}
}

func TestResolveEmptyColorscheme(t *testing.T) {
	cs := &Colorscheme{}
	resolved := Resolve(cs)
	if len(resolved) != 0 {
		t.Errorf("Resolve(empty) returned %d entries", len(resolved))
	}
}

func TestGetResolvedExisting(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
		},
	}
	color, ok := GetResolved(cs, "A")
	if !ok || color.Fg != "#111111" {
		t.Errorf("GetResolved(A) = %+v, %v; want #111111, true", color, ok)
	}
}

func TestGetResolvedNonExisting(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
		},
	}
	_, ok := GetResolved(cs, "B")
	if ok {
		t.Error("GetResolved(B) returned true for non-existent group")
	}
}

func TestResolveAllInvalidHex(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("invalid")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() failed to detect invalid hex color")
	}
}

func TestResolveAllShortHex(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#fff")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() failed to detect short hex color (not supported yet)")
	}
}

func TestResolveAllHexWithoutPrefix(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("ffffff")},
		},
	}
	if err := ResolveAll(cs); err != nil {
		t.Errorf("ResolveAll() failed on valid hex without prefix: %v", err)
	}
}

func TestResolveAllNoErrors(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Link: new("A")},
		},
	}
	if err := ResolveAll(cs); err != nil {
		t.Errorf("ResolveAll() returned unexpected error: %v", err)
	}
}

func TestResolveAllNilColorscheme(t *testing.T) {
	if err := ResolveAll(nil); err != nil {
		t.Errorf("ResolveAll(nil) returned error: %v", err)
	}
}

func TestResolveNilColorscheme(t *testing.T) {
	res := Resolve(nil)
	if len(res) != 0 {
		t.Errorf("Resolve(nil) returned %d entries", len(res))
	}
}

func TestGetResolvedNilColorscheme(t *testing.T) {
	_, ok := GetResolved(nil, "A")
	if ok {
		t.Error("GetResolved(nil, A) returned true")
	}
}

func TestResolveAllWithModifiers(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Bold: new(true), Italic: new(true)},
		},
	}
	res := Resolve(cs)
	if !res["A"].Bold || !res["A"].Italic {
		t.Errorf("A = %+v, want Bold=true, Italic=true", res["A"])
	}
}

func TestMergeCopiesAndOverlays(t *testing.T) {
	cs1 := &Colorscheme{
		Name: "cs1",
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Fg: new("#222222")},
		},
	}
	cs2 := &Colorscheme{
		Name: "cs2",
		Groups: map[string]Highlight{
			"B": {Fg: new("#333333")},
			"C": {Fg: new("#444444")},
		},
	}

	merged := Merge(cs1, cs2)
	if merged.Name != "cs2" {
		t.Errorf("Merged Name = %q, want cs2", merged.Name)
	}
	if *merged.Groups["A"].Fg != "#111111" {
		t.Errorf("A.Fg = %q, want #111111", *merged.Groups["A"].Fg)
	}
	if *merged.Groups["B"].Fg != "#333333" {
		t.Errorf("B.Fg = %q, want #333333", *merged.Groups["B"].Fg)
	}
	if *merged.Groups["C"].Fg != "#444444" {
		t.Errorf("C.Fg = %q, want #444444", *merged.Groups["C"].Fg)
	}
}

func TestMergeIgnoresNilSchemes(t *testing.T) {
	cs1 := &Colorscheme{
		Name: "cs1",
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
		},
	}
	merged := Merge(nil, cs1, nil)
	if merged.Name != "cs1" || len(merged.Groups) != 1 {
		t.Errorf("Merged state invalid: Name=%q, Len=%d", merged.Name, len(merged.Groups))
	}
}

func TestResolveLinkWithAllModifiers(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"Base": {
				Fg:        new("#111111"),
				Bg:        new("#222222"),
				Bold:      new(true),
				Underline: new(true),
				Italic:    new(true),
				Reverse:   new(true),
			},
			"Linked": {Link: new("Base")},
		},
	}
	res := Resolve(cs)["Linked"]
	expected := ResolvedColor{
		Fg:        "#111111",
		Bg:        "#222222",
		Bold:      true,
		Underline: true,
		Italic:    true,
		Reverse:   true,
	}
	if res != expected {
		t.Errorf("Linked = %+v, want %+v", res, expected)
	}
}

func TestFullResolutionPipeline(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Link: new("A"), Bg: new("#222222")},
		},
	}
	if err := ResolveAll(cs); err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}
	res := Resolve(cs)
	if res["B"].Fg != "#111111" || res["B"].Bg != "#222222" {
		t.Errorf("B = %+v, want Fg=#111111, Bg=#222222", res["B"])
	}
}

func TestFullResolutionPipelineWithCircular(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("A")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("Expected error for circular link A->A")
	}
}

func TestLinkChainWithOverrideAtEachLevel(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"Level0": {Fg: new("#000000"), Bg: new("#000000"), Bold: new(false)},
			"Level1": {Link: new("Level0"), Fg: new("#111111")},
			"Level2": {Link: new("Level1"), Bg: new("#222222")},
			"Level3": {Link: new("Level2"), Bold: new(true)},
		},
	}
	res := Resolve(cs)["Level3"]
	if res.Fg != "#111111" || res.Bg != "#222222" || !res.Bold {
		t.Errorf("Level3 = %+v, want Fg=#111111, Bg=#222222, Bold=true", res)
	}
}

func TestResolveReturnsNewMapEachTime(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
		},
	}
	res1 := Resolve(cs)
	res1["A"] = ResolvedColor{Fg: "#ff0000"}
	res2 := Resolve(cs)
	if res2["A"].Fg != "#111111" {
		t.Error("Resolve() returned a shared map or modified original highlights")
	}
}

func TestColorschemeJSONRoundTrip(t *testing.T) {
	cs := &Colorscheme{
		Name: "round-trip",
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111"), Bold: new(true)},
		},
	}
	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Colorscheme
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.Name != cs.Name || *decoded.Groups["A"].Fg != "#111111" || !*decoded.Groups["A"].Bold {
		t.Errorf("Round-trip failed: %+v", decoded)
	}
}

func TestHighlightLinkOnly(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Fg: new("#111111")},
			"B": {Link: new("A")},
		},
	}
	res := Resolve(cs)
	if res["B"].Fg != "#111111" {
		t.Errorf("B.Fg = %q, want #111111", res["B"].Fg)
	}
}

func TestHighlightSelfLink(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("A")},
		},
	}
	if err := ResolveAll(cs); err == nil {
		t.Error("ResolveAll() failed to detect self-link A->A")
	}
}

func TestHighlightSelfLinkResolve(t *testing.T) {
	cs := &Colorscheme{
		Groups: map[string]Highlight{
			"A": {Link: new("A")},
		},
	}
	// Resolve() should handle it without infinite recursion if validation was skipped
	// but it's better to verify it doesn't crash.
	res := Resolve(cs)
	if _, ok := res["A"]; !ok {
		// Even if circular, it should return something or just not crash.
		// Topo resolve will mark it as visited and return empty for the rest.
	}
}

func TestPaletteResolution(t *testing.T) {
	jsonData := `{
		"name": "palette-test",
		"palette": {
			"my-red": "#ff0000",
			"my-blue": "#0000ff"
		},
		"groups": {
			"Error": { "fg": "my-red" },
			"Info":  { "fg": "my-blue" },
			"Other": { "fg": "#ffffff" }
		}
	}`

	var cs Colorscheme
	if err := json.Unmarshal([]byte(jsonData), &cs); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	resolvePalette(&cs)

	if *cs.Groups["Error"].Fg != "#ff0000" {
		t.Errorf("Error.Fg = %v, want #ff0000", *cs.Groups["Error"].Fg)
	}
	if *cs.Groups["Info"].Fg != "#0000ff" {
		t.Errorf("Info.Fg = %v, want #0000ff", *cs.Groups["Info"].Fg)
	}
	if *cs.Groups["Other"].Fg != "#ffffff" {
		t.Errorf("Other.Fg = %v, want #ffffff", *cs.Groups["Other"].Fg)
	}
}
