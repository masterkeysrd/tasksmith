package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// mcpPromptRefPrefix and mcpResourceRefPrefix are the composer triggers for
// invoking an MCP prompt and attaching an MCP resource. The trigger character
// ("/" or "@") is consumed by the autocomplete engine, so candidate insert text
// starts at the body; parseMCPRef accepts both the full reference and the body.
const (
	// MCPRefPrefix is the slash trigger for invoking an MCP prompt. It is
	// exported so the composer can tell an MCP prompt reference apart from a
	// slash command.
	MCPRefPrefix = "/mcp:"

	mcpPromptRefPrefix   = MCPRefPrefix
	mcpResourceRefPrefix = "@mcp:"
)

// mcpAuthPollInterval and mcpAuthPollTimeout bound the wait for an OAuth
// callback to land after the browser is opened. They are variables so a test can
// exercise the whole flow without waiting out a human-sized budget.
var (
	mcpAuthPollInterval = 500 * time.Millisecond
	mcpAuthPollTimeout  = 90 * time.Second
)

// mcpRestartTimeout bounds a reconnect. It is short because a restart needs no
// human: either the server answers or it does not.
const mcpRestartTimeout = 30 * time.Second

// mcpLaunchAuthURL is the seam through which the authorization page is opened.
var mcpLaunchAuthURL = openMCPAuthURL

// mcpBackground tracks the goroutines MCP actions run on so they can be waited
// on. Nothing in the app depends on this, but a test tears the store down when
// it finishes, and an action still publishing a listing at that point races the
// teardown rather than its own code.
var mcpBackground sync.WaitGroup

// goMCPBackground runs fn on a tracked goroutine.
func goMCPBackground(fn func()) {
	mcpBackground.Go(func() {
		fn()
	})
}

func (r *Registry) registerMCPCommands() {
	r.Register(Command{
		Name:        "mcp",
		Aliases:     []string{"mcps"},
		Usage:       "/mcp [auth <name>|restart <name>]",
		Description: "Browse, inspect, authenticate, and restart MCP servers",
		Handler:     handleMCP,
		Complete:    completeMCPSubcommands,
	})
}

func handleMCP(args []string) error {
	switch {
	case len(args) == 0:
		enqueueMCPBrowser("")
		return nil
	case args[0] == "auth":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("/mcp %s requires a server name (usage: /mcp %s <name>)", args[0], args[0])
		}
		enqueueMCPBrowserAuth(strings.TrimSpace(args[1]))
		return nil
	case args[0] == "restart":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("/mcp %s requires a server name (usage: /mcp %s <name>)", args[0], args[0])
		}
		restartMCPServerFromCommand(strings.TrimSpace(args[1]))
		return nil
	default:
		return fmt.Errorf("unknown /mcp subcommand %q (expected auth or restart, or run /mcp to browse)", args[0])
	}
}

// restartMCPServerFromCommand reconnects a server straight from the composer and
// reports the outcome in the timeline, the way /daemon restart and /workspace
// reload do. A restart is reversible and reveals nothing the user cannot undo, so
// it asks for nothing beyond the command itself.
func restartMCPServerFromCommand(name string) {
	store.AppendCommandOutput("/mcp restart "+name, "Restarting "+name+"…")
	goMCPBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), mcpRestartTimeout)
		defer cancel()
		message, ok := restartMCPServer(ctx, name, nil)
		if ok {
			store.AppendOutput(message)
			return
		}
		store.AppendError("MCP", message)
	})
}

// restartMCPServer reconnects the named server and reports the outcome as a message
// plus whether it was a success. Progress is reported through notice, which may be
// nil. The message always says what happened, including when there was no daemon
// connection to restart through, so a caller logging it never writes a blank line.
func restartMCPServer(ctx context.Context, name string, notice func(string)) (string, bool) {
	if notice == nil {
		notice = func(string) {}
	}
	c := store.Client()
	if c == nil {
		failure := "No daemon connection; cannot restart " + name
		notice(failure)
		return failure, false
	}
	notice("Restarting " + name + "…")
	server, err := c.RestartMCP(ctx, name)
	if err != nil {
		failure := "Failed to restart " + name + ": " + api.CleanMCPError(err.Error())
		notice(failure)
		_, _ = refreshMCPServersSilent(ctx)
		return failure, false
	}
	// The status wording matches the list row, so a completion message and
	// the row the user just watched agree on what state the server is in.
	done := fmt.Sprintf("Restarted %s — %s.", name, mcpStatusLabel(server.Status))
	notice(done)
	_, _ = refreshMCPServersSilent(ctx)
	return done, true
}

