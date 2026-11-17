package exec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// localCommands lists the `/slash` names exec resolves against the daemon API
// without a turn. Anything else beginning with "/" is forwarded so the daemon
// (workspace commands, skills, MCP prompts) stays the single source of truth
// for workflow behavior across clients.
var localCommands = []string{
	"model", "models", "mode", "agent", "agents", "effort", "thinking", "budget", "usage", "compact", "exit", "quit", "help",
}

// newMessageID mints the uuid-v7 message id the daemon expects on sends.
func newMessageID() string { return uuid.NewString() }

// reportCommand emits a client-local command outcome through the writer.
func (r *run) reportCommand(command, output string, err error) {
	r.reportCommandData(command, output, nil, err)
}

func (r *run) reportCommandData(command, output string, data any, err error) {
	res := api.ExecCommandResultFrame{Command: command, OK: err == nil, Output: output, Data: data}
	if err != nil {
		res.Error = err.Error()
	}
	if werr := r.w.commandResult(res); werr != nil {
		r.warn(fmt.Sprintf("write command result: %v", werr))
	}
}

// localCommand resolves commands exec owns: session configuration, listings,
// compaction, and exit. It reports whether the text was a local command; a
// non-nil error is a command failure reported by the caller.
func (r *run) localCommand(text string) (bool, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false, nil
	}
	raw, ok := strings.CutPrefix(fields[0], "/")
	if !ok || raw == "" {
		return false, nil
	}
	name := strings.ToLower(raw)
	args := fields[1:]

	switch name {
	case "help":
		return true, r.cmdHelp()
	case "exit", "quit":
		return true, r.cmdExit()
	case "model":
		return true, r.cmdModel(args)
	case "models":
		return true, r.cmdModels()
	case "mode":
		return true, r.cmdMode(args)
	case "agent":
		return true, r.cmdAgent(args)
	case "agents":
		return true, r.cmdAgents()
	case "effort":
		return true, r.cmdEffort(args)
	case "thinking":
		return true, r.cmdThinking(args)
	case "budget":
		return true, r.cmdBudget(args)
	case "usage":
		return true, r.cmdUsage()
	case "compact":
		return true, r.cmdCompact(args)
	default:
		return false, nil
	}
}

func (r *run) cmdHelp() error {
	out := "exec local commands: " + strings.Join(localCommands, ", ") +
		"\nanything else (/name, !cmd, /mcp:<server>:<prompt>) runs on the daemon."
	r.reportCommand("/help", out, nil)
	return nil
}

// cmdExit ends the run: in a bidirectional session pending work drains first;
// in one-shot mode the loop settles with the current status.
func (r *run) cmdExit() error {
	select {
	case r.quit <- struct{}{}:
	default:
	}
	return nil
}

// cmdModel shows or sets the session model, delegating ID and @alias resolution
// to the daemon so every client accepts the exact same model references.
func (r *run) cmdModel(args []string) error {
	if len(args) == 0 {
		label := "(daemon default)"
		if r.sess.Model != nil {
			label = modelLabel(r.sess.Model)
		}
		r.reportCommandData("/model", "model: "+label, r.sess.Model, nil)
		return nil
	}
	id := args[0]
	sess, err := r.c.UpdateSession(r.ctx, r.sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Model: id},
	})
	if err != nil {
		return err
	}
	if sess != nil {
		r.sess = sess
	}
	label := id
	if r.sess.Model != nil {
		label = modelLabel(r.sess.Model)
	}
	r.reportCommandData("/model "+id, "model set to "+label, r.sess.Model, nil)
	return nil
}

// cmdModels lists workspace models with the active one marked.
func (r *run) cmdModels() error {
	models, err := r.c.ListModels(r.ctx)
	if err != nil {
		return err
	}
	var activeID string
	if r.sess.Model != nil {
		activeID = r.sess.Model.ID
	}
	lines := make([]string, 0, len(models))
	for i, m := range models {
		marker := "  "
		if m.ID == activeID {
			marker = "* "
		}
		lines = append(lines, marker+modelLabel(&models[i]))
	}
	r.reportCommandData("/models", strings.Join(lines, "\n"), models, nil)
	return nil
}

// cmdMode shows or sets the permission mode.
func (r *run) cmdMode(args []string) error {
	if len(args) == 0 {
		mode := r.sess.Config.Mode
		if mode == "" {
			mode = api.ModeDefault
		}
		r.reportCommandData("/mode", "mode: "+string(mode), mode, nil)
		return nil
	}
	mode := api.PermissionMode(strings.ToLower(args[0]))
	if !mode.IsValid() {
		return fmt.Errorf("unknown mode %q (want default, accept_edits, plan, ask, or auto)", args[0])
	}
	sess, err := r.c.UpdateSession(r.ctx, r.sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Mode: mode},
	})
	if err != nil {
		return err
	}
	if sess != nil {
		r.sess = sess
	}
	r.reportCommandData("/mode "+string(mode), "mode set to "+string(mode), mode, nil)
	return nil
}

// cmdAgent shows or sets the session agent, delegating validation to the daemon.
func (r *run) cmdAgent(args []string) error {
	if len(args) == 0 {
		agent := r.sess.Config.Agent
		if agent == "" {
			agent = "(workspace default)"
		}
		r.reportCommandData("/agent", "agent: "+agent, r.sess.Config.Agent, nil)
		return nil
	}
	name := args[0]
	sess, err := r.c.UpdateSession(r.ctx, r.sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Agent: name},
	})
	if err != nil {
		return err
	}
	if sess != nil {
		r.sess = sess
	}
	r.reportCommandData("/agent "+name, "agent set to "+name, r.sess.Config.Agent, nil)
	return nil
}

