package command

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// enqueueMCPBrowser opens the server browser with an optional status line.
func enqueueMCPBrowser(notice string) {
	enqueueMCPBrowserWithStore(notice, nil)
}

// enqueueMCPBrowserAuth opens the browser directly on the interactive
// authorization screen for one server. Naming a server in the composer is already
// the decision to authenticate it, so nothing is asked on the way in.
func enqueueMCPBrowserAuth(name string) {
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return mcpBrowser(mcpBrowserProps{
				slotID:            slotID,
				authStore:         newMCPAuthStore(),
				initialAuthServer: name,
			})
		},
	})
}

func enqueueMCPBrowserWithStore(notice string, noticeStore *kites.Store[string]) {
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return mcpBrowser(mcpBrowserProps{
				slotID:        slotID,
				initialNotice: notice,
				noticeStore:   noticeStore,
			})
		},
	})
}

// mcpScope renders a server's scope badge, falling back to the namespace and
// then to Workspace, where an unscoped project manifest lives.
func mcpScope(server api.MCPServer) string {
	if scope := strings.TrimSpace(server.Scope); scope != "" {
		return "[" + scope + "]"
	}
	if ns := strings.TrimSpace(server.Namespace); ns != "" {
		return "[" + ns + "]"
	}
	return "[Workspace]"
}

// mcpScopeOrder lists scope names in the order the browser groups them, weakest
// override first so the servers closest to the repository appear at the top.
var mcpScopeOrder = []string{"Project", "Workspace", "User", "Builtin"}

// mcpScopeName renders a server's bare scope name, using the namespace and then
// Workspace as fallbacks the way mcpScope does.
func mcpScopeName(server api.MCPServer) string {
	scope := strings.Trim(mcpScope(server), "[]")
	for _, known := range mcpScopeOrder {
		if strings.EqualFold(scope, known) {
			return known
		}
	}
	if scope != "" {
		return scope
	}
	return "Workspace"
}

// mcpScopeRank returns the display rank of a scope name. An unrecognised scope
// sorts after the known tiers but before nothing, so it still groups together.
func mcpScopeRank(scope string) int {
	for i, known := range mcpScopeOrder {
		if scope == known {
			return i
		}
	}
	return len(mcpScopeOrder)
}

// mcpGroupServers returns the servers ordered by scope tier and then by name,
// which is the order the browser lists them in.
func mcpGroupServers(servers []api.MCPServer) []api.MCPServer {
	ordered := slices.Clone(servers)
	slices.SortStableFunc(ordered, func(a, b api.MCPServer) int {
		if rank := mcpScopeRank(mcpScopeName(a)) - mcpScopeRank(mcpScopeName(b)); rank != 0 {
			return rank
		}
		return strings.Compare(a.Name, b.Name)
	})
	return ordered
}

// mcpScopeLocation renders the shortened directory a scope's manifests live in,
// taken from a server's own portable manifest path. It returns "" when the path
// is unknown, so a heading never shows empty parentheses.
func mcpScopeLocation(server api.MCPServer) string {
	manifest := strings.TrimSpace(server.Path)
	if manifest == "" {
		return ""
	}
	dir := path.Dir(filepath.ToSlash(manifest))
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return components.ShortenHomePath(filepath.FromSlash(dir))
}

// mcpScopeHeading renders the muted heading above one scope group, naming the
// directory its manifests live in when it is known.
func mcpScopeHeading(server api.MCPServer) string {
	title := mcpScopeName(server) + " MCPs"
	if location := mcpScopeLocation(server); location != "" {
		return title + " (" + location + ")"
	}
	return title
}

// mcpStatusIcon renders the glyph shown in a list row's cursor gutter.
func mcpStatusIcon(status api.MCPStatus) string {
	switch status {
	case api.MCPStatusConnected:
		return "✔"
	case api.MCPStatusAuthRequired:
		return "⚠"
	case api.MCPStatusError:
		return "✘"
	case api.MCPStatusConnecting:
		return "⟳"
	default:
		return "○"
	}
}

// mcpStatusIconColor maps a connection state to its icon color: connected is
// green, authorization pending is yellow, a failure is red, an in-flight
// connection is cyan, and everything else is muted.
func mcpStatusIconColor(scheme *theme.Scheme, status api.MCPStatus) style.Style {
	switch status {
	case api.MCPStatusConnected:
		return style.S().Foreground(scheme.Color.Text.Success)
	case api.MCPStatusAuthRequired:
		return style.S().Foreground(scheme.Color.Text.Warning)
	case api.MCPStatusError:
		return style.S().Foreground(scheme.Color.Text.Error)
	case api.MCPStatusConnecting:
		return style.S().Foreground(scheme.Color.Text.Info)
	default:
		return style.S().Foreground(scheme.Color.Text.Muted)
	}
}

// mcpCapabilitySummary renders what a connected server offers, e.g.
// "25 tools · 2 prompts", or "connected" when it discovered nothing. Prohibited
// items are called out only when policy blocks some of them, because listing
// "24/25 tools" everywhere would shout about a policy that rarely exists.
func mcpCapabilitySummary(server api.MCPServer) string {
	var parts []string
	if n := len(server.Tools); n > 0 {
		parts = append(parts, mcpCapabilityCount(server.Tools, countAllowedTools(server.Tools), "tool", "tools"))
	}
	if n := len(server.Prompts); n > 0 {
		parts = append(parts, mcpCapabilityCount(server.Prompts, countAllowedPrompts(server.Prompts), "prompt", "prompts"))
	}
	if n := len(server.Resources); n > 0 {
		parts = append(parts, mcpCapabilityCount(server.Resources, countAllowedResources(server.Resources), "resource", "resources"))
	}
	if len(parts) == 0 {
		return "connected"
	}
	return strings.Join(parts, " · ")
}

// mcpCapabilityCount renders one discovered kind's count, appending how many are
// allowed only when the server's policy blocks some of them.
func mcpCapabilityCount[T any](items []T, allowed int, singular, plural string) string {
	noun := plural
	if len(items) == 1 {
		noun = singular
	}
	if allowed < len(items) {
		return fmt.Sprintf("%d %s (%d allowed)", len(items), noun, allowed)
	}
	return fmt.Sprintf("%d %s", len(items), noun)
}

// mcpStatusLabel names a connection state the way the list row does, so notices
// and completions never invent a second vocabulary for the same state.
func mcpStatusLabel(status api.MCPStatus) string {
	switch status {
	case api.MCPStatusConnected:
		return "connected"
	case api.MCPStatusAuthRequired:
		return "needs authentication"
	case api.MCPStatusConnecting:
		return "connecting…"
	case api.MCPStatusError:
		return "failed to connect"
	default:
		return "disconnected"
	}
}

// mcpRowStatus renders the right-hand status column of a list row. An unhealthy
// server explains itself there, because a manifest description would otherwise
// hide that it never connected or is waiting for a login. A connected server
// reports what it offers instead, and a failure names its reason.
func mcpRowStatus(server api.MCPServer, maxWidth int) string {
	switch server.Status {
	case api.MCPStatusConnected:
		return mcpCapabilitySummary(server)
	case api.MCPStatusError:
		reason := strings.TrimSpace(api.CleanMCPError(server.Error))
		if reason == "" {
			return mcpStatusLabel(server.Status)
		}
		text := "failed to connect (" + reason + ")"
		if maxWidth > 0 {
			// Keep the row scannable: the full reason stays in the inspector.
			return format.CompactSingleLine(text, maxWidth)
		}
		return text
	default:
		return mcpStatusLabel(server.Status)
	}
}

