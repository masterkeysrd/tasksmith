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

func TestSmartReplace_FuzzySequenceBoundaryBug(t *testing.T) {
	content := "func before() {\n}\n\nfuncc main() {\n\tinit()\n\trun()\n}\n\nfunc after() {\n}"
	target := "func main() {\n\tinit()\n\trun()\n}"
	repl := "func main() {\n\tsetup()\n}"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "func before() {\n}\n\nfunc main() {\n\tsetup()\n}\n\nfunc after() {\n}"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q", out, expected)
	}
}

func TestSmartReplace_SliceCapacityCorruption(t *testing.T) {
	content := "line 1\nline 2 \nline 3 \nline 4\nline 5"
	target := "line 2\nline 3"
	repl := "line A\nline B\nline C\nline D"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "line 1\nline A\nline B\nline C\nline D\nline 4\nline 5"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q", out, expected)
	}
}

func TestSmartReplace_CRLFConversion(t *testing.T) {
	content := "line 1\r\nline 2\r\nline 3\r\n"
	target := "line 2\r\n"
	repl := "line replaced\r\n"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "line 1\r\nline replaced\r\nline 3\r\n"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (CRLF line endings were converted to LF)", out, expected)
	}
}

func TestSmartReplace_MixedIndent(t *testing.T) {
	content := "\tfunc foo() {\n\t\tbar()\n\t}"
	target := "bar() "                            // trailing space forces Stage 2 fuzzy matching
	repl := "    if cond {\n        baz()\n    }" // uses 4-space indent units

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	// The baz() line should use tabs to match the base indentation \t\t, not mix tabs and spaces
	expected := "\tfunc foo() {\n\t\tif cond {\n\t\t\tbaz()\n\t\t}\n\t}"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (mixed tabs and spaces indent corruption)", out, expected)
	}
}

func TestSmartReplace_EmptyReplacementLeavesEmptyLine(t *testing.T) {
	content := "line 1\nline 2\nline 3"
	target := "line 2 " // trailing space forces Stage 2 fuzzy matching
	repl := ""

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "line 1\nline 3"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (empty replacement left an extra newline/empty line)", out, expected)
	}
}

func TestSmartReplace_Stage3MultiMatchAmbiguity(t *testing.T) {
	content := "func first() {\n\ta = 1\n\tb = 22\n\tc = 3\n\td = 4\n}\n\nfunc first() {\n\ta = 1\n\tb = 222\n\tc = 3\n\td = 4\n}\n\nfunc pad() {\n\tx = 1\n}"
	target := "func first() {\n\ta = 1\n\tb = 2\n\tc = 3\n\td = 4\n}"
	repl := "func replaced() {\n\ta = 3\n}"

	_, _, err := SmartReplace(content, target, repl, false)
	if err == nil {
		t.Fatal("expected error due to ambiguous Stage 3 multi-match, but got nil")
	}
}

func TestSmartReplace_FlatLineIndentationPreserved(t *testing.T) {
	content := "\tfunc foo() {\n\t\tbar()\n\t}"
	target := "bar() "                                             // forces Stage 2 matching
	repl := "    if cond {\n        baz()\n    }\n// flat comment" // last line is intentionally flat

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "\tfunc foo() {\n\t\tif cond {\n\t\t\tbaz()\n\t\t}\n// flat comment\n\t}"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (flat line indentation was corrupted/indented)", out, expected)
	}
}

func TestSmartReplace_NoTrailingNewlinePreserved(t *testing.T) {
	content := "line 1\nline 2"
	target := "line 2 " // forces Stage 2 matching
	repl := "line replaced"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "line 1\nline replaced"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (trailing newline was forced onto the content)", out, expected)
	}
}

func TestSmartReplace_EmptyTarget(t *testing.T) {
	content := "line 1\nline 2"
	_, _, err := SmartReplace(content, "   ", "replaced", false)
	if err == nil {
		t.Fatal("expected error for empty target block, got nil")
	}
}