// mcpStaticAuthInstruction returns the daemon's actionable credential guidance
// for a server that cannot use an interactive login, or an empty string for any
// other failure. The daemon names the environment variable or credential file to
// supply, which reads as advice rather than the wrapped API error it arrives as.
func mcpStaticAuthInstruction(err error) string {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != api.ErrCodeMCPAuthStatic {
		return ""
	}
	return strings.TrimSpace(apiErr.Message)
}

// mcpAuthOutcome is one report from an interactive authorization in progress.
type mcpAuthOutcome struct {
	// AuthURL is the page the user must open. It is empty until the daemon has
	// published one.
	AuthURL string
	// Message is what the user should read right now: progress while the browser
	// is open, the credential guidance for a server that has no browser flow, or
	// the final result of the attempt.
	Message string
	// Settled reports the flow is over, so nothing further should be waited on.
	Settled bool
	// Failed marks a settled outcome that is a failure rather than a success.
	Failed bool
	// LeavesBrowser reports whether the /mcp browser should close when the flow
	// ends. A server that connected, or that had nothing to authorize, has no
	// reason left to keep the browser open.
	LeavesBrowser bool
}

// mcpStartAuth begins interactive authorization for the named server and reports
// the page to open. The daemon parks the server in auth_required and holds the
// flow open for its own callback, so this returns as soon as there is a URL
// rather than waiting for the user.
func mcpStartAuth(ctx context.Context, name string) mcpAuthOutcome {
	c := store.Client()
	if c == nil {
		return mcpAuthOutcome{
			Message:       "No daemon connection; cannot authorize " + name,
			Settled:       true,
			Failed:        true,
			LeavesBrowser: true,
		}
	}
	auth, err := c.AuthenticateMCP(ctx, name)
	if err != nil {
		_, _ = refreshMCPServersSilent(ctx)
		// A server with static credentials has no browser flow, so the daemon
		// answers with the credential to supply instead of a generic failure.
		if instruction := mcpStaticAuthInstruction(err); instruction != "" {
			return mcpAuthOutcome{Message: instruction, Settled: true, Failed: true}
		}
		return mcpAuthOutcome{
			Message: "Failed to start authorization for " + name + ": " + api.CleanMCPError(err.Error()),
			Settled: true,
			Failed:  true,
		}
	}
	if auth.AuthURL == "" {
		_, _ = refreshMCPServersSilent(ctx)
		return mcpAuthOutcome{
			Message:       fmt.Sprintf("%s needs no interactive authorization — %s.", name, mcpStatusLabel(auth.Status)),
			Settled:       true,
			LeavesBrowser: true,
		}
	}
	if mcpLaunchAuthURL(auth.AuthURL) {
		return mcpAuthOutcome{
			AuthURL: auth.AuthURL,
			Message: "Opened the authorization page for " + name + "; waiting for it to finish…",
		}
	}
	return mcpAuthOutcome{
		AuthURL: auth.AuthURL,
		Message: "Open this URL to authorize " + name + ": " + auth.AuthURL,
	}
}

