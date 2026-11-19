package harness

import (
	"cmp"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

//go:embed prompts/mode_transition.md
var modeTransitionPromptTemplate string

var modeTransitionTemplate = template.Must(template.New("mode_transition").Parse(modeTransitionPromptTemplate))

type tagAttr struct {
	Name  string
	Value string
}

func writeTagAttrs(sb *strings.Builder, attrs []tagAttr) {
	for _, a := range attrs {
		if a.Value == "" {
			continue
		}
		fmt.Fprintf(sb, " %s=%q", a.Name, a.Value)
	}
}

func formatBlockTag(tag string, attrs []tagAttr, body string) string {
	var sb strings.Builder
	sb.WriteByte('<')
	sb.WriteString(tag)
	writeTagAttrs(&sb, attrs)
	sb.WriteString(">\n")
	sb.WriteString(body)
	sb.WriteString("\n</")
	sb.WriteString(tag)
	sb.WriteByte('>')
	return sb.String()
}

func formatInlineTag(tag string, attrs []tagAttr, body string) string {
	var sb strings.Builder
	sb.WriteByte('<')
	sb.WriteString(tag)
	writeTagAttrs(&sb, attrs)
	if body == "" {
		sb.WriteString(" />")
		return sb.String()
	}
	sb.WriteByte('>')
	sb.WriteString(body)
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteByte('>')
	return sb.String()
}

// ReminderItem formats a single section inside a <system_reminders> block.
type ReminderItem interface {
	FormatReminder() string
}

// SystemReminder assembles one or more [ReminderItem] sections and optional
// metadata/tool blocks into a synthetic <system_reminders> user message.
type SystemReminder struct {
	Items       []ReminderItem
	RuleNames   []string
	ExtraBlocks []message.Block
}

// Format renders all non-empty reminder items wrapped in <system_reminders>,
// returning "" when no items produce output.
func (r SystemReminder) Format() string {
	sections := make([]string, 0, len(r.Items))
	for _, item := range r.Items {
		if item == nil {
			continue
		}
		if sec := item.FormatReminder(); sec != "" {
			sections = append(sections, sec)
		}
	}
	if len(sections) == 0 {
		return ""
	}
	return "<system_reminders>\n" + strings.Join(sections, "\n\n") + "\n</system_reminders>"
}

// Message builds a synthetic reminder *message.User, returning nil when Format
// produces no content.
func (r SystemReminder) Message() *message.User {
	text := r.Format()
	if text == "" {
		return nil
	}
	meta := map[string]any{
		MetaSystemKind: SystemKindReminder,
	}
	if len(r.RuleNames) > 0 {
		meta[MetaRuleNames] = r.RuleNames
	}
	content := make(message.Content, 0, 1+len(r.ExtraBlocks))
	content = append(content, &message.TextBlock{Text: text})
	content = append(content, r.ExtraBlocks...)
	return &message.User{
		Base:    message.Base{Metadata: meta},
		Content: content,
	}
}

// NewSystemReminder builds a synthetic reminder *message.User from items,
// returning nil when no items produce output.
func NewSystemReminder(items ...ReminderItem) *message.User {
	return (SystemReminder{Items: items}).Message()
}

// FileReminder renders a <context_file> block for a file or line-slice attachment.
type FileReminder struct {
	api.Attachment
	Path    string
	Content string
}

// FormatReminder formats the file reminder as a <context_file> block tag.
func (f FileReminder) FormatReminder() string {
	var lines string
	if f.StartLine > 0 {
		lines = fmt.Sprintf("%d-%d", f.StartLine, f.EndLine)
	}
	return formatBlockTag("context_file", []tagAttr{
		{"ref", f.Ref},
		{"path", cmp.Or(f.Path, f.Target)},
		{"lines", lines},
	}, f.Content)
}

// DirectoryReminder renders a <context_directory> block for a directory listing.
type DirectoryReminder struct {
	api.Attachment
	Path      string
	Entries   []std.ListEntry
	Truncated bool
}

// FormatReminder formats the directory listing as a <context_directory> block tag.
func (d DirectoryReminder) FormatReminder() string {
	lines := make([]string, 0, len(d.Entries))
	for _, entry := range d.Entries {
		entryType := "file"
		if entry.IsDir {
			entryType = "dir"
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %d bytes)", entry.Name, entryType, entry.Size))
	}
	content := strings.Join(lines, "\n")
	if d.Truncated {
		if content == "" {
			content = "... (truncated)"
		} else {
			content += "\n... (truncated)"
		}
	}
	return formatBlockTag("context_directory", []tagAttr{
		{"ref", d.Ref},
		{"path", cmp.Or(d.Path, d.Target)},
	}, content)
}

// UnsupportedMediaReason returns the standard omission reason for an unsupported
// media modality.
func UnsupportedMediaReason(mod media.Modality, label, modelID string) string {
	return fmt.Sprintf("Cannot attach %s %q: active model %q does not support %s input.", mod, label, modelID, mod)
}

// MediaReminder renders a <context_media> inline tag for an attached or omitted
// media payload.
type MediaReminder struct {
	api.Media
	Ref    string
	Path   string
	Status string
	Reason string
}

// FormatReminder formats the media reminder as a self-closing or inline <context_media> tag.
func (m MediaReminder) FormatReminder() string {
	var size string
	if m.Status == "" {
		size = format.Bytes(m.ByteSize)
	}
	var attrs []tagAttr
	if m.Path != "" {
		attrs = []tagAttr{
			{"ref", m.Ref},
			{"path", m.Path},
			{"mime_type", m.MIMEType},
			{"size", size},
			{"status", m.Status},
		}
	} else {
		attrs = []tagAttr{
			{"name", m.DisplayName},
			{"mime_type", m.MIMEType},
			{"size", size},
			{"status", m.Status},
		}
	}
	return formatInlineTag("context_media", attrs, m.Reason)
}

// MCPResourceReminder renders a <context_resource> block for an MCP resource attachment.
type MCPResourceReminder struct {
	api.Attachment
	Server   string
	URI      string
	MIMEType string
	Content  string
}

// FormatReminder formats the MCP resource as a <context_resource> block tag.
func (r MCPResourceReminder) FormatReminder() string {
	return formatBlockTag("context_resource", []tagAttr{
		{"ref", r.Ref},
		{"server", r.Server},
		{"uri", r.URI},
		{"mime_type", r.MIMEType},
	}, r.Content)
}

// RuleReminder renders a <rule> block for an active or manually attached workspace rule.
type RuleReminder struct {
	Name    string
	Path    string
	Content string
}

// FormatReminder formats the rule as a <rule> block tag.
func (r RuleReminder) FormatReminder() string {
	return formatBlockTag("rule", []tagAttr{
		{"name", r.Name},
		{"path", r.Path},
	}, r.Content)
}

// SkillReminder renders a <skill> block for a manually attached skill.
type SkillReminder struct {
	Name    string
	Path    string
	Content string
}

// FormatReminder formats the skill as a <skill> block tag.
func (s SkillReminder) FormatReminder() string {
	return formatBlockTag("skill", []tagAttr{
		{"name", s.Name},
		{"path", s.Path},
	}, s.Content)
}

// RuleSuggestion describes a single keyword-matched conditional rule suggestion.
type RuleSuggestion struct {
	Name        string
	Description string
	UseWhen     string
}

// RuleSuggestionsReminder formats keyword-matched workspace rule suggestions.
type RuleSuggestionsReminder struct {
	Rules []RuleSuggestion
}

// FormatReminder formats the rule suggestions list, returning "" when empty.
func (r RuleSuggestionsReminder) FormatReminder() string {
	if len(r.Rules) == 0 {
		return ""
	}
	bullets := make([]string, 0, len(r.Rules))
	for _, s := range r.Rules {
		bullet := fmt.Sprintf("- **%s**: %s", s.Name, s.Description)
		if useWhen := strings.TrimSpace(s.UseWhen); useWhen != "" {
			bullet += fmt.Sprintf(" (Use when: %s)", useWhen)
		}
		bullets = append(bullets, bullet)
	}
	return "The following workspace rules may be relevant to your current task. Call load_rule to apply any that match:\n" + strings.Join(bullets, "\n")
}

// ModeTransitionReminder formats a behavioral mode transition prompt when entering
// or leaving non-act modes (plan or ask).
type ModeTransitionReminder struct {
	Prev     api.PermissionMode
	Next     api.PermissionMode
	PlansDir string
}

// FormatReminder renders the mode transition template, returning "" when the
// transition is silent or a no-op.
func (m ModeTransitionReminder) FormatReminder() string {
	next := cmp.Or(m.Next, api.ModeDefault)
	if m.Prev == next {
		return ""
	}
	var buf strings.Builder
	data := struct {
		Prev     string
		Next     string
		PlansDir string
	}{
		Prev:     string(m.Prev),
		Next:     string(next),
		PlansDir: m.PlansDir,
	}
	if err := modeTransitionTemplate.Execute(&buf, data); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}