// cmdAgents lists workspace agents.
func (r *run) cmdAgents() error {
	agents, err := r.c.ListAgents(r.ctx)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(agents))
	for _, a := range agents {
		desc := a.Description
		if desc != "" {
			desc = " - " + format.CompactSingleLine(desc, 80)
		}
		lines = append(lines, a.Name+desc)
	}
	r.reportCommandData("/agents", strings.Join(lines, "\n"), agents, nil)
	return nil
}

// cmdEffort shows or sets reasoning effort on the active session.
func (r *run) cmdEffort(args []string) error {
	if len(args) == 0 {
		r.reportCommandData("/effort", "thinking: "+thinkingLabel(r.sess), r.sess.Config.Thinking, nil)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /effort [level|default]")
	}
	val := strings.ToLower(strings.TrimSpace(args[0]))
	setting := api.ThinkingSetting{Type: "effort", Effort: val}
	msg := "thinking effort set to " + val
	if val == "default" {
		setting = api.ThinkingSetting{Type: "default"}
		msg = "thinking reset to default"
	}
	return r.applyThinking("/effort "+val, setting, msg)
}

// cmdThinking shows or sets thinking strategy (on, off, adaptive, default).
func (r *run) cmdThinking(args []string) error {
	if len(args) == 0 {
		r.reportCommandData("/thinking", "thinking: "+thinkingLabel(r.sess), r.sess.Config.Thinking, nil)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /thinking [on|off|adaptive|default]")
	}
	val := strings.ToLower(strings.TrimSpace(args[0]))
	var setting api.ThinkingSetting
	var msg string
	switch val {
	case "on":
		setting = api.ThinkingSetting{Type: "toggle", Enabled: true}
		msg = "thinking enabled"
	case "off":
		setting = api.ThinkingSetting{Type: "toggle", Enabled: false}
		msg = "thinking disabled"
	case "adaptive":
		setting = api.ThinkingSetting{Type: "adaptive"}
		msg = "thinking set to adaptive"
	case "default":
		setting = api.ThinkingSetting{Type: "default"}
		msg = "thinking reset to default"
	default:
		return fmt.Errorf("unknown thinking mode %q (want on, off, adaptive, or default)", args[0])
	}
	return r.applyThinking("/thinking "+val, setting, msg)
}

// cmdBudget shows or sets the thinking token budget.
func (r *run) cmdBudget(args []string) error {
	if len(args) == 0 {
		r.reportCommandData("/budget", "thinking: "+thinkingLabel(r.sess), r.sess.Config.Thinking, nil)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /budget [tokens|default]")
	}
	val := strings.ToLower(strings.TrimSpace(args[0]))
	if val == "default" {
		return r.applyThinking("/budget default", api.ThinkingSetting{Type: "default"}, "thinking reset to default")
	}
	tokens, err := strconv.Atoi(val)
	if err != nil || tokens <= 0 {
		return fmt.Errorf("invalid token budget %q (want positive integer or default)", args[0])
	}
	return r.applyThinking("/budget "+val, api.ThinkingSetting{Type: "budget", Budget: tokens}, fmt.Sprintf("thinking budget set to %d", tokens))
}

func (r *run) applyThinking(cmd string, setting api.ThinkingSetting, summary string) error {
	sess, err := r.c.UpdateSession(r.ctx, r.sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Thinking: &setting},
	})
	if err != nil {
		return err
	}
	if sess != nil {
		r.sess = sess
	}
	r.reportCommandData(cmd, summary, r.sess.Config.Thinking, nil)
	return nil
}

func thinkingLabel(sess *api.Session) string {
	t := sess.Config.Thinking
	defaulted := false
	if t == nil && sess.Model != nil {
		t = sess.Model.ThinkingDefault
		defaulted = true
	}
	if t == nil {
		return "provider default"
	}
	var desc string
	switch t.Type {
	case "effort":
		desc = t.Effort
	case "budget", "budget_tokens":
		desc = fmt.Sprintf("%d tokens", t.Budget)
	case "toggle":
		if t.Enabled {
			desc = "on"
		} else {
			desc = "off"
		}
	case "adaptive":
		desc = "adaptive"
	default:
		desc = t.Type
	}
	if defaulted {
		return "default (" + desc + ")"
	}
	return desc
}

// cmdUsage prints the session usage totals.
func (r *run) cmdUsage() error {
	usage, err := r.c.SessionUsage(r.ctx, r.sess.ID)
	if err != nil {
		return err
	}
	t := usage.Totals
	out := fmt.Sprintf("calls: %d | tokens in/out: %s / %s | cache read/write: %s / %s | cost: %s",
		t.Calls,
		format.Tokens(int(t.Tokens.Input)), format.Tokens(int(t.Tokens.Output)),
		format.Tokens(int(t.Tokens.CacheRead)), format.Tokens(int(t.Tokens.CacheWrite)),
		format.FormatCostNano(t.CostNano, t.UnpricedCalls > 0))
	if t.QuotaCalls > 0 {
		out += fmt.Sprintf(" | quota-billed calls: %d", t.QuotaCalls)
	}
	r.reportCommandData("/usage", out, usage, nil)
	return nil
}

// cmdCompact triggers daemon-side context compaction as a turn.
func (r *run) cmdCompact(args []string) error {
	req := api.SendRequest{Compact: &api.CompactRequest{}}
	if len(args) > 0 {
		req.Compact.Focus = strings.Join(args, " ")
	}
	r.pendingSends++
	if err := r.c.Send(r.ctx, r.sess.ID, req); err != nil {
		r.pendingSends--
		return err
	}
	return nil
}

// modelLabel renders a model's display label.
func modelLabel(m *api.Model) string {
	if m == nil {
		return ""
	}
	if m.Label != "" {
		return m.Label
	}
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}