// mcpRowStatusColor styles the status column: everything healthy or neutral is
// muted, while an unhealthy state keeps the color of its icon.
func mcpRowStatusColor(scheme *theme.Scheme, status api.MCPStatus) style.Style {
	if status == api.MCPStatusConnected || status == api.MCPStatusDisconnected {
		return style.S().Foreground(scheme.Color.Text.Muted)
	}
	return mcpStatusIconColor(scheme, status)
}

// mcpTransport renders the transport name, or an em dash when unknown.
func mcpTransport(server api.MCPServer) string {
	if transport := strings.TrimSpace(server.Transport); transport != "" {
		return transport
	}
	return "—"
}

// mcpServerName renders the name shown in listings. The display name is shown
// as a separate inspector field, so the list stays sortable by the name the
// subcommands and references use.
func mcpServerName(server api.MCPServer) string {
	return server.Name
}

// cleanMCPError reduces a daemon-reported connection failure to its reason, so a
// row or inspector line shows `401 Unauthorized` rather than the whole chain of
// transport wrappers the MCP SDK nests around it.
func cleanMCPError(errText string) string {
	return api.CleanMCPError(errText)
}

// mcpToolBadge renders a discovered tool's policy verdict and safety posture.
func mcpToolBadge(tool api.MCPToolSummary) string {
	badges := []string{mcpItemBadge(tool.Allowed)}
	if tool.Annotations.IsReadOnly {
		badges = append(badges, "[ReadOnly]")
	}
	if tool.Annotations.IsDangerous {
		badges = append(badges, "[Dangerous]")
	}
	if tool.Annotations.IsOpenWorld {
		badges = append(badges, "[OpenWorld]")
	}
	return strings.Join(badges, " ")
}

// mcpItemBadge renders the policy verdict for a tool, prompt, or resource.
// Prohibited items stay visible so the user can see what a policy blocks.
func mcpItemBadge(allowed bool) string {
	if allowed {
		return "[Allowed]"
	}
	return "[Prohibited]"
}

// mcpItemCell labels an item in the prompts/resources picker. A resource shows
// its name and URI together, because the URI is what the user references and a
// friendly name alone would hide it.
func mcpItemCell(target mcpTarget) string {
	if target.kind == mcpTargetResource && target.name != "" && target.name != target.uri {
		return target.name + " (" + target.uri + ")"
	}
	return target.label()
}

// mcpAllowedColor styles an [Allowed] badge green and a [Prohibited] badge red.
func mcpAllowedColor(scheme *theme.Scheme, allowed bool) style.Style {
	base := style.S().Bold(true)
	if allowed {
		return base.Foreground(scheme.Color.Text.Success)
	}
	return base.Foreground(scheme.Color.Text.Error)
}

// mcpBadgeNode wraps a badge in a clipping flex row so a narrow column cannot
// push the rest of the row off screen.
func mcpBadgeNode(scheme *theme.Scheme, text string, sty style.Style) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, trail.Span(trail.SpanProps{
		Style: sty.WhiteSpace(style.WhiteSpacePre).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, trail.Text(text)))
}

// mcpIconNode renders a row's status glyph at its natural width, since it shares
// the name column with the cursor gutter and the server name.
func mcpIconNode(text string, sty style.Style) trail.Node {
	return trail.Span(trail.SpanProps{
		Style: sty.WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(text))
}

// mcpListColumns sizes the browser's two list columns from the servers actually
// listed. OptionList renders a fixed-width column as width minus its two-cell
// right padding, and the name column also carries the two-cell cursor gutter plus
// a one-cell status glyph and space, so those widths include that overhead. A
// zero or narrow terminal leaves the status column unmeasured (0), which lets the
// status text render unsqueezed.
func mcpListColumns(servers []api.MCPServer, termWidth int) (name, status int) {
	longestName := len("notes")
	for _, server := range servers {
		if n := len(mcpServerName(server)); n > longestName {
			longestName = n
		}
	}
	const (
		cursorGutter = 2
		iconCells    = 2 // status glyph plus the space before the name
		cellPadding  = 2
		minName      = 16
		maxName      = 28
	)
	name = min(max(longestName+cursorGutter+iconCells+cellPadding, minName), maxName)
	if termWidth > name+cellPadding+minStatusColumn {
		status = termWidth - name - cellPadding
	}
	return name, status
}

// minStatusColumn keeps the status column wide enough to say something useful
// before the browser starts squeezing the reason into an ellipsis.
const minStatusColumn = 18

// mcpDetailLine renders one logical inspector line. Each line needs its own
// flex-row box because sibling spans inside a column box collapse onto one row.
func mcpDetailLine(items ...trail.Node) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, items...)
}

// blankRow renders the empty line that separates two blocks of a card.
func blankRow() trail.Node {
	return mcpDetailLine(trail.Span(trail.SpanProps{}, trail.Text("")))
}

// sectionHeading renders the title of a card section, such as `Parameters:`.
func sectionHeading(sty style.Style, text string) trail.Node {
	return mcpDetailLine(trail.Span(trail.SpanProps{Style: sty}, trail.Text(text)))
}

// The overview card lays its values out as an aligned table: a fixed indent,
// then a label column wide enough for the longest label.
const (
	mcpCardIndent     = 2
	mcpCardLabelWidth = 18
)

// mcpCardField is one aligned `Key: Value` row of the server overview card.
type mcpCardField struct {
	Label string
	// Nodes holds the already-styled value, because a status row colors its glyph
	// and words together while a config path keeps its scope badge a different
	// color from the path itself.
	Nodes []trail.Node
	// Text and ValueStyle carry the same value as plain text, which is what a
	// value too long for one line is rendered from so its continuation lines can
	// be styled to match.
	Text       string
	ValueStyle style.Style
}