// mcpCheckAuth re-reads the listing and reports whether the authorization for the
// named server has settled. The daemon flips the server to connected once the
// callback lands and the token exchange finishes, which is what makes polling a
// sound way to detect completion.
func mcpCheckAuth(ctx context.Context, name string) mcpAuthOutcome {
	updated, err := refreshMCPServersSilent(ctx)
	if err != nil {
		return mcpAuthOutcome{Message: "Still waiting for " + name + " authorization: " + err.Error()}
	}
	for _, server := range updated {
		if server.Name != name {
			continue
		}
		switch server.Status {
		case api.MCPStatusConnected:
			return mcpAuthOutcome{
				Message:       "Authentication successful. Connected to " + name + ".",
				Settled:       true,
				LeavesBrowser: true,
			}
		case api.MCPStatusError:
			return mcpAuthOutcome{
				Message: "Authorization for " + name + " failed: " + api.CleanMCPError(server.Error),
				Settled: true,
				Failed:  true,
			}
		}
	}
	return mcpAuthOutcome{}
}

// mcpAuthTimeoutMessage renders the notice shown when the wait budget runs out.
func mcpAuthTimeoutMessage(name string) string {
	return "Timed out waiting for " + name + " authorization."
}

// mcpSubmitAuthCallback hands a redirect URL copied from the user's browser to the
// authorization the daemon still holds open. The flow continues on its own
// afterwards, so the caller keeps waiting for the server to connect.
func mcpSubmitAuthCallback(ctx context.Context, name, callbackURL string) mcpAuthOutcome {
	c := store.Client()
	if c == nil {
		return mcpAuthOutcome{
			Message: "No daemon connection; cannot complete the authorization for " + name,
			Settled: true,
			Failed:  true,
		}
	}
	if err := c.CompleteMCPAuth(ctx, name, api.MCPAuthCallbackRequest{CallbackURL: callbackURL}); err != nil {
		// A structured answer means the daemon understood the paste and refused it,
		// usually because no authorization is open any more. Waiting would only end
		// in a timeout, so the reason is reported now. A transport failure says
		// nothing about the paste, so the flow keeps waiting as it was.
		if _, ok := errors.AsType[*api.APIError](err); ok {
			return mcpAuthOutcome{
				Message: "Could not use that callback URL: " + api.CleanMCPError(err.Error()),
				Settled: true,
				Failed:  true,
			}
		}
		return mcpAuthOutcome{Message: "Could not reach the daemon to submit the callback: " + api.CleanMCPError(err.Error())}
	}
	return mcpAuthOutcome{Message: "Callback received; finishing authorization for " + name + "…"}
}

// mcpTargetKind identifies the kind of item an MCP reference points at.
type mcpTargetKind string

const (
	mcpTargetPrompt   mcpTargetKind = "prompt"
	mcpTargetResource mcpTargetKind = "resource"
)

// mcpTarget identifies one prompt or resource on one server. It is the payload
// shared by the inspector, the completion candidates, and the Option B
// confirmation flow.
type mcpTarget struct {
	server api.MCPServer
	kind   mcpTargetKind
	// Name is the prompt name or resource name.
	name string
	// URI is the resource URI; empty for prompts.
	uri string
	// allowed is the server manifest's policy verdict for this item.
	allowed bool
	// args carries rendered prompt arguments when the target is submitted.
	args map[string]string
}

// ref renders the composer reference for the target, e.g.
// "/mcp:notes:summarize" or "@mcp:notes:notes://specs/spec-1".
func (t mcpTarget) ref() string {
	if t.kind == mcpTargetResource {
		return mcpResourceRefPrefix + t.server.Name + ":" + t.uri
	}
	return mcpPromptRefPrefix + t.server.Name + ":" + t.name
}

// label renders the human-readable item name.
func (t mcpTarget) label() string {
	if t.kind == mcpTargetResource {
		if t.name != "" && t.name != t.uri {
			return t.name
		}
		return t.uri
	}
	return t.name
}

// slotValue encodes the target into a picker option value. The prefix keeps
// prompt and resource items with identical names distinguishable.
func (t mcpTarget) slotValue() string {
	if t.kind == mcpTargetResource {
		return "resource:" + t.server.Name + ":" + t.uri
	}
	return "prompt:" + t.server.Name + ":" + t.name
}

