package mentions

import (
	"reflect"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestScanTokens(t *testing.T) {
	input := `
Hello world! Check @composer.go and also (@cli/app.go#L10-L20)!
Look at @docs/. It has documentation.
Directory without punctuation: @docs/
Quoted: @"my space file.go"#L5-L15
Single quoted: @'notes.txt'
Explicit file: @file:internal/daemon/app.go#L42
Explicit rule: @rule:go-verification-gate
Explicit skill: @skill:release-runbook
MCP reference: @mcp:server/resource
Email address should be ignored: test@example.com
Inside backticks:
` + "```go\n@secret.env\n```\n" + `
Inside tildes:
` + "~~~\n@another_secret.txt\n~~~\n"

	tokens := ScanTokens(input)
	type wantToken struct {
		full  string
		base  string
		start int
		end   int
	}

	expected := []wantToken{
		{full: "@composer.go", base: "@composer.go", start: 0, end: 0},
		{full: "@cli/app.go#L10-L20", base: "@cli/app.go", start: 10, end: 20},
		{full: "@docs/", base: "@docs/", start: 0, end: 0},
		{full: "@docs/", base: "@docs/", start: 0, end: 0},
		{full: `@"my space file.go"#L5-L15`, base: `@"my space file.go"`, start: 5, end: 15},
		{full: "@'notes.txt'", base: "@'notes.txt'", start: 0, end: 0},
		{full: "@file:internal/daemon/app.go#L42", base: "@file:internal/daemon/app.go", start: 42, end: 0},
		{full: "@rule:go-verification-gate", base: "@rule:go-verification-gate", start: 0, end: 0},
		{full: "@skill:release-runbook", base: "@skill:release-runbook", start: 0, end: 0},
		{full: "@mcp:server/resource", base: "@mcp:server/resource", start: 0, end: 0},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %+v", len(expected), len(tokens), tokens)
	}

	for i, exp := range expected {
		tok := tokens[i]
		if tok.FullToken != exp.full || tok.BaseToken != exp.base || tok.StartLine != exp.start || tok.EndLine != exp.end {
			t.Errorf("[%d] got %+v, want %+v", i, tok, exp)
		}
	}
}

func TestResolve(t *testing.T) {
	tracked := []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@composer.go", Target: "internal/cli/components/composer.go"},
		{Kind: api.AttachmentFile, Ref: "@cli/app.go", Target: "internal/cli/app.go"},
		{Kind: api.AttachmentRule, Ref: "@rule:commit-hygiene", Target: "commit-hygiene"},
		{Kind: api.AttachmentMCP, Ref: "@mcp:notes:notes://secret", Target: "notes:notes://secret", AllowProhibited: true},
		{Kind: api.AttachmentFile, Ref: "@deleted.go", Target: "internal/deleted.go"},
	}

	text := `Compare @composer.go with @composer.go, and move @composer.go#L10-L20 to @composer.go#L40-L50.
Also check @cli/app.go#L5, unbound @docs/, @Override, @mcp:server/res, @mcp:notes:notes://specs/spec-1#42, @mcp:NOTES:notes://secret, explicit @file:internal/daemon/std.go#L10-L30, @rule:go-verification-gate, and @skill:release-runbook.`

	calls := 0
	atts := Resolve(text, tracked, func(cand string) (string, bool) {
		calls++
		if cand == "docs/" {
			return "docs/", true
		}
		return "", false
	})

	expected := []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@composer.go", Target: "internal/cli/components/composer.go"},
		{Kind: api.AttachmentFile, Ref: "@composer.go#L10-L20", Target: "internal/cli/components/composer.go", StartLine: 10, EndLine: 20},
		{Kind: api.AttachmentFile, Ref: "@composer.go#L40-L50", Target: "internal/cli/components/composer.go", StartLine: 40, EndLine: 50},
		{Kind: api.AttachmentFile, Ref: "@cli/app.go#L5", Target: "internal/cli/app.go", StartLine: 5},
		{Kind: api.AttachmentFile, Ref: "@docs/", Target: "docs/"},
		{Kind: api.AttachmentMCP, Ref: "@mcp:notes:notes://specs/spec-1#42", Target: "notes:notes://specs/spec-1#42"},
		{Kind: api.AttachmentMCP, Ref: "@mcp:NOTES:notes://secret", Target: "notes:notes://secret", AllowProhibited: true},
		{Kind: api.AttachmentFile, Ref: "@file:internal/daemon/std.go#L10-L30", Target: "internal/daemon/std.go", StartLine: 10, EndLine: 30},
		{Kind: api.AttachmentRule, Ref: "@rule:go-verification-gate", Target: "go-verification-gate"},
		{Kind: api.AttachmentSkill, Ref: "@skill:release-runbook", Target: "release-runbook"},
	}

	if !reflect.DeepEqual(atts, expected) {
		t.Fatalf("got:\n%+v\nwant:\n%+v", atts, expected)
	}
	if calls != 2 { // "docs/" and "Override" ("@mcp:" is skipped)
		t.Errorf("expected 2 lookup calls, got %d", calls)
	}
}

func TestResolveEdgeCases(t *testing.T) {
	// 1. Swapped line numbers
	tokens := ScanTokens("Check @file:test.go#L50-L10 and @foo.go#40-5")
	if len(tokens) != 2 || tokens[0].StartLine != 10 || tokens[0].EndLine != 50 || tokens[1].StartLine != 5 || tokens[1].EndLine != 40 {
		t.Fatalf("unexpected swapped line tokens: %+v", tokens)
	}

	// 2. Dot slash root directory mention
	rootRes := Resolve("Check @file:./", nil, nil)
	if len(rootRes) != 1 || rootRes[0].Target != "." {
		t.Errorf("expected target '.' for './', got %+v", rootRes)
	}

	// 3. History recall with previously-reconciled #L range: idempotent re-submit and range edit
	recalled := []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@composer.go#L10-L20", Target: "internal/cli/components/composer.go", StartLine: 10, EndLine: 20},
	}
	resSame := Resolve("Check @composer.go#L10-L20", recalled, nil)
	if len(resSame) != 1 || resSame[0].StartLine != 10 || resSame[0].EndLine != 20 || resSame[0].Target != "internal/cli/components/composer.go" {
		t.Errorf("expected recalled range 10-20 preserved, got %+v", resSame)
	}
	resEdited := Resolve("Check @composer.go#L30-L45", recalled, nil)
	if len(resEdited) != 1 || resEdited[0].Ref != "@composer.go#L30-L45" || resEdited[0].StartLine != 30 || resEdited[0].EndLine != 45 || resEdited[0].Target != "internal/cli/components/composer.go" {
		t.Errorf("expected edited range 30-45 bound to composer.go, got %+v", resEdited)
	}
}