// mcpCardTextField builds a field whose value is one run of identically styled
// text, which is the common case and the only one that ever needs wrapping.
func mcpCardTextField(label, text string, sty style.Style) mcpCardField {
	return mcpCardField{
		Label: label,
		Nodes: []trail.Node{trail.Span(trail.SpanProps{
			Style: sty.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(text))},
		Text:       text,
		ValueStyle: sty,
	}
}

// mcpCardLabelWidthFor returns the column card values start in: wide enough for the
// longest label plus its colon and two separating spaces, but never narrower than
// minimum so a card whose labels are all short still reads as a table rather than a
// ragged list.
func mcpCardLabelWidthFor(fields []mcpCardField, minimum int) int {
	width := minimum
	for _, field := range fields {
		if n := len(field.Label) + 3; n > width {
			width = n
		}
	}
	return width
}

// mcpWrapText breaks text at width on spaces. A word that cannot fit a line on
// its own is cut mid-word so a long URL or failure reason still reaches the
// terminal instead of being clipped. Continuation lines are indented to line up
// under the value they belong to, and that indent is itself capped so narrow
// terminals still leave room for at least a few characters per line.
func mcpWrapText(text string, width, indent int) []string {
	const minWidth = 12
	if width < minWidth {
		width = minWidth
	}
	if len([]rune(text)) <= width {
		return []string{text}
	}

	pad := []rune(strings.Repeat(" ", min(indent, width/2)))
	var lines []string
	var current []rune
	// empty tracks whether the line being built holds any content yet, since a
	// line that only holds its indent must not be given a word separator.
	empty := true

	flush := func() {
		if len(current) > 0 {
			lines = append(lines, string(current))
		}
		current = nil
		empty = true
	}

	for word := range strings.FieldsSeq(text) {
		runes := []rune(word)
		for len(runes) > 0 {
			room := width - len(current)
			// Start a new line when the current one has no room left for a separator,
			// or when the word would fit on a line of its own and so should move down
			// whole instead of being split across two lines.
			if !empty && (room < 2 || (room-1 < len(runes) && len(runes) <= width-len(pad))) {
				flush()
			}
			if len(current) == 0 && len(lines) > 0 {
				current = append(current, pad...)
			}
			if !empty {
				current = append(current, ' ')
			}
			empty = false

			room = width - len(current)
			if len(runes) > room {
				current = append(current, runes[:room]...)
				runes = runes[room:]
				flush()
				continue
			}
			current = append(current, runes...)
			runes = nil
		}
	}
	flush()
	return lines
}

// mcpOverviewFields returns the card's rows in display order. A field is
// omitted rather than shown as an em dash when it says nothing: a stdio server
// has no URL, a server with no authentication has no auth row, and a server that
// connected cleanly has no error row.
func mcpOverviewFields(scheme *theme.Scheme, server api.MCPServer) []mcpCardField {
	metaStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	scopeStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)

	fields := []mcpCardField{{
		Label: "Status",
		Nodes: []trail.Node{
			trail.Span(trail.SpanProps{
				Style: mcpStatusIconColor(scheme, server.Status).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(mcpStatusIcon(server.Status)+" "+mcpStatusLabel(server.Status))),
		},
		Text:       mcpStatusIcon(server.Status) + " " + mcpStatusLabel(server.Status),
		ValueStyle: mcpStatusIconColor(scheme, server.Status),
	}}

	if authType := strings.TrimSpace(server.AuthType); authType != "" || server.Status == api.MCPStatusAuthRequired {
		fields = append(fields, mcpOverviewAuthField(scheme, server, authType))
	}
	if len(server.Command) > 0 {
		fields = append(fields, mcpCardTextField("Command", strings.Join(server.Command, " "), metaStyle))
	}
	if endpoint := strings.TrimSpace(server.Endpoint); endpoint != "" {
		fields = append(fields, mcpCardTextField("URL", endpoint, metaStyle))
	}

	fields = append(fields, mcpCardTextField("Transport", mcpTransport(server), metaStyle))

	if config := strings.TrimSpace(server.Path); config != "" {
		location := components.ShortenHomePath(filepath.FromSlash(filepath.ToSlash(config)))
		fields = append(fields, mcpCardField{
			Label: "Config location",
			Nodes: []trail.Node{
				trail.Span(trail.SpanProps{
					Style: metaStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text(location)),
				trail.Span(trail.SpanProps{Style: scopeStyle}, trail.Text(" "+mcpScope(server))),
			},
			Text:       location + " " + mcpScope(server),
			ValueStyle: metaStyle,
		})
	}

	fields = append(fields, mcpCardTextField("Capabilities", mcpCapabilityKinds(server), metaStyle))
	if n := len(server.Tools); n > 0 {
		fields = append(fields, mcpCardTextField("Tools", mcpCountSummary(n, countAllowedTools(server.Tools), "tool", "tools"), metaStyle))
	}
	if n := len(server.Prompts); n > 0 {
		fields = append(fields, mcpCardTextField("Prompts", mcpCountSummary(n, countAllowedPrompts(server.Prompts), "prompt", "prompts"), metaStyle))
	}
	if n := len(server.Resources); n > 0 {
		fields = append(fields, mcpCardTextField("Resources", mcpCountSummary(n, countAllowedResources(server.Resources), "resource", "resources"), metaStyle))
	}
	if server.AuthURL != "" {
		fields = append(fields, mcpCardTextField("Authorize at", server.AuthURL, metaStyle))
	}
	if reason := strings.TrimSpace(cleanMCPError(server.Error)); reason != "" {
		fields = append(fields, mcpCardTextField("Error", reason, mcpStatusIconColor(scheme, server.Status)))
	}
	return fields
}

// mcpOverviewAuthField renders the auth row: an outstanding authorization is
// called out with a warning glyph, a satisfied one with a check, and a static
// scheme just names itself. The caller only asks for this row when there is an
// auth scheme to report or an authorization still outstanding.
func mcpOverviewAuthField(scheme *theme.Scheme, server api.MCPServer, authType string) mcpCardField {
	switch server.Status {
	case api.MCPStatusAuthRequired:
		text := "not authenticated"
		if authType != "" {
			text += " (" + authType + ")"
		}
		return mcpCardTextField("Auth", "✘ "+text, mcpStatusIconColor(scheme, api.MCPStatusAuthRequired))
	case api.MCPStatusConnected:
		return mcpCardTextField("Auth", "✔ authenticated ("+authType+")", mcpStatusIconColor(scheme, api.MCPStatusConnected))
	default:
		return mcpCardTextField("Auth", authType, style.S().Foreground(scheme.Color.Text.Secondary))
	}
}

// mcpCapabilityKinds lists the capability kinds a server actually discovered, so
// a tool-only server does not advertise prompts it has none of.
func mcpCapabilityKinds(server api.MCPServer) string {
	var kinds []string
	if len(server.Tools) > 0 {
		kinds = append(kinds, "tools")
	}
	if len(server.Prompts) > 0 {
		kinds = append(kinds, "prompts")
	}
	if len(server.Resources) > 0 {
		kinds = append(kinds, "resources")
	}
	if len(kinds) == 0 {
		return "none"
	}
	return strings.Join(kinds, " · ")
}

// mcpCountSummary renders one discovered kind's total, calling out how many are
// allowed and how many a policy blocks only when a policy blocks some of them.
func mcpCountSummary(total, allowed int, singular, plural string) string {
	noun := plural
	if total == 1 {
		noun = singular
	}
	if allowed == total {
		return fmt.Sprintf("%d %s", total, noun)
	}
	return fmt.Sprintf("%d %s (%d allowed, %d prohibited)", total, noun, allowed, total-allowed)
}

// mcpCardRows renders the overview card: the server title, its description when
// it declares one, and the aligned metadata rows.
func mcpCardRows(scheme *theme.Scheme, server api.MCPServer, termWidth int) []trail.Node {
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	metaStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	mutedStyle := style.S().Foreground(scheme.Color.Text.Muted)

	fields := mcpOverviewFields(scheme, server)
	labelWidth := mcpCardLabelWidthFor(fields, mcpCardLabelWidth)
	valueWidth := termWidth - mcpCardIndent - labelWidth - 1

	rows := []trail.Node{
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{Style: titleStyle},
			trail.Text(mcpServerName(server)+" MCP Server"))),
	}
	if description := strings.TrimSpace(server.Description); description != "" {
		rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
			Style: mutedStyle.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(strings.Repeat(" ", mcpCardIndent)+description))))
	}

	for _, field := range fields {
		rows = append(rows, mcpCardFieldRow(field, labelWidth, valueWidth, metaStyle))
	}
	return rows
}

