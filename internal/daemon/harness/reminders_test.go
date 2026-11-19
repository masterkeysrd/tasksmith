package harness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

func TestReminderItemsFormat(t *testing.T) {
	tests := []struct {
		name string
		item ReminderItem
		want string
	}{
		{
			name: "FileReminder with ref, path, lines, and unescaped Go generics/shell/HTML",
			item: FileReminder{
				Attachment: api.Attachment{
					Ref:       "@main.go#L1-L2",
					Target:    "ignored.go",
					StartLine: 1,
					EndLine:   2,
				},
				Path:    "main.go",
				Content: "func Map[T comparable](xs []T) bool { return len(xs) > 0 && xs[0] != *new(T) } // <a> & \"b\"",
			},
			want: `<context_file ref="@main.go#L1-L2" path="main.go" lines="1-2">` + "\n" +
				`func Map[T comparable](xs []T) bool { return len(xs) > 0 && xs[0] != *new(T) } // <a> & "b"` + "\n" +
				`</context_file>`,
		},
		{
			name: "FileReminder falls back to Target, omits empty ref/lines, preserves empty body",
			item: FileReminder{
				Attachment: api.Attachment{Target: "empty.txt"},
			},
			want: "<context_file path=\"empty.txt\">\n\n</context_file>",
		},
		{
			name: "DirectoryReminder formats files, dirs, and truncation",
			item: DirectoryReminder{
				Attachment: api.Attachment{Ref: "@docs/", Target: "docs/"},
				Entries: []std.ListEntry{
					{Name: "sub", IsDir: true, Size: 0},
					{Name: "readme.md", IsDir: false, Size: 42},
				},
				Truncated: true,
			},
			want: "<context_directory ref=\"@docs/\" path=\"docs/\">\n" +
				"- sub (dir, 0 bytes)\n" +
				"- readme.md (file, 42 bytes)\n" +
				"... (truncated)\n" +
				"</context_directory>",
		},
		{
			name: "DirectoryReminder empty truncated",
			item: DirectoryReminder{
				Path:      ".",
				Truncated: true,
			},
			want: "<context_directory path=\".\">\n... (truncated)\n</context_directory>",
		},
		{
			name: "DirectoryReminder empty non-truncated",
			item: DirectoryReminder{
				Path: "empty_dir",
			},
			want: "<context_directory path=\"empty_dir\">\n\n</context_directory>",
		},
		{
			name: "MediaReminder path attached self-closing with size",
			item: MediaReminder{
				Media: api.Media{MIMEType: "image/png", ByteSize: 12},
				Ref:   "@shot.png",
				Path:  "shot.png",
			},
			want: `<context_media ref="@shot.png" path="shot.png" mime_type="image/png" size="12 B" />`,
		},
		{
			name: "MediaReminder path omitted unsupported hides size",
			item: MediaReminder{
				Media:  api.Media{MIMEType: "audio/wav", ByteSize: 1024},
				Ref:    "@clip.wav",
				Path:   "clip.wav",
				Status: "omitted",
				Reason: UnsupportedMediaReason(media.ModalityAudio, "clip.wav", "google/gemini-2.5"),
			},
			want: `<context_media ref="@clip.wav" path="clip.wav" mime_type="audio/wav" status="omitted">Cannot attach audio "clip.wav": active model "google/gemini-2.5" does not support audio input.</context_media>`,
		},
		{
			name: "MediaReminder path omitted too large without mime_type",
			item: MediaReminder{
				Ref:    "@big.png",
				Path:   "big.png",
				Status: "omitted",
				Reason: "file exceeds media limit",
			},
			want: `<context_media ref="@big.png" path="big.png" status="omitted">file exceeds media limit</context_media>`,
		},
		{
			name: "MediaReminder display name attached self-closing",
			item: MediaReminder{
				Media: api.Media{DisplayName: "pasted.png", MIMEType: "image/png", ByteSize: 2048},
			},
			want: `<context_media name="pasted.png" mime_type="image/png" size="2 KB" />`,
		},
		{
			name: "MediaReminder display name omitted hides size",
			item: MediaReminder{
				Media:  api.Media{DisplayName: "clip.wav", MIMEType: "audio/wav", ByteSize: 512},
				Status: "omitted",
				Reason: UnsupportedMediaReason(media.ModalityAudio, "clip.wav", "text-only"),
			},
			want: `<context_media name="clip.wav" mime_type="audio/wav" status="omitted">Cannot attach audio "clip.wav": active model "text-only" does not support audio input.</context_media>`,
		},
		{
			name: "MCPResourceReminder with unescaped markdown/HTML",
			item: MCPResourceReminder{
				Attachment: api.Attachment{Ref: "@mcp:notes:notes://spec"},
				Server:     "notes",
				URI:        "notes://spec",
				MIMEType:   "text/markdown",
				Content:    "# Spec\nUse `map[string]any` & `a < b > c`.",
			},
			want: "<context_resource ref=\"@mcp:notes:notes://spec\" server=\"notes\" uri=\"notes://spec\" mime_type=\"text/markdown\">\n" +
				"# Spec\nUse `map[string]any` & `a < b > c`.\n" +
				"</context_resource>",
		},
		{
			name: "MCPResourceReminder omits empty ref and mime_type",
			item: MCPResourceReminder{
				Server:  "notes",
				URI:     "notes://raw",
				Content: "plain",
			},
			want: "<context_resource server=\"notes\" uri=\"notes://raw\">\nplain\n</context_resource>",
		},
		{
			name: "RuleReminder with unescaped body and optional path",
			item: RuleReminder{
				Name:    "go-style",
				Path:    ".agents/rules/go-style.rule.md",
				Content: "Prefer `errors.Is(err, target)` && avoid `<nil>`.",
			},
			want: "<rule name=\"go-style\" path=\".agents/rules/go-style.rule.md\">\n" +
				"Prefer `errors.Is(err, target)` && avoid `<nil>`.\n" +
				"</rule>",
		},
		{
			name: "RuleReminder omits empty path",
			item: RuleReminder{
				Name:    "inline-rule",
				Content: "Follow rules.",
			},
			want: "<rule name=\"inline-rule\">\nFollow rules.\n</rule>",
		},
		{
			name: "SkillReminder with unescaped body",
			item: SkillReminder{
				Name:    "release",
				Path:    ".agents/skills/release",
				Content: "Run `go test ./... && echo \"ok\" > /dev/null`.",
			},
			want: "<skill name=\"release\" path=\".agents/skills/release\">\n" +
				"Run `go test ./... && echo \"ok\" > /dev/null`.\n" +
				"</skill>",
		},
		{
			name: "RuleSuggestionsReminder empty returns empty string",
			item: RuleSuggestionsReminder{},
			want: "",
		},
		{
			name: "RuleSuggestionsReminder formats bullets with and without UseWhen",
			item: RuleSuggestionsReminder{
				Rules: []RuleSuggestion{
					{Name: "rule-a", Description: "Desc A", UseWhen: "  editing Go files  "},
					{Name: "rule-b", Description: "Desc B"},
				},
			},
			want: "The following workspace rules may be relevant to your current task. Call load_rule to apply any that match:\n" +
				"- **rule-a**: Desc A (Use when: editing Go files)\n" +
				"- **rule-b**: Desc B",
		},
		{
			name: "ModeTransitionReminder same mode returns empty string",
			item: ModeTransitionReminder{Prev: api.ModeDefault, Next: ""},
			want: "",
		},
		{
			name: "ModeTransitionReminder act-to-act returns empty string",
			item: ModeTransitionReminder{Prev: api.ModeDefault, Next: api.ModeAuto},
			want: "",
		},
		{
			name: "ModeTransitionReminder plan entry includes plansDir",
			item: ModeTransitionReminder{Prev: api.ModeDefault, Next: api.ModePlan, PlansDir: "tmp/plans"},
			want: ModeTransitionReminder{Prev: api.ModeDefault, Next: api.ModePlan, PlansDir: "tmp/plans"}.FormatReminder(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.item.FormatReminder()
			if got != tc.want {
				t.Fatalf("FormatReminder() mismatch:\ngot:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}

	planText := (ModeTransitionReminder{Prev: api.ModeDefault, Next: api.ModePlan, PlansDir: "tmp/plans"}).FormatReminder()
	if !strings.Contains(planText, "**plan** mode") || !strings.Contains(planText, "tmp/plans") {
		t.Fatalf("unexpected plan reminder text: %q", planText)
	}
	askExit := (ModeTransitionReminder{Prev: api.ModeAsk, Next: api.ModeDefault}).FormatReminder()
	if !strings.Contains(askExit, "exited **ask** mode") || !strings.Contains(askExit, "**default** mode") {
		t.Fatalf("unexpected ask exit reminder text: %q", askExit)
	}
}

func TestSystemReminderFormatAndMessage(t *testing.T) {
	t.Run("empty or all-empty items return empty string and nil message", func(t *testing.T) {
		rem := SystemReminder{
			Items: []ReminderItem{
				nil,
				RuleSuggestionsReminder{},
				ModeTransitionReminder{Prev: api.ModeDefault, Next: api.ModeAuto},
			},
			RuleNames:   []string{"ignored"},
			ExtraBlocks: []message.Block{&message.ToolReferenceBlock{ToolName: "helper"}},
		}
		if got := rem.Format(); got != "" {
			t.Fatalf("Format() = %q, want empty", got)
		}
		if msg := rem.Message(); msg != nil {
			t.Fatalf("Message() = %+v, want nil", msg)
		}
		if msg := NewSystemReminder(); msg != nil {
			t.Fatalf("NewSystemReminder() = %+v, want nil", msg)
		}
	})

	t.Run("combines non-empty items, metadata, and extra blocks", func(t *testing.T) {
		extra := &message.ToolReferenceBlock{ToolName: "release_helper"}
		rem := SystemReminder{
			Items: []ReminderItem{
				nil,
				RuleSuggestionsReminder{},
				RuleReminder{Name: "r1", Path: "r1.md", Content: "Rule body <T> & \"q\""},
				FileReminder{Attachment: api.Attachment{Ref: "@a.go"}, Path: "a.go", Content: "1: package a"},
			},
			RuleNames:   []string{"r1"},
			ExtraBlocks: []message.Block{extra},
		}

		wantText := "<system_reminders>\n" +
			"<rule name=\"r1\" path=\"r1.md\">\nRule body <T> & \"q\"\n</rule>\n\n" +
			"<context_file ref=\"@a.go\" path=\"a.go\">\n1: package a\n</context_file>\n" +
			"</system_reminders>"
		if got := rem.Format(); got != wantText {
			t.Fatalf("Format() mismatch:\ngot:\n%s\nwant:\n%s", got, wantText)
		}

		msg := rem.Message()
		if msg == nil {
			t.Fatal("Message() = nil, want *message.User")
		}
		if SystemKindOf(msg) != SystemKindReminder {
			t.Fatalf("SystemKindOf = %q, want %q", SystemKindOf(msg), SystemKindReminder)
		}
		if gotRules, ok := msg.GetMetadata()[MetaRuleNames].([]string); !ok || !reflect.DeepEqual(gotRules, []string{"r1"}) {
			t.Fatalf("MetaRuleNames = %#v, want []string{\"r1\"}", msg.GetMetadata()[MetaRuleNames])
		}
		if len(msg.Content) != 2 {
			t.Fatalf("len(Content) = %d, want 2", len(msg.Content))
		}
		if tb, ok := msg.Content[0].(*message.TextBlock); !ok || tb.Text != wantText {
			t.Fatalf("Content[0] = %#v, want TextBlock(%q)", msg.Content[0], wantText)
		}
		if msg.Content[1] != extra {
			t.Fatalf("Content[1] = %#v, want %#v", msg.Content[1], extra)
		}
	})

	t.Run("omits MetaRuleNames when RuleNames is empty", func(t *testing.T) {
		msg := NewSystemReminder(SkillReminder{Name: "sk", Path: "dir", Content: "body"})
		if msg == nil {
			t.Fatal("NewSystemReminder returned nil")
		}
		if _, exists := msg.GetMetadata()[MetaRuleNames]; exists {
			t.Fatalf("unexpected MetaRuleNames in metadata: %#v", msg.GetMetadata())
		}
	})
}