func TestSmartReplace_FuzzyBoundaryEdgeCases(t *testing.T) {
	// Case 1: Target is at the very beginning of the file (index 0) with a minor typo
	content1 := "func startMeUp() {\n\ta = 1\n\tb = 2\n}\n\nfunc main() {}"
	target1 := "func startMeUp() {\n\ta = 1\n\tb = 22\n}" // minor typo '22' vs '2'
	repl1 := "func startMeUp() {\n\ta = 100\n}"

	out1, count1, err1 := SmartReplace(content1, target1, repl1, false)
	if err1 != nil {
		t.Fatalf("unexpected err in start edge case: %v", err1)
	}
	if count1 != 1 {
		t.Fatalf("expected 1 match in start edge case, got %d", count1)
	}
	expected1 := "func startMeUp() {\n\ta = 100\n}\n\nfunc main() {}"
	if out1 != expected1 {
		t.Errorf("start edge case failed:\ngot:\n%q\nwant:\n%q", out1, expected1)
	}

	// Case 2: Target is at the very end of the file
	content2 := "func main() {}\n\nfunc endMeUp() {\n\ta = 1\n\tb = 2\n}"
	target2 := "func endMeUp() {\n\ta = 1\n\tb = 22\n}"
	repl2 := "func endMeUp() {\n\ta = 200\n}"

	out2, count2, err2 := SmartReplace(content2, target2, repl2, false)
	if err2 != nil {
		t.Fatalf("unexpected err in EOF edge case: %v", err2)
	}
	if count2 != 1 {
		t.Fatalf("expected 1 match in EOF edge case, got %d", count2)
	}
	expected2 := "func main() {}\n\nfunc endMeUp() {\n\ta = 200\n}"
	if out2 != expected2 {
		t.Errorf("EOF edge case failed:\ngot:\n%q\nwant:\n%q", out2, expected2)
	}
}

func TestSmartReplace_NormalizeIndentLossless(t *testing.T) {
	// A 5-space relative indentation should be converted to 1 tab and 1 space, preserving the space
	content := "\tfunc foo() {\n\t\tbar()\n\t}"
	target := "bar() "                             // forces Stage 2 fuzzy matching
	repl := "    if cond {\n         baz()\n    }" // 4 spaces base, 9 spaces nested line (5 spaces relative)

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	// Target uses tabs (origIndent is \t\t).
	// \t\t + \t + " " -> \t\t\t baz()
	expected := "\tfunc foo() {\n\t\tif cond {\n\t\t\t baz()\n\t\t}\n\t}"
	if out != expected {
		t.Errorf("got:\n%q\nwant:\n%q (5 spaces relative indent was promoted to 2 tabs instead of tab+space)", out, expected)
	}
}

func TestSmartReplace_BoundaryProjectionUnrelatedDeletion(t *testing.T) {
	// Original code has hooks block first, then theme check
	content := "var Composer = kitex.FC(\"Composer\", func() {\n\tState1 := 1\n\tState2 := 2\n\tState3 := 3\n\tState4 := 4\n\tState5 := 5\n\tif t == nil {\n\t\treturn\n\t}\n\tblueColor := 1\n})"
	// Agent swapped the order: theme check is placed before hooks in target
	target := "if t == nil {\n\treturn\n}\nState1 := 1\nState2 := 2\nState3 := 3\nState4 := 4\nState5 := 5\nblueColor := 1"
	repl := "if t == nil {\n\treturn\n}\nblueColor := 1"

	// This should fail to match (count = 0) because:
	// 1. The bad match at i=0 is rejected by prefix/suffix similarity validation.
	// 2. The correct match at the end has too low of a ratio (4/9 = 44%) to meet the 65% threshold.
	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 matches due to similarity check, but got %d (output: %q)", count, out)
	}
}

func TestSmartReplace_InnerWhitespaceAlignment(t *testing.T) {
	content := "var border = Border{\n\tTop:      \"==\",\n\tBottom:   \"==\",\n}"
	target := "var border = Border{\n\tTop:        \"==\",\n\tBottom:  \"==\",\n}"
	repl := "var border = Border{\n\tTop:      \"--\",\n\tBottom:   \"--\",\n}"

	out, count, err := SmartReplace(content, target, repl, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 match, got %d", count)
	}
	expected := "var border = Border{\n\tTop:      \"--\",\n\tBottom:   \"--\",\n}"
	if out != expected {
		t.Errorf("got\n%q\nwant\n%q", out, expected)
	}
}
