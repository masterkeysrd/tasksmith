package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/exec"
)

// execOptions mirrors the headless run flags. They are declared per command
// (not on the root) so `tasksmith exec` and the root `--print` shorthand each
// own a clean flag set without colliding with the REPL defaults.
type execOptions struct {
	inputFormat  string
	outputFormat string
	agent        string
	model        string
	effort       string
	mode         string
	sandboxMode  string
	sessionID    string
	continueLast bool
	forkSession  bool
	title        string
	maxTurns     int
	ephemeral    bool
	force        bool
}

func bindExecFlags(cmd *cobra.Command, opts *execOptions) {
	cmd.Flags().StringVar(&opts.inputFormat, "input-format", "text", "stdin mode: \"text\" (piped context) or \"jsonl\" (bidirectional control frames)")
	cmd.Flags().StringVar(&opts.outputFormat, "output-format", "text", "stdout mode: \"text\" (final reply), \"json\" (single result envelope), or \"jsonl\" (NDJSON event stream)")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "Agent name for the session")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model identifier for the session")
	cmd.Flags().StringVar(&opts.effort, "effort", "", "Reasoning effort level for the session (e.g. low, medium, high, default)")
	cmd.Flags().StringVar(&opts.mode, "mode", "", "Permission mode for the session (default, accept_edits, plan, ask, auto)")
	cmd.Flags().StringVar(&opts.sandboxMode, "sandbox", "", "Sandbox isolation backend for new sessions: \"native\" or \"local\"")
	cmd.Flags().StringVar(&opts.sessionID, "session", "", "Existing session ID to resume")
	cmd.Flags().BoolVarP(&opts.continueLast, "continue", "c", false, "Resume the workspace's most recently updated session")
	cmd.Flags().BoolVar(&opts.forkSession, "fork", false, "Branch the resumed session (--session or -c) and run on the fork")
	cmd.Flags().StringVar(&opts.title, "title", "", "Session title when creating a new session")
	cmd.Flags().IntVar(&opts.maxTurns, "max-turns", 0, "Stop the run after this many completed turns (0 = no limit)")
	cmd.Flags().BoolVar(&opts.ephemeral, "ephemeral", false, "Delete the session after the run settles")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Force model switch even if the target model drops input modalities active in the resumed session")
}

func (o *execOptions) toRunOptions(rootOpts *rootOptions, cmd *cobra.Command) (exec.Options, error) {
	wsDir, err := rootOpts.resolveWorkspace()
	if err != nil {
		return exec.Options{}, err
	}
	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return exec.Options{}, err
	}

	inputFormat, err := exec.ParseInputFormat(o.inputFormat)
	if err != nil {
		return exec.Options{}, err
	}
	outputFormat, err := exec.ParseOutputFormat(o.outputFormat)
	if err != nil {
		return exec.Options{}, err
	}

	mode := api.PermissionMode(strings.ToLower(strings.TrimSpace(o.mode)))
	if mode != "" && !mode.IsValid() {
		return exec.Options{}, fmt.Errorf("invalid --mode %q (want default, accept_edits, plan, ask, or auto)", o.mode)
	}
	sandbox := api.SandboxMode(strings.ToLower(strings.TrimSpace(o.sandboxMode)))
	if sandbox != "" && !sandbox.IsValid() {
		return exec.Options{}, fmt.Errorf("invalid --sandbox %q (want %q or %q)", o.sandboxMode, api.SandboxNative, api.SandboxLocal)
	}

	var thinking *api.ThinkingSetting
	if effort := strings.ToLower(strings.TrimSpace(o.effort)); effort != "" {
		if effort == "default" {
			thinking = &api.ThinkingSetting{Type: "default"}
		} else {
			thinking = &api.ThinkingSetting{Type: "effort", Effort: effort}
		}
	}

	opts := exec.Options{
		Socket:       sockPath,
		Workspace:    wsDir,
		InputFormat:  inputFormat,
		OutputFormat: outputFormat,
		SessionID:    strings.TrimSpace(o.sessionID),
		Continue:     o.continueLast,
		Fork:         o.forkSession,
		Title:        strings.TrimSpace(o.title),
		MaxTurns:     o.maxTurns,
		Ephemeral:    o.ephemeral,
		Force:        o.force,
		Config: api.SessionConfig{
			Agent:    strings.TrimSpace(o.agent),
			Model:    strings.TrimSpace(o.model),
			Thinking: thinking,
			Mode:     mode,
			Sandbox:  sandbox,
		},
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
	}
	return opts, nil
}