// parseMCPSlotValue decodes a value produced by slotValue.
func parseMCPSlotValue(value string) (serverName string, kind mcpTargetKind, item string, ok bool) {
	kindPart, rest, found := strings.Cut(value, ":")
	if !found {
		return "", "", "", false
	}
	serverName, item, found = strings.Cut(rest, ":")
	if !found || serverName == "" || item == "" {
		return "", "", "", false
	}
	switch kindPart {
	case "prompt":
		return serverName, mcpTargetPrompt, item, true
	case "resource":
		return serverName, mcpTargetResource, item, true
	default:
		return "", "", "", false
	}
}

// parseMCPRef parses the body of a composer reference — with or without its
// trigger character — into a target and reports whether it is well formed.
// The remainder after the reference is returned so a caller can preserve any
// text the user typed after it.
func parseMCPRef(text string) (serverName string, kind mcpTargetKind, item string, remainder string, ok bool) {
	body := text
	switch {
	case strings.HasPrefix(body, mcpPromptRefPrefix):
		kind = mcpTargetPrompt
		body = body[len(mcpPromptRefPrefix):]
	case strings.HasPrefix(body, mcpResourceRefPrefix):
		kind = mcpTargetResource
		body = body[len(mcpResourceRefPrefix):]
	default:
		return "", "", "", "", false
	}
	// A resource URI may itself contain ':' (scheme://path), so only the first
	// colon separates the server name.
	serverName, rest, found := strings.Cut(body, ":")
	if !found || strings.TrimSpace(serverName) == "" {
		return "", "", "", "", false
	}
	item, remainder, _ = strings.Cut(rest, " ")
	if strings.TrimSpace(item) == "" {
		return "", "", "", "", false
	}
	return strings.TrimSpace(serverName), kind, item, remainder, true
}

// mcpTargetFor resolves a parsed reference against the server listing.
func mcpTargetFor(servers []api.MCPServer, serverName string, kind mcpTargetKind, item string) (mcpTarget, bool) {
	for _, server := range servers {
		if !strings.EqualFold(server.Name, serverName) {
			continue
		}
		switch kind {
		case mcpTargetPrompt:
			for _, p := range server.Prompts {
				if p.Name == item {
					return mcpTarget{server: server, kind: kind, name: p.Name, allowed: p.Allowed}, true
				}
			}
		case mcpTargetResource:
			for _, res := range server.Resources {
				if res.URI == item {
					return mcpTarget{server: server, kind: kind, name: res.Name, uri: res.URI, allowed: res.Allowed}, true
				}
			}
		}
		return mcpTarget{}, false
	}
	return mcpTarget{}, false
}

// mcpRefSpan is a half-open byte span locating one MCP reference in a line.
type mcpRefSpan struct {
	start int
	end   int
}

// findMCPRefs returns the byte spans of every MCP reference token in text. A
// reference must begin at a token boundary, which keeps prose that merely
// mentions the prefix from being treated as a reference.
func findMCPRefs(text string) []mcpRefSpan {
	var spans []mcpRefSpan
	for i := 0; i < len(text); i++ {
		if i != 0 && !isMCPSpace(text[i-1]) {
			continue
		}
		prefix := ""
		switch {
		case strings.HasPrefix(text[i:], "/mcp:"):
			prefix = "/mcp:"
		case strings.HasPrefix(text[i:], "@mcp:"):
			prefix = "@mcp:"
		default:
			continue
		}
		end := i + len(prefix)
		for end < len(text) && !isMCPSpace(text[end]) {
			end++
		}
		// A bare trigger with no body is not a reference.
		if end > i+len(prefix) {
			spans = append(spans, mcpRefSpan{start: i, end: end})
			i = end - 1
		}
	}
	return spans
}

func isMCPSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// resolveMCPSubmission resolves the single MCP reference in a submitted line and
// returns the submitted text with that reference removed, so anything the user
// typed alongside it is preserved. It reports whether the line referenced a
// known server, and fails when the reference names an item that server does not
// offer, because silently forwarding a broken reference wastes a turn.
func resolveMCPSubmission(text string) (target mcpTarget, remainder string, referenced bool, err error) {
	spans := findMCPRefs(text)
	if len(spans) == 0 {
		return mcpTarget{}, "", false, nil
	}
	if len(spans) > 1 {
		return mcpTarget{}, "", true, fmt.Errorf("run one MCP reference at a time (found %d in this message)", len(spans))
	}
	var servers []api.MCPServer
	if cached := store.GetApp().MCPServers; cached != nil {
		servers = *cached
	}
	span := spans[0]
	serverName, kind, item, _, ok := parseMCPRef(text[span.start:span.end])
	if !ok {
		return mcpTarget{}, "", false, nil
	}
	serverKnown := false
	for _, server := range servers {
		if strings.EqualFold(server.Name, serverName) {
			serverKnown = true
			break
		}
	}
	// An unknown server means the text is prose rather than a reference, so it is
	// left alone instead of hijacking the message.
	if !serverKnown {
		return mcpTarget{}, "", false, nil
	}
	target, found := mcpTargetFor(servers, serverName, kind, item)
	if !found {
		noun := "prompt"
		if kind == mcpTargetResource {
			noun = "resource"
		}
		return mcpTarget{}, "", true, fmt.Errorf("MCP %s %q was not found on server %q (run /mcp to inspect it)", noun, item, serverName)
	}
	remainder = strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(text[:span.start]),
		strings.TrimSpace(text[span.end:]),
	}, " "))
	return target, remainder, true, nil
}

// SubmitMCPReference routes a submitted composer line containing an MCP
// reference. Allowed items run immediately; prohibited ones present the Option B
// confirmation. It reports whether the line was handled, so the caller only
// forwards an ordinary message when this returns false.
func SubmitMCPReference(text string) bool {
	target, remainder, referenced, err := resolveMCPSubmission(text)
	if err != nil {
		store.AppendError("MCP", err.Error())
		return true
	}
	if !referenced {
		return false
	}
	confirmMCPSubmission(target, remainder)
	return true
}

// mcpProhibitedMessage builds the Option B explanation naming the item, the
// policy section, and the manifest path, mirroring what the daemon reports.
func mcpProhibitedMessage(target mcpTarget) string {
	section := "spec.policies.prompts"
	if target.kind == mcpTargetResource {
		section = "spec.policies.resources"
	}
	path := strings.TrimSpace(target.server.Path)
	if path == "" {
		path = "the server manifest"
	}
	kind := string(target.kind)
	return fmt.Sprintf("MCP %s '%s' is prohibited by %s in %s.", kind, target.label(), section, path)
}

// mcpEnableManifestReason explains why one server's manifest cannot be rewritten
// to permit a prohibited item, or returns an empty string when it can. Only a
// workspace- or project-scoped manifest with a known path is editable, which is
// why this takes the server rather than the item: the same answer decides whether
// a prohibited prompt, resource, or tool can be enabled.
func mcpEnableManifestReason(server api.MCPServer) string {
	if strings.TrimSpace(server.Path) == "" {
		return "This server has no manifest path to update."
	}
	if scope := strings.ToLower(strings.TrimSpace(server.Scope)); scope != "" && scope != "workspace" && scope != "project" {
		return "Only a workspace-scoped manifest can be edited; this server comes from " + server.Scope + " scope."
	}
	return ""
}

// mcpPromptNeedsArgs reports whether rendering the prompt requires arguments.
func mcpPromptNeedsArgs(target mcpTarget) bool {
	for _, p := range target.server.Prompts {
		if p.Name == target.name {
			return len(p.Arguments) > 0
		}
	}
	return false
}

// mcpPromptArgsTemplate renders the argument skeleton offered as the initial
// value of the prompt-arguments input, e.g. `{"pr": ""}`.
func mcpPromptArgsTemplate(target mcpTarget) string {
	for _, p := range target.server.Prompts {
		if p.Name != target.name || len(p.Arguments) == 0 {
			continue
		}
		entries := make([]string, 0, len(p.Arguments))
		for _, arg := range p.Arguments {
			entries = append(entries, fmt.Sprintf("%q: %q", arg.Name, ""))
		}
		return "{" + strings.Join(entries, ", ") + "}"
	}
	return "{}"
}