// mcpCardFieldRow renders one card row. A value that cannot fit the terminal is
// wrapped under its own first line rather than clipped, because the failure
// reason is the one row a user must be able to read in full.
func mcpCardFieldRow(field mcpCardField, labelWidth, valueWidth int, metaStyle style.Style) trail.Node {
	label := fmt.Sprintf("%-*s", labelWidth, field.Label+":")
	lead := trail.Span(trail.SpanProps{
		Style: metaStyle.WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(strings.Repeat(" ", mcpCardIndent)+label))

	// The value's spans sit in the same flex row as the label: nesting them in a
	// box of their own makes that box claim the whole row and pushes the value onto
	// the line below the label.
	lines := mcpWrapText(field.Text, valueWidth, mcpCardIndent+labelWidth)
	if field.Text == "" || len(lines) <= 1 {
		return mcpDetailLine(append([]trail.Node{lead}, field.Nodes...)...)
	}

	rows := []trail.Node{mcpDetailLine(lead, trail.Span(trail.SpanProps{
		Style: field.ValueStyle.WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(lines[0])))}
	for _, line := range lines[1:] {
		rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
			Style: field.ValueStyle.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(line))))
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, rows...)
}

// mcpMenuAction is what selecting a menu entry does.
type mcpMenuAction int

const (
	mcpMenuBack mcpMenuAction = iota
	mcpMenuTools
	mcpMenuPrompts
	mcpMenuResources
	mcpMenuAuth
	mcpMenuReconnect
)

// mcpMenuItem is one numbered entry of the overview's action menu.
type mcpMenuItem struct {
	Label  string
	Action mcpMenuAction
	// Key is the single-letter shortcut that runs the same action from the
	// overview, empty when the entry has none.
	Key string
}

// mcpMenuActions carries the callbacks a menu entry can invoke.
type mcpMenuActions struct {
	onBack      func()
	onTools     func()
	onPrompts   func()
	onResources func()
	onAuth      func()
	onRestart   func()
}

// mcpMenuFor builds the action menu for a server. A server that cannot be used
// yet leads with the thing that would fix it, so the first entry is the answer to
// "why can't I use this server?".
func mcpMenuFor(server api.MCPServer, actions mcpMenuActions) []mcpMenuItem {
	items := []mcpMenuItem{}
	switch server.Status {
	case api.MCPStatusAuthRequired:
		items = append(items, mcpMenuItem{Label: "Authenticate", Action: mcpMenuAuth, Key: "a"})
		items = append(items, mcpMenuItem{Label: "Reconnect", Action: mcpMenuReconnect, Key: "r"})
		items = append(items, mcpMenuItem{Label: "Back to servers", Action: mcpMenuBack})
		return items
	}

	if len(server.Tools) > 0 {
		items = append(items, mcpMenuItem{
			Label:  fmt.Sprintf("View tools (%d)", len(server.Tools)),
			Action: mcpMenuTools,
		})
	}
	if len(server.Prompts) > 0 {
		items = append(items, mcpMenuItem{
			Label:  fmt.Sprintf("Run prompt (%d)", len(server.Prompts)),
			Action: mcpMenuPrompts,
			Key:    "p",
		})
	}
	if len(server.Resources) > 0 {
		items = append(items, mcpMenuItem{
			Label:  fmt.Sprintf("Attach resource (%d)", len(server.Resources)),
			Action: mcpMenuResources,
			Key:    "u",
		})
	}
	if api.MCPSchemeUsesOAuth(server.AuthType) && server.Status != api.MCPStatusAuthRequired {
		items = append(items, mcpMenuItem{Label: "Authenticate", Action: mcpMenuAuth, Key: "a"})
	}
	items = append(items, mcpMenuItem{Label: "Reconnect", Action: mcpMenuReconnect, Key: "r"})
	return items
}

// run dispatches a menu entry to its callback. A missing callback is a no-op
// rather than a crash, because a pane can legitimately omit one.
func (a mcpMenuActions) run(action mcpMenuAction) {
	switch action {
	case mcpMenuBack:
		if a.onBack != nil {
			a.onBack()
		}
	case mcpMenuTools:
		if a.onTools != nil {
			a.onTools()
		}
	case mcpMenuPrompts:
		if a.onPrompts != nil {
			a.onPrompts()
		}
	case mcpMenuResources:
		if a.onResources != nil {
			a.onResources()
		}
	case mcpMenuAuth:
		if a.onAuth != nil {
			a.onAuth()
		}
	case mcpMenuReconnect:
		if a.onRestart != nil {
			a.onRestart()
		}
	}
}

// livePaneProps is embedded in the props of every browser pane that renders the live
// MCP listing. Kitex memoizes a component whose subtree is large enough once its
// props compare equal, and it compares them by reflection at a bounded depth, which
// can judge two genuinely different servers or tools equal. A pane whose whole job is
// to report what the daemon just discovered would then keep painting the snapshot
// taken when it opened, so these panes decline memoization: re-rendering one small
// view is a far cheaper failure than showing a stale policy verdict.
type livePaneProps struct{}

// Equal reports live pane props as never equal, which disables automatic memoization.
func (livePaneProps) Equal(any) bool { return false }

type mcpInspectorProps struct {
	livePaneProps
	server      api.MCPServer
	onBack      func()
	onTools     func()
	onPrompts   func()
	onResources func()
	onAuth      func()
	onRestart   func()
}

// mcpInspector renders one server's metadata and everything discovered on it.
var mcpInspector = trail.FC("MCPServerInspector", func(props mcpInspectorProps) trail.Node {
	scrollPos, setScrollPos := kitex.UseState(0)
	scrollBoxRef := kitex.UseRef[dom.Element](nil)
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	kitex.UseLayoutEffect(func() {
		if el := scrollBoxRef.Current; el != nil {
			el.ScrollTo(0, scrollPos())
		}
	}, []any{scrollPos()})

	// The menu cursor is local to this view, and a refresh that shrinks the menu
	// clamps it rather than letting it point past the last entry.
	cursor, setCursor := kitex.UseState(0)

	actions := mcpMenuActions{
		onBack:      props.onBack,
		onTools:     props.onTools,
		onPrompts:   props.onPrompts,
		onResources: props.onResources,
		onAuth:      props.onAuth,
		onRestart:   props.onRestart,
	}
	items := mcpMenuFor(props.server, actions)

	// A refresh can shrink the menu, for instance when a server loses its prompts,
	// so the cursor is clamped rather than allowed to point past the last entry.
	if cursor() >= len(items) {
		setCursor(len(items) - 1)
	}
	current := max(cursor(), 0)

	selectItem := func(index int) {
		if index < 0 || index >= len(items) {
			return
		}
		setCursor(index)
		actions.run(items[index].Action)
	}

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			actions.run(mcpMenuBack)
		case k.MatchString("up", "ctrl+p"):
			if current > 0 {
				setCursor(current - 1)
			}
		case k.MatchString("down", "ctrl+n"):
			if current < len(items)-1 {
				setCursor(current + 1)
			}
		case k.MatchString("enter"):
			selectItem(current)
		case k.MatchString("pgup", "ctrl+u", "ctrl+b"):
			if cur := scrollPos(); cur > 8 {
				setScrollPos(cur - 8)
			} else {
				setScrollPos(0)
			}
		case k.MatchString("pgdown", "ctrl+d", "ctrl+f"):
			setScrollPos(scrollPos() + 8)
		case k.MatchString("g"):
			setScrollPos(0)
		case k.MatchString("G"):
			setScrollPos(9999)
		}

		// The letter shortcuts stay so a returning user never has to walk the menu
		// to do the two things they do most.
		for i, item := range items {
			if item.Key == "" {
				continue
			}
			if k.MatchString(item.Key) {
				setCursor(i)
				actions.run(item.Action)
				return
			}
		}

		// A digit picks the matching entry, which is the fastest path on a screen
		// whose entries are numbered anyway.
		if digit := mcpMenuDigit(k.Key); digit > 0 {
			selectItem(digit - 1)
		}
	})

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

	children := mcpCardRows(scheme, props.server, winSize.Width)
	children = append(children, mcpMenuRows(scheme, items, current))

	children = append(children, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: mcpInspectorBindings(items, winSize.Width),
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Box(trail.BoxProps{
			Ref: scrollBoxRef,
			Style: style.S().
				Width(style.Percent(100)).
				MaxHeight(style.Cells(mcpInspectorHeight(winSize.Height))).
				OverflowY(style.OverflowAuto).
				ScrollbarY(true),
		}, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn),
		}, children...)),
	)
})