// composeExecPrompt joins positional args and reads piped stdin. In text-input
// mode an interactive stdin contributes nothing; a pipe becomes context for
// the prompt.
func composeExecPrompt(stdin io.Reader, interactive bool, args []string) (prompt, stdinText string, err error) {
	prompt = strings.TrimSpace(strings.Join(args, " "))
	if interactive || stdin == nil {
		return prompt, "", nil
	}
	data, rerr := io.ReadAll(stdin)
	if rerr != nil {
		return "", "", fmt.Errorf("read stdin: %w", rerr)
	}
	return prompt, string(data), nil
}

func newExecCmd(rootOpts *rootOptions) *cobra.Command {
	opts := &execOptions{}
	cmd := &cobra.Command{
		Use:   "exec [prompt]",
		Short: "Run a single headless task against the Brain Daemon and exit",
		Long: "Run one headless session turn and exit. The prompt may come from\n" +
			"arguments or piped stdin (which becomes context). With --input-format\n" +
			"jsonl the process stays open and drives a bidirectional NDJSON\n" +
			"session: daemon events on stdout, control frames on stdin.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExec(cmd, rootOpts, opts, args)
		},
	}
	bindExecFlags(cmd, opts)
	return cmd
}

// printOptions backs the root `-p/--print` shorthand. The session flags
// (--session, --agent, --model, --title, --sandbox) already live on the root
// command for the REPL and are reused here; only the headless-specific flags
// are added, so the two surfaces never collide.
type printOptions struct {
	active       bool
	inputFormat  string
	outputFormat string
	effort       string
	mode         string
	maxTurns     int
	ephemeral    bool
}

func (p *printOptions) bind(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&p.active, "print", "p", false, "Run the prompt headlessly and print the result (implies a one-shot `exec`)")
	cmd.Flags().StringVar(&p.inputFormat, "input-format", "text", "stdin mode for --print: \"text\" or \"jsonl\"")
	cmd.Flags().StringVar(&p.outputFormat, "output-format", "text", "stdout mode for --print: \"text\", \"json\", or \"jsonl\"")
	cmd.Flags().StringVar(&p.effort, "effort", "", "Reasoning effort level for --print (e.g. low, medium, high, default)")
	cmd.Flags().StringVar(&p.mode, "mode", "", "Permission mode for --print (default, accept_edits, plan, ask, auto)")
	cmd.Flags().IntVar(&p.maxTurns, "max-turns", 0, "Stop the --print run after this many completed turns (0 = no limit)")
	cmd.Flags().BoolVar(&p.ephemeral, "ephemeral", false, "Delete the --print session after the run settles")
}

// runExec adapts the root shorthand onto the exec engine, reusing the shared
// root REPL flags for session selection.
func (p *printOptions) runExec(cmd *cobra.Command, rootOpts *rootOptions, replOpts *replOptions, args []string) error {
	opts := &execOptions{
		inputFormat:  p.inputFormat,
		outputFormat: p.outputFormat,
		agent:        replOpts.agent,
		model:        replOpts.model,
		effort:       p.effort,
		mode:         p.mode,
		sandboxMode:  replOpts.sandboxMode,
		sessionID:    replOpts.sessionID,
		continueLast: replOpts.continueLast,
		forkSession:  replOpts.forkSession,
		title:        replOpts.title,
		maxTurns:     p.maxTurns,
		ephemeral:    p.ephemeral,
	}
	return runExec(cmd, rootOpts, opts, args)
}

// runExec implements both `tasksmith exec` and the root `--print` shorthand.
func runExec(cmd *cobra.Command, rootOpts *rootOptions, opts *execOptions, args []string) error {
	runOpts, err := opts.toRunOptions(rootOpts, cmd)
	if err != nil {
		return &exec.ExitCodeError{Code: exec.ExitUsage, Err: err}
	}

	// In jsonl mode stdin carries control frames and must stay untouched;
	// otherwise a pipe becomes prompt context. An explicitly attached stdin
	// (tests, embedding callers) is always consumed; only an inherited
	// process stdin is probed for TTY-ness.
	var prompt, stdinText string
	if runOpts.InputFormat == exec.InputJSONL {
		prompt = strings.TrimSpace(strings.Join(args, " "))
	} else {
		interactive := cmd.InOrStdin() == io.Reader(os.Stdin) && isatty.IsTerminal(os.Stdin.Fd())
		prompt, stdinText, err = composeExecPrompt(cmd.InOrStdin(), interactive, args)
		if err != nil {
			return &exec.ExitCodeError{Code: exec.ExitUsage, Err: err}
		}
	}
	runOpts.Prompt = prompt
	runOpts.StdinText = stdinText

	return exec.Run(cmd.Context(), runOpts)
}