// mcpPromptArgsDescription documents the arguments input for prompts with more
// than one argument.
func mcpPromptArgsDescription(target mcpTarget) string {
	for _, p := range target.server.Prompts {
		if p.Name != target.name || len(p.Arguments) == 0 {
			continue
		}
		parts := make([]string, 0, len(p.Arguments))
		for _, arg := range p.Arguments {
			name := arg.Name
			if arg.Required {
				name += " (required)"
			}
			if arg.Description != "" {
				name += " — " + arg.Description
			}
			parts = append(parts, name)
		}
		return "Arguments as a JSON object. " + strings.Join(parts, "; ")
	}
	return "Arguments as a JSON object."
}

// parseMCPPromptArgs accepts either a JSON object or, for a single-argument
// prompt, a bare value that is assigned to that argument.
func parseMCPPromptArgs(target mcpTarget, raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var args map[string]string
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return nil, fmt.Errorf("arguments must be a JSON object of string values: %v", err)
		}
		return args, nil
	}
	for _, p := range target.server.Prompts {
		if p.Name != target.name {
			continue
		}
		if len(p.Arguments) == 1 {
			return map[string]string{p.Arguments[0].Name: trimmed}, nil
		}
	}
	return nil, fmt.Errorf("arguments must be a JSON object of string values")
}

// submitMCPPrompt renders an MCP prompt and submits its messages as the user's
// turn, so the rendered template drives the request instead of being quoted
// into it.
func submitMCPPrompt(target mcpTarget, suffix string) {
	c := store.Client()
	if c == nil {
		store.AppendError("MCP", "No daemon connection; cannot run MCP prompt "+target.ref())
		return
	}
	goMCPBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := c.GetMCPPrompt(ctx, target.server.Name, target.name, api.MCPPromptRequest{
			Args:            target.args,
			AllowProhibited: !target.allowed,
		})
		if err != nil {
			store.AppendError("MCP", "Failed to run "+target.ref()+": "+err.Error())
			return
		}
		var promptBuilder strings.Builder
		for _, msg := range resp.Messages {
			if strings.TrimSpace(msg.Text) == "" {
				continue
			}
			if promptBuilder.Len() > 0 {
				promptBuilder.WriteString("\n\n")
			}
			promptBuilder.WriteString(msg.Text)
		}
		if promptBuilder.Len() == 0 {
			store.AppendError("MCP", "MCP prompt "+target.ref()+" rendered no messages.")
			return
		}
		text := promptBuilder.String()
		if trimmed := strings.TrimSpace(suffix); trimmed != "" {
			text += "\n\n" + trimmed
		}
		store.Send(text)
	})
}

// attachMCPResource reads an MCP resource and submits its content as the user's
// turn alongside any text the user typed after the reference.
func attachMCPResource(target mcpTarget, suffix string) {
	c := store.Client()
	if c == nil {
		store.AppendError("MCP", "No daemon connection; cannot read MCP resource "+target.ref())
		return
	}
	goMCPBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := c.ReadMCPResource(ctx, target.server.Name, api.MCPResourceRequest{
			URI:             target.uri,
			AllowProhibited: !target.allowed,
		})
		if err != nil {
			store.AppendError("MCP", "Failed to read "+target.ref()+": "+err.Error())
			return
		}
		header := fmt.Sprintf("MCP resource %s from server %q", target.uri, target.server.Name)
		if resp.MIMEType != "" {
			header += " (" + resp.MIMEType + ")"
		}
		text := header + ":\n\n" + resp.Text
		if trimmed := strings.TrimSpace(suffix); trimmed != "" {
			text = trimmed + "\n\n" + text
		}
		store.Send(text)
	})
}

// confirmMCPSubmission presents the Option B flow for a prohibited item: cancel
// (default), use once, or enable in the manifest. Allowed items run directly.
func confirmMCPSubmission(target mcpTarget, suffix string) {
	if target.allowed {
		runMCPTarget(target, suffix)
		return
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: mcpOptionBPrompt(target, suffix),
	})
}