// mcpMenuDigit maps a digit key press to the menu entry it selects, or 0 when the
// key is not a digit. Only the digits that exist on screen mean anything, which
// the caller checks against the menu length.
func mcpMenuDigit(k key.Key) int {
	if k.Mod != 0 || k.Text == "" || len([]rune(k.Text)) != 1 {
		return 0
	}
	digit := []rune(k.Text)[0]
	if digit < '1' || digit > '9' {
		return 0
	}
	return int(digit - '0')
}

// mcpMenuRows renders the numbered action menu with its cursor.
func mcpMenuRows(scheme *theme.Scheme, items []mcpMenuItem, current int) trail.Node {
	rows := []trail.Node{trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, trail.Span(trail.SpanProps{}, trail.Text("")))}

	cursorStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
	labelStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	for i, item := range items {
		marker := "  "
		styleFor := labelStyle
		if i == current {
			marker = "❯ "
			styleFor = cursorStyle
		}
		rows = append(rows, mcpDetailLine(
			trail.Span(trail.SpanProps{
				Style: cursorStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(strings.Repeat(" ", mcpCardIndent)+marker)),
			trail.Span(trail.SpanProps{Style: styleFor},
				trail.Text(fmt.Sprintf("%d. %s", i+1, item.Label))),
		))
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, rows...)
}

// mcpMenuHint names a menu action for the shortcut hint row, where the full menu
// label ("Attach resource (3)") would be too wide to sit beside the other keys.
func mcpMenuHint(action mcpMenuAction) string {
	switch action {
	case mcpMenuTools:
		return "Tools"
	case mcpMenuPrompts:
		return "Prompts"
	case mcpMenuResources:
		return "Resources"
	case mcpMenuAuth:
		return "Auth"
	case mcpMenuReconnect:
		return "Reconnect"
	default:
		return ""
	}
}

// mcpHintRank orders the letter shortcuts by how essential they are to advertise,
// lowest first: the two actions a user reaches for most stay on a narrow terminal
// while the drill-down shortcuts, which the numbered menu lists anyway, go first.
func mcpHintRank(action mcpMenuAction) int {
	switch action {
	case mcpMenuReconnect:
		return 0
	case mcpMenuAuth:
		return 1
	case mcpMenuPrompts:
		return 2
	case mcpMenuResources:
		return 3
	default:
		return 4
	}
}

// mcpInspectorBindings lists only the shortcuts that exist for the current
// server, so the hint row never advertises a key that does nothing. Letter
// shortcuts are dropped when the terminal is too narrow for the whole row; they
// keep working either way, and the numbered menu spells them out.
func mcpInspectorBindings(items []mcpMenuItem, termWidth int) []uicomponents.KeyBinding {
	bindings := []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Run"},
		{Key: "1-9", Label: "Jump"},
	}
	// Letters are collected in menu order so the hint row reads left to right in
	// the same sequence as the menu below it.
	letters := make([]mcpMenuItem, 0, len(items))
	for _, item := range items {
		if item.Key != "" {
			letters = append(letters, item)
		}
	}

	esc := uicomponents.KeyBinding{Key: "esc", Label: "Back"}
	// Esc is the way out of the screen, so it counts against the width before any
	// letter shortcut is dropped.
	row := func(items []mcpMenuItem) []uicomponents.KeyBinding {
		row := slices.Clone(bindings)
		for _, item := range items {
			row = append(row, uicomponents.KeyBinding{Key: item.Key, Label: mcpMenuHint(item.Action)})
		}
		return append(row, esc)
	}
	drop := slices.Clone(letters)
	slices.SortStableFunc(drop, func(a, b mcpMenuItem) int {
		return mcpHintRank(b.Action) - mcpHintRank(a.Action)
	})
	for _, item := range drop {
		if mcpHintWidth(row(letters)) <= termWidth {
			break
		}
		letters = slices.DeleteFunc(letters, func(candidate mcpMenuItem) bool {
			return candidate.Action == item.Action
		})
	}
	return row(letters)
}

// mcpHintWidth measures the row KeyboardHint renders for these bindings: a
// "Keyboard: " prefix, then "key label" pairs separated by two spaces.
func mcpHintWidth(bindings []uicomponents.KeyBinding) int {
	const prefix = len("Keyboard: ")
	width := prefix
	for i, binding := range bindings {
		if i > 0 {
			width += 2
		}
		width += len([]rune(binding.Key))
		if binding.Label != "" {
			width += 1 + len([]rune(binding.Label))
		}
	}
	return width
}

// mcpInspectorHeight sizes the overview's scroll region from the terminal so a
// long failure reason stays readable instead of pushing the menu off screen. An
// unknown terminal height falls back to the roomy default.
func mcpInspectorHeight(termHeight int) int {
	const fallback, minRows, maxRows = 22, 8, 28
	if termHeight <= 0 {
		return fallback
	}
	rows := termHeight - 10
	if rows < minRows {
		return minRows
	}
	if rows > maxRows {
		return maxRows
	}
	return rows
}

// mcpOptionBPrompt renders the three-choice confirmation for a prohibited item.
// The default choice is Cancel, so an accidental Enter changes nothing.
func mcpOptionBPrompt(target mcpTarget, suffix string) components.PickerProps {
	message := mcpProhibitedMessage(target)
	reason := mcpEnableManifestReason(target.server)

	options := []uicomponents.Option{
		{Value: "cancel", Label: "Cancel", Description: "Abort and do nothing (default)", Current: true},
		{Value: "once", Label: "Use once", Description: "Run this single request without editing the manifest"},
	}
	if reason == "" {
		options = append(options, uicomponents.Option{
			Value:       "manifest",
			Label:       "Enable in manifest",
			Description: fmt.Sprintf("Permit %s in %s, refresh the server, then run it", target.label(), target.server.Path),
		})
	}

	props := components.NewPicker("Prohibited MCP item", options, func(opt uicomponents.Option) {
		switch opt.Value {
		case "once":
			runMCPTarget(target, suffix)
		case "manifest":
			enableMCPPolicyAndRun(target, suffix)
		default:
			store.AppendCommandOutput("/mcp", "Cancelled "+target.ref()+".")
		}
	})
	props.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Apply"},
		{Key: "esc", Label: "Cancel"},
	}
	props.OnCancel = func() {
		store.AppendCommandOutput("/mcp", "Cancelled "+target.ref()+".")
	}

	scheme := theme.Get()
	props.Header = trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
	}, trail.Span(trail.SpanProps{
		Style: style.S().Bold(true).Foreground(scheme.Color.Text.Warning),
	}, trail.Text(message)))

	return props
}

type mcpArgsPromptProps struct {
	target   mcpTarget
	onSubmit func(args map[string]string)
	onCancel func()
}

