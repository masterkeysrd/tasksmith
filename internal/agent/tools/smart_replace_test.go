package tools

import (
	"testing"
)

func TestSmartReplace_Exact(t *testing.T) {
	content := "func foo() {\n\tfmt.Println(1)\n}"
	target := "func foo() {\n\tfmt.Println(1)\n}"
	repl := "func foo() {\n\tfmt.Println(2)\n}"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "func foo() {\n\tfmt.Println(2)\n}"
	if out != expected {
		t.Errorf("got %q, want %q", out, expected)
	}
}

func TestSmartReplace_FuzzyIndent(t *testing.T) {
	content := "func foo() {\n\t\tfmt.Println(1)\n\t\tfmt.Println(2)\n}"
	// Target provided by agent with wrong indentation (flat)
	target := "fmt.Println(1)\nfmt.Println(2)"
	// Replacement provided by agent is also flat
	repl := "fmt.Println(3)\nfmt.Println(4)"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	// It should automatically apply the "\t\t" base indent!
	expected := "func foo() {\n\t\tfmt.Println(3)\n\t\tfmt.Println(4)\n}"
	if out != expected {
		t.Errorf("got\n%q\nwant\n%q", out, expected)
	}
}

func TestSmartReplace_FuzzyTrailingSpace(t *testing.T) {
	content := "line 1\nline 2 \nline 3"
	// Agent missed the trailing space on line 2
	target := "line 2\n"
	repl := "line replaced"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "line 1\nline replaced\nline 3"
	if out != expected {
		t.Errorf("got\n%q\nwant\n%q", out, expected)
	}
}

func TestSmartReplace_MultipleMatches(t *testing.T) {
	content := "line 1\nline 1\nline 2"
	target := "line 1"
	repl := "line replaced"

	// Without replaceAll, should fail
	_, _, err := SmartReplace(content, target, repl, false)
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}

	// With replaceAll, should succeed
	out, count, err := SmartReplace(content, target, repl, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 matches, got %d", count)
	}
	expected := "line replaced\nline replaced\nline 2"
	if out != expected {
		t.Errorf("got\n%q\nwant\n%q", out, expected)
	}
}

func TestSmartReplace_FuzzySequenceMatcher(t *testing.T) {
	content := "func main() {\n\tinit()\n\tstartServer()\n\tcleanup()\n}"

	// Agent hallucinates an extra line in the target and makes a typo,
	// but the overall block is very similar.
	// content: [func main() {, init(), startServer(), cleanup(), }]
	// target:  [func main() {, initt(), startServer(), typo(), cleanup(), }]
	target := "func main() {\n\tinitt()\n\tstartServer()\n\ttypo()\n\tcleanup()\n}"
	repl := "func main() {\n\t// replaced\n}"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "func main() {\n\t// replaced\n}"
	if out != expected {
		t.Errorf("got\n%q\nwant\n%q", out, expected)
	}
}