func runMCPTarget(target mcpTarget, suffix string) {
	if target.kind == mcpTargetResource {
		attachMCPResource(target, suffix)
		return
	}
	submitMCPPrompt(target, suffix)
}

// enableMCPPolicyAndRun rewrites the server manifest to permit one item, then
// runs the original request.
func enableMCPPolicyAndRun(target mcpTarget, suffix string) {
	c := store.Client()
	if c == nil {
		store.AppendError("MCP", "No daemon connection; cannot update the manifest for "+target.ref())
		return
	}
	goMCPBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		value := target.name
		if target.kind == mcpTargetResource {
			value = target.uri
		}
		kind := api.MCPAllowPolicyKindPrompt
		if target.kind == mcpTargetResource {
			kind = api.MCPAllowPolicyKindResource
		}
		if _, err := c.AllowMCPPolicy(ctx, target.server.Name, api.MCPAllowPolicyRequest{Kind: kind, Value: value}); err != nil {
			store.AppendError("MCP", "Failed to enable "+target.label()+" in "+target.server.Path+": "+err.Error())
			return
		}
		store.AppendCommandOutput("/mcp", fmt.Sprintf("Enabled %s %q in %s.", target.kind, value, target.server.Path))
		// The manifest change also invalidates the cached listing, so the next
		// browse reflects the new policy.
		if servers, err := c.ListMCPs(ctx); err == nil {
			store.SetWorkspaceMCPServers(servers)
		}
		// The item is permitted now, so it runs through the normal allowed path.
		target.allowed = true
		runMCPTarget(target, suffix)
	})
}

// completeMCPSubcommands suggests the /mcp subcommands.
func completeMCPSubcommands(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []autocomplete.Candidate{
		{Value: "auth", Label: "auth", Description: "Authorize an MCP server in the browser"},
		{Value: "restart", Label: "restart", Description: "Reconnect an MCP server and re-discover its tools"},
	}, nil
}

// completeMCPServerNames suggests server names for "/mcp auth <name>" and
// "/mcp restart <name>". The status is part of the description so the user can
// see at a glance whether a server even needs authorizing.
func completeMCPServerNames(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	servers, err := fetchMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]autocomplete.Candidate, 0, len(servers))
	for _, server := range servers {
		description := mcpStatusLabel(server.Status)
		if server.Description != "" {
			description += " · " + server.Description
		}
		candidates = append(candidates, autocomplete.Candidate{
			Value:       server.Name,
			Label:       server.Name,
			Description: description,
		})
	}
	return candidates, nil
}

// mcpServerNameSources supplies name completion for the subcommands' argument,
// which the command's own trigger cannot reach because it ends at the subcommand.
func mcpServerNameSources() []autocomplete.Source {
	var sources []autocomplete.Source
	for _, sub := range []string{"auth", "restart"} {
		sources = append(sources, autocomplete.Source{
			Name:      "commands:mcp-" + sub,
			Trigger:   "/mcp " + sub + " ",
			StartOnly: true,
			Complete:  completeMCPServerNames,
		})
	}
	return sources
}

// fetchMCPServers returns the cached listing when present and otherwise asks
// the daemon, caching whatever comes back.
func fetchMCPServers(ctx context.Context) ([]api.MCPServer, error) {
	if cached := store.GetApp().MCPServers; cached != nil && len(*cached) > 0 {
		return *cached, nil
	}
	c := store.Client()
	if c == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	servers, err := c.ListMCPs(ctx)
	if err != nil {
		return nil, err
	}
	// A cancelled publish only means the cache stays as it was; the listing is
	// still the right answer for the caller that asked.
	_ = publishMCPServers(ctx, servers)
	return servers, nil
}

// refreshMu serializes listing publishes. An action, the browser's poll, and a
// reload can each refresh on its own goroutine, and kite's store notify is not
// safe to enter twice at once; serializing also stops a slow fetch from
// overwriting a newer listing that arrived while it was in flight.
var refreshMu sync.Mutex