// mcpArgsPrompt collects a prompt's arguments as a JSON object. A
// single-argument prompt accepts a bare value, which is the common case.
var mcpArgsPrompt = trail.FC("mcpArgsPrompt", func(props mcpArgsPromptProps) trail.Node {
	value, setValue := kitex.UseState(mcpPromptArgsTemplate(props.target))
	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{AutoFocus: true, IsActive: true})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			if props.onCancel != nil {
				props.onCancel()
			}
		case k.MatchString("enter"):
			args, err := parseMCPPromptArgs(props.target, value())
			if err != nil {
				store.AppendError("MCP", err.Error())
				return
			}
			if props.onSubmit != nil {
				props.onSubmit(args)
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
		}, trail.Text("Arguments for "+props.target.ref()))),
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text("> ")),
			kitex.Input(kitex.InputProps{
				Ref:   inputRef,
				Value: value(),
				Style: style.S().Flex(1),
				OnChange: func(e event.Event) {
					if ie, ok := e.(*event.InputEvent); ok {
						setValue(ie.Value)
					}
				},
			}),
		),
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted),
		}, trail.Text("  "+mcpPromptArgsDescription(props.target)))),
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
			Bindings: []uicomponents.KeyBinding{
				{Key: "enter", Label: "Run prompt"},
				{Key: "esc", Label: "Cancel"},
			},
		}),
	)
})

type mcpItemPickerProps struct {
	livePaneProps
	server api.MCPServer
	kind   mcpTargetKind
	onBack func()
	// onSelect decides what happens to a chosen item. The browser routes it
	// through its own state machine so a prompt that declares arguments collects
	// them before running.
	onSelect func(target mcpTarget)
}

// mcpItemPicker lists one server's prompts or resources and uses the selected
// one, routing prohibited items through the Option B confirmation. It is
// searchable, which is what makes a server with dozens of items usable.
var mcpItemPicker = trail.FC("mcpItemPicker", func(props mcpItemPickerProps) trail.Node {
	scheme := theme.UseTheme()
	noun := "prompts"
	if props.kind == mcpTargetResource {
		noun = "resources"
	}

	var items []mcpTarget
	descriptions := make(map[string]string)
	switch props.kind {
	case mcpTargetPrompt:
		for _, prompt := range props.server.Prompts {
			items = append(items, mcpTarget{
				server:  props.server,
				kind:    mcpTargetPrompt,
				name:    prompt.Name,
				allowed: prompt.Allowed,
			})
			descriptions["prompt:"+prompt.Name] = prompt.Description
		}
	case mcpTargetResource:
		for _, resource := range props.server.Resources {
			items = append(items, mcpTarget{
				server:  props.server,
				kind:    mcpTargetResource,
				name:    resource.Name,
				uri:     resource.URI,
				allowed: resource.Allowed,
			})
			description := resource.URI
			if resource.Description != "" {
				description = resource.Description + " · " + resource.URI
			}
			descriptions["resource:"+resource.URI] = description
		}
	}

	if len(items) == 0 {
		return mcpEmptyNotice(mcpEmptyNoticeProps{
			title:   props.server.Name + " — " + noun,
			message: "No " + noun + " discovered on this server yet.",
			action:  props.onBack,
		})
	}

	options := make([]uicomponents.Option, len(items))
	for i, target := range items {
		key := string(target.kind) + ":"
		if target.kind == mcpTargetResource {
			key += target.uri
		} else {
			key += target.name
		}
		badge := mcpItemBadge(target.allowed)
		options[i] = uicomponents.Option{
			Value:       target.slotValue(),
			Label:       target.label(),
			Description: descriptions[key],
			Cells:       []string{mcpItemCell(target), badge},
			CellNodes:   []trail.Node{nil, mcpBadgeNode(scheme, badge, mcpAllowedColor(scheme, target.allowed))},
		}
	}

	pickerProps := components.NewPicker(
		fmt.Sprintf("%s — %s (%d)", props.server.Name, noun, len(items)),
		options,
		func(opt uicomponents.Option) {
			serverName, kind, item, ok := parseMCPSlotValue(opt.Value)
			if !ok {
				return
			}
			target, found := mcpTargetFor([]api.MCPServer{props.server}, serverName, kind, item)
			if !found {
				return
			}
			if props.onSelect != nil {
				props.onSelect(target)
			}
		},
	)
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0, Align: style.TextAlignLeft},
		{Width: len("[Prohibited]") + 3, Align: style.TextAlignLeft},
	}
	pickerProps.Filterable = true
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "type", Label: "Filter"},
		{Key: "enter", Label: "Use item"},
		{Key: "esc", Label: "Back"},
	}
	pickerProps.OnCancel = func() {
		if props.onBack != nil {
			props.onBack()
		}
	}
	return components.Picker(pickerProps)
})

type mcpEmptyNoticeProps struct {
	title   string
	message string
	action  func()
	// onReload optionally offers a workspace reload, which is what a user needs
	// right after adding a manifest that has not been discovered yet.
	onReload func()
}

// mcpEmptyNotice reports an empty list with a way back.
var mcpEmptyNotice = trail.FC("mcpEmptyNotice", func(props mcpEmptyNoticeProps) trail.Node {
	scheme := theme.UseTheme()
	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape") && props.action != nil:
			props.action()
		case k.MatchString("R", "ctrl+r") && props.onReload != nil:
			props.onReload()
		}
	})

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
		}, trail.Text(props.title))),
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text("  "+props.message))),
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
			Bindings: mcpEmptyNoticeBindings(props.onReload != nil),
		}),
	)
})

// mcpEmptyNoticeBindings keeps the hint honest: reload is only advertised where
// it is actually handled.
func mcpEmptyNoticeBindings(canReload bool) []uicomponents.KeyBinding {
	bindings := []uicomponents.KeyBinding{}
	if canReload {
		bindings = append(bindings, uicomponents.KeyBinding{Key: "R / ctrl+r", Label: "Reload workspace"})
	}
	return append(bindings, uicomponents.KeyBinding{Key: "esc", Label: "Back"})
}

type mcpBrowserProps struct {
	slotID        string
	initialNotice string
	noticeStore   *kites.Store[string]
	// authStore carries an interactive authorization in progress. It is supplied
	// when the browser opens straight onto the authorization screen, so the flow
	// outlives the pane showing it.
	authStore *kites.Store[mcpAuthState]
	// initialAuthServer names the server to authorize as soon as the browser opens.
	initialAuthServer string
}

// mcpPane selects which view the browser shows.
type mcpPane int

const (
	mcpPaneServers mcpPane = iota
	mcpPaneInspector
	mcpPaneTools
	mcpPaneToolDetail
	mcpPanePrompts
	mcpPaneResources
	mcpPaneArgs
	mcpPaneAuth
)

type mcpBrowserState struct {
	pane   mcpPane
	server *api.MCPServer
	target mcpTarget
	// tool is the discovered tool the tools detail pane shows.
	tool api.MCPToolSummary
}

var emptyMCPNoticeStore = kites.Create("")