// publishMCPServers stores a freshly fetched listing, skipping the write when
// its request was cancelled while in flight so a stale listing cannot replace a
// newer one.
func publishMCPServers(ctx context.Context, servers []api.MCPServer) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	store.SetWorkspaceMCPServers(servers)
	return nil
}

func publishMCPServersSilent(ctx context.Context, servers []api.MCPServer) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	store.SetWorkspaceMCPServersSilent(servers)
	return nil
}

// refreshMCPServers always re-reads the daemon, because a connection state or
// discovery result may have changed since the last listing.
func refreshMCPServers(ctx context.Context) ([]api.MCPServer, error) {
	c := store.Client()
	if c == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	servers, err := c.ListMCPs(ctx)
	if err != nil {
		return nil, err
	}
	if err := publishMCPServers(ctx, servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// refreshMCPServersSilent re-reads the daemon and updates the cached listing
// without emitting automatic timeline warnings, for callers that already report
// their own action outcome in the timeline.
func refreshMCPServersSilent(ctx context.Context) ([]api.MCPServer, error) {
	c := store.Client()
	if c == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	servers, err := c.ListMCPs(ctx)
	if err != nil {
		return nil, err
	}
	if err := publishMCPServersSilent(ctx, servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// completeMCPPrompts suggests "/mcp:<server>:<prompt>" bodies, including
// prohibited prompts labeled so the user can see what a policy blocks.
func completeMCPPrompts(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	servers, err := fetchMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	var candidates []autocomplete.Candidate
	for _, server := range servers {
		for _, prompt := range server.Prompts {
			target := mcpTarget{server: server, kind: mcpTargetPrompt, name: prompt.Name, allowed: prompt.Allowed}
			// Value and InsertText carry the reference body only; the badge is
			// display text, so it belongs in Label alone.
			body := server.Name + ":" + prompt.Name
			label := body
			if !prompt.Allowed {
				label += " [Prohibited]"
			}
			candidates = append(candidates, autocomplete.Candidate{
				Value:       body,
				Label:       label,
				Description: prompt.Description,
				// The "/mcp:" trigger is consumed by the engine, so the inserted
				// text is the reference body only.
				InsertText: body + " ",
				Data:       target,
			})
		}
	}
	return candidates, nil
}

// completeMCPResources suggests "@mcp:<server>:<uri>" bodies, including
// prohibited resources labeled so the user can see what a policy blocks.
func completeMCPResources(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	servers, err := fetchMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	var candidates []autocomplete.Candidate
	for _, server := range servers {
		for _, resource := range server.Resources {
			target := mcpTarget{server: server, kind: mcpTargetResource, name: resource.Name, uri: resource.URI, allowed: resource.Allowed}
			body := server.Name + ":" + resource.URI
			label := body
			if !resource.Allowed {
				label += " [Prohibited]"
			}
			description := resource.Description
			if description == "" {
				description = resource.MIMEType
			}
			candidates = append(candidates, autocomplete.Candidate{
				Value:       body,
				Label:       label,
				Description: description,
				InsertText:  body + " ",
				Data:        target,
			})
		}
	}
	return candidates, nil
}

// openMCPAuthURL launches the system browser for an authorization URL and
// reports whether it succeeded; the caller prints the URL as a fallback.
func openMCPAuthURL(url string) bool {
	if strings.TrimSpace(url) == "" {
		return false
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if _, err := exec.LookPath(name); err != nil {
		return false
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return false
	}
	// The launcher exits on its own; reaping it avoids a zombie process.
	go func() { _ = cmd.Wait() }()
	return true
}

func countAllowedTools(items []api.MCPToolSummary) int {
	n := 0
	for _, item := range items {
		if item.Allowed {
			n++
		}
	}
	return n
}

func countAllowedPrompts(items []api.MCPPromptSummary) int {
	n := 0
	for _, item := range items {
		if item.Allowed {
			n++
		}
	}
	return n
}

func countAllowedResources(items []api.MCPResourceSummary) int {
	n := 0
	for _, item := range items {
		if item.Allowed {
			n++
		}
	}
	return n
}