// mcpBrowser renders the interactive MCP server browser inside <Slot />.
var mcpBrowser = trail.FC("MCPBrowser", func(props mcpBrowserProps) trail.Node {
	serversPtr := store.UseApp(func(s store.AppState) *[]api.MCPServer { return s.MCPServers })
	activeNoticeStore := props.noticeStore
	if activeNoticeStore == nil {
		activeNoticeStore = emptyMCPNoticeStore
	}
	sharedNotice := kites.Use(activeNoticeStore, func(s string) string { return s })
	localNotice, setNotice := kitex.UseState(props.initialNotice)
	notice := func() string {
		if n := localNotice(); n != "" {
			return n
		}
		return sharedNotice
	}
	busy, setBusy := kitex.UseState("")
	state, setState := kitex.UseState(mcpBrowserState{})
	// toolNotice reports the outcome of an action taken inside the tool detail view,
	// which has no title of its own to carry it.
	toolNotice, setToolNotice := kitex.UseState("")
	// authCopyNotice confirms a copy pressed inside the authorization screen. It
	// lives here because that screen unmounts the moment the user leaves it.
	authCopyNotice, setAuthCopyNotice := kitex.UseState("")
	// authReported records that a finished authorization already wrote its outcome to
	// the timeline, so no re-render can write it a second time.
	authReported, setAuthReported := kitex.UseState(false)
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	// One authorization flow per browser instance, surviving the pane that shows it
	// so leaving and re-entering the screen does not restart the browser flow.
	authStoreRef := kitex.UseRef[*kites.Store[mcpAuthState]](nil)
	if props.authStore != nil {
		authStoreRef.Current = props.authStore
	} else if authStoreRef.Current == nil {
		authStoreRef.Current = newMCPAuthStore()
	}
	authStore := authStoreRef.Current
	flowRef := kitex.UseRef[*mcpAuthFlow](nil)
	authState := kites.Use(authStore, func(s mcpAuthState) mcpAuthState { return s })

	// A server connects in the background, so the listing goes stale while the
	// browser is open. Fetch on mount, then poll only while a connection is still
	// in progress, which is when the view changes on its own. Client availability is
	// a dependency so the initial fetch retries once a daemon connection exists; a nil
	// deps slice would instead re-fetch on every render.
	clientReady := store.Client() != nil
	kitex.UseEffect(func() {
		if _, err := refreshMCPServers(context.Background()); err != nil {
			setNotice("Failed to load MCP servers: " + err.Error())
		}
	}, []any{clientReady})

	var servers []api.MCPServer
	if serversPtr != nil {
		servers = *serversPtr
	}
	connecting := false
	for _, server := range servers {
		if server.Status == api.MCPStatusConnecting {
			connecting = true
			break
		}
	}
	kitex.UseTimeout(func() {
		_, _ = refreshMCPServers(context.Background())
	}, 2*time.Second, []any{connecting, notice()})

	// runBusy performs one action at a time and reports progress in the title.
	runBusy := func(key string, fn func(ctx context.Context, notice func(string))) {
		if busy() != "" {
			return
		}
		setBusy(key)
		goMCPBackground(func() {
			defer setBusy("")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fn(ctx, setNotice)
		})
	}

	doReload := func() {
		runBusy("workspace", func(ctx context.Context, report func(string)) {
			report("Reloading workspace…")
			if err := store.ReloadWorkspace(ctx); err != nil {
				report("Failed to reload workspace: " + err.Error())
				return
			}
			updated, err := refreshMCPServers(ctx)
			if err != nil {
				report("Workspace reloaded, but the MCP listing failed: " + err.Error())
				return
			}
			report(fmt.Sprintf("Workspace reloaded (%d MCP servers).", len(updated)))
		})
	}

	// doRestart reconnects a server from the browser, showing progress in the
	// subtitle while it runs and then closing the browser and reporting the single
	// outcome in the conversation timeline.
	doRestart := func(name string) {
		runBusy(name, func(ctx context.Context, report func(string)) {
			report("Restarting " + name + "…")
			message, ok := restartMCPServer(ctx, name, nil)
			report("")
			store.RemoveSlot(props.slotID)
			if ok {
				store.AppendCommandOutput("/mcp", message)
				return
			}
			store.AppendError("MCP", message)
		})
	}

	// doAuth opens the authorization screen and starts the browser flow behind it.
	// Unlike a restart, authorization is interactive and can take minutes, so it runs
	// on its own tracked goroutine instead of occupying the browser's single busy
	// slot, which would freeze every other action while the user is at the login page.
	doAuth := func(name string) {
		setNotice("")
		setAuthCopyNotice("")
		setAuthReported(false)
		setState(mcpBrowserState{pane: mcpPaneAuth, server: &api.MCPServer{Name: name}})
		if flowRef.Current != nil {
			flowRef.Current.cancel()
		}
		flowRef.Current = startMCPAuth(name, authStore)
	}

	// leaveAuth stops this client's reporting and returns to wherever the user came
	// from. The daemon keeps holding the callback open, so finishing the login in the
	// browser afterwards still connects the server.
	leaveAuth := func() {
		if flowRef.Current != nil {
			flowRef.Current.cancel()
			flowRef.Current = nil
		}
		setAuthCopyNotice("")
		// /mcp auth <name> opened this screen directly, so going back means leaving
		// the browser too; there is nothing behind it to return to.
		if props.initialAuthServer != "" {
			store.RemoveSlot(props.slotID)
			return
		}
		setState(mcpBrowserState{})
	}

	// An authorization that finishes while the browser is open reports itself once in
	// the timeline and closes the browser slot so the outcome is immediately visible.
	// authReported guards the write so a re-render cannot record the same outcome twice.
	kitex.UseEffect(func() {
		if !authState.settled || authReported() {
			return
		}
		setAuthReported(true)
		mcpAuthTimelineReport(authState)
		store.RemoveSlot(props.slotID)
	}, []any{authState.settled, authState.notice})

	// /mcp auth <name> names the server to authorize, so the browser opens on that
	// flow rather than on a list the user has already decided they did not want.
	kitex.UseEffect(func() {
		if props.initialAuthServer == "" {
			return
		}
		doAuth(props.initialAuthServer)
	}, []any{props.initialAuthServer})

	// enableTool rewrites the server manifest to permit one prohibited tool, then
	// refreshes the listing so the tool's detail view shows the new verdict without
	// the user leaving it. The outcome is reported in that view rather than in the
	// list title, because that is where the key was pressed.
	enableTool := func(server api.MCPServer, tool api.MCPToolSummary) {
		name := mcpToolName(tool)
		runBusy("enable:"+server.Name+":"+name, func(ctx context.Context, _ func(string)) {
			c := store.Client()
			if c == nil {
				setToolNotice("No daemon connection; cannot update " + server.Path)
				return
			}
			if _, err := c.AllowMCPPolicy(ctx, server.Name, api.MCPAllowPolicyRequest{
				Kind:  api.MCPAllowPolicyKindTool,
				Value: name,
			}); err != nil {
				setToolNotice("Failed to enable " + name + " in " + server.Path + ": " + err.Error())
				return
			}
			_, _ = refreshMCPServers(ctx)
			setToolNotice("Enabled " + name + " in " + server.Path + ".")
		})
	}

	// useTarget runs a chosen prompt or resource, collecting arguments first when
	// the prompt declares any. The browser hides the composer while open, so it
	// closes before the turn is submitted.
	useTarget := func(target mcpTarget) {
		if target.kind == mcpTargetPrompt && mcpPromptNeedsArgs(target) {
			server := target.server
			setState(mcpBrowserState{pane: mcpPaneArgs, server: &server, target: target})
			return
		}
		store.RemoveSlot(props.slotID)
		confirmMCPSubmission(target, "")
	}

	current := state()

	// liveServer re-resolves the pane's server by name, because the listing
	// refreshes underneath and a pane must show live discovery, not the snapshot
	// taken when it opened.
	liveServer := func(server *api.MCPServer) api.MCPServer {
		snapshot := *server
		for _, candidate := range servers {
			if candidate.Name == snapshot.Name {
				return candidate
			}
		}
		return snapshot
	}

	switch current.pane {
	case mcpPaneAuth:
		return mcpAuthPane(mcpAuthPaneProps{
			server: mcpAuthServerName(current.server, authState),
			state:  authStore,
			notice: authCopyNotice(),
			onCopy: func() { setAuthCopyNotice(mcpAuthCopiedNotice) },
			onSubmit: func(callbackURL string) {
				setAuthCopyNotice("")
				if flowRef.Current != nil {
					flowRef.Current.submit(context.Background(), callbackURL)
				}
			},
			onBack: leaveAuth,
		})
	case mcpPaneArgs:
		return mcpArgsPrompt(mcpArgsPromptProps{
			target: current.target,
			onSubmit: func(args map[string]string) {
				target := current.target
				target.args = args
				store.RemoveSlot(props.slotID)
				confirmMCPSubmission(target, "")
			},
			onCancel: func() {
				setState(mcpBrowserState{pane: mcpPanePrompts, server: current.server})
			},
		})
	case mcpPanePrompts, mcpPaneResources:
		if current.server == nil {
			break
		}
		kind := mcpTargetPrompt
		if current.pane == mcpPaneResources {
			kind = mcpTargetResource
		}
		return mcpItemPicker(mcpItemPickerProps{
			server: *current.server,
			kind:   kind,
			onBack: func() { setState(mcpBrowserState{pane: mcpPaneInspector, server: current.server}) },
			// Routing through useTarget lets a prompt that declares arguments
			// collect them before it runs.
			onSelect: useTarget,
		})
	case mcpPaneTools:
		if current.server == nil {
			break
		}
		return mcpToolList(mcpToolListProps{
			server: liveServer(current.server),
			onBack: func() { setState(mcpBrowserState{pane: mcpPaneInspector, server: current.server}) },
			onSelect: func(tool api.MCPToolSummary) {
				// A notice left over from another tool must not follow the user in here.
				setToolNotice("")
				setState(mcpBrowserState{pane: mcpPaneToolDetail, server: current.server, tool: tool})
			},
		})
	case mcpPaneToolDetail:
		if current.server == nil {
			break
		}
		live := liveServer(current.server)
		// The listing refreshes underneath, so show the tool's current policy rather
		// than the one read when the pane opened; that is what makes `e` visibly take
		// effect without leaving the view.
		tool := current.tool
		for _, candidate := range live.Tools {
			if candidate.Name == tool.Name {
				tool = candidate
				break
			}
		}
		// A prohibited tool can be permitted only where the manifest can be rewritten,
		// so the shortcut appears exactly when pressing it would do something.
		var onEnable func()
		if !tool.Allowed && mcpEnableManifestReason(live) == "" {
			onEnable = func() { enableTool(live, tool) }
		}
		return mcpToolDetailView(mcpToolDetailProps{
			server:   live,
			tool:     tool,
			notice:   toolNotice(),
			onEnable: onEnable,
			onBack: func() {
				setToolNotice("")
				setState(mcpBrowserState{pane: mcpPaneTools, server: current.server})
			},
		})
	case mcpPaneInspector:
		if current.server == nil {
			break
		}
		server := liveServer(current.server)
		return mcpInspector(mcpInspectorProps{
			server: server,
			onBack: func() {
				setNotice("")
				setState(mcpBrowserState{})
			},
			onTools: func() {
				setNotice("")
				setState(mcpBrowserState{pane: mcpPaneTools, server: current.server})
			},
			onPrompts: func() {
				setNotice("")
				setState(mcpBrowserState{pane: mcpPanePrompts, server: current.server})
			},
			onResources: func() {
				setNotice("")
				setState(mcpBrowserState{pane: mcpPaneResources, server: current.server})
			},
			onAuth:    func() { doAuth(server.Name) },
			onRestart: func() { doRestart(server.Name) },
		})
	}

	if len(servers) == 0 {
		message := "No MCP servers configured. Add one at .agents/mcps/<name>.mcp.yaml."
		if note := notice(); note != "" {
			message = note
		}
		return mcpEmptyNotice(mcpEmptyNoticeProps{
			title:    "Manage MCP servers (0 configured)",
			message:  message,
			action:   func() { store.RemoveSlot(props.slotID) },
			onReload: doReload,
		})
	}

	listed := mcpGroupServers(servers)
	nameCol, statusCol := mcpListColumns(listed, winSize.Width)

	// Each scope group is named once, from the first server listed in it, because
	// the manifest path that reveals where the group lives is per-server.
	headings := make(map[string]string, len(mcpScopeOrder))
	options := make([]uicomponents.Option, len(listed))
	for i, server := range listed {
		scope := mcpScopeName(server)
		if _, seen := headings[scope]; !seen {
			headings[scope] = mcpScopeHeading(server)
		}
		status := mcpRowStatus(server, statusCol)
		options[i] = uicomponents.Option{
			Value:       server.Name,
			Label:       mcpServerName(server),
			Description: status,
			Cells:       []string{mcpServerName(server), status},
			Icon:        mcpIconNode(mcpStatusIcon(server.Status), mcpStatusIconColor(scheme, server.Status)),
			CellNodes: []trail.Node{
				nil,
				mcpBadgeNode(scheme, status, mcpRowStatusColor(scheme, server.Status)),
			},
		}
	}

	inspect := func(opt uicomponents.Option) {
		for i := range listed {
			if listed[i].Name == opt.Value {
				server := listed[i]
				setNotice("")
				setState(mcpBrowserState{pane: mcpPaneInspector, server: &server})
				return
			}
		}
	}

	title := "Manage MCP servers"
	subtitle := mcpServerCountSubtitle(len(listed))
	if note := strings.TrimSpace(notice()); note != "" {
		subtitle = subtitle + " · " + note
	}

	pickerProps := components.NewPicker(title, options, inspect)
	pickerProps.Subtitle = subtitle
	pickerProps.Columns = []uicomponents.Column{
		{Width: nameCol, Align: style.TextAlignLeft},
		{Width: statusCol, Align: style.TextAlignLeft},
	}
	pickerProps.GroupLabel = func(opt uicomponents.Option) string {
		for _, server := range listed {
			if server.Name == opt.Value {
				return headings[mcpScopeName(server)]
			}
		}
		return ""
	}
	// Screen 1 stays unfiltered on purpose: a filter input would swallow the
	// printable keys this screen advertises, and every action is reachable from
	// the server card that Enter opens.
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Open"},
		{Key: "a", Label: "Auth"},
		{Key: "r", Label: "Restart"},
		{Key: "R", Label: "Reload"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"i": inspect,
		"a": func(opt uicomponents.Option) { doAuth(opt.Value) },
		"r": func(opt uicomponents.Option) { doRestart(opt.Value) },
		"R": func(_ uicomponents.Option) { doReload() },
		// A capital R is awkward to reach, so the reload is also available as a
		// chord.
		"ctrl+r": func(_ uicomponents.Option) { doReload() },
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(props.slotID)
		store.AppendCommandOutput("/mcp", "Exited /mcp command")
	}

	return components.Picker(pickerProps)
})

// mcpServerCountSubtitle renders the line under the browser title, singular for
// one server so the count reads as English rather than a debug counter.
func mcpServerCountSubtitle(count int) string {
	if count == 1 {
		return "1 server"
	}
	return fmt.Sprintf("%d servers", count)
}
