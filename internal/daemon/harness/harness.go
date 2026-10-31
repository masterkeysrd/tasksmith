package harness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/masterkeysrd/loom/checkpoint/sqlite"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Sentinel errors returned by the harness package.
var (
	ErrInvalidConfig                = errors.New("harness: invalid configuration")
	ErrEmptyThreadID                = errors.New("harness: thread ID must not be empty")
	ErrNilMessage                   = errors.New("harness: input message must not be nil")
	ErrNoPendingPermissions         = errors.New("harness: no pending permissions for thread")
	ErrPendingPermissionsUnresolved = errors.New("harness: all pending permissions must be resolved before resuming")
	ErrNoPendingQuestions           = errors.New("harness: no pending questions for thread")
	ErrPendingQuestionsUnresolved   = errors.New("harness: all pending questions must be resolved before resuming")
)

// Queue is the consumer interface for the session's execution queue
// accessed by the harness execution graph.
type Queue interface {
	HasItems() bool
	HasLeadingCommand() bool
	PopCommand() (id, command string, ok bool)
	PopMessages() []message.Message
}

// BashRunner executes a shell command inside the session environment.
// Satisfied by *std.Std.
type BashRunner interface {
	Bash(ctx context.Context, req std.BashRequest) (*std.BashResult, error)
}

type CommandCompleted struct {
	ID string
	api.CommandRecord
}

type CompactionCompleted struct {
	ID string
	api.CompactedRecord
}

type Interrupted struct {
	Permissions []api.PermissionRequest
	Questions   []api.QuestionRequest
}

type Completed struct {
	Message message.Message
}

// Input represents the conversational input to [Harness.Stream].
// It can provide either a new user message, resolved permission decisions and question answers,
// or a compaction request.
type Input struct {
	Message   message.Message
	Decisions map[string]api.PermissionDecision
	Answers   map[string]api.QuestionResponse
	Compact   *api.CompactRequest
}

func (input Input) HasResponses() bool {
	return input.Decisions != nil || input.Answers != nil
}

func (input Input) HasInteractiveContent() bool {
	return input.Message != nil || input.HasResponses()
}

func (input Input) Validate() error {
	if input.Compact != nil && input.HasInteractiveContent() {
		return fmt.Errorf("%w: cannot specify both compact and message/responses", ErrInvalidConfig)
	}
	if input.Message != nil && input.HasResponses() {
		return fmt.Errorf("%w: cannot specify both message and responses", ErrInvalidConfig)
	}
	return nil
}

// Config holds the mandatory dependencies required to construct a [Harness].
type Config struct {
	ID          string
	DB          *sql.DB
	Bash        BashRunner
	Spiller     tools.Spiller
	Permissions permissions.SessionManager
	Queue       Queue
	Logger      log.Interface
	// Metrics receives tool call metrics. A nil recorder disables them.
	Metrics metrics.Recorder
}

// Validate checks that all required configuration dependencies are present.
func (c Config) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidConfig)
	}
	if c.DB == nil {
		return fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	if c.Bash == nil {
		return fmt.Errorf("%w: bash runner is required", ErrInvalidConfig)
	}
	if c.Spiller == nil {
		return fmt.Errorf("%w: spiller is required", ErrInvalidConfig)
	}
	if c.Permissions == nil {
		return fmt.Errorf("%w: permissions manager is required", ErrInvalidConfig)
	}
	if c.Queue == nil {
		return fmt.Errorf("%w: queue is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return nil
}

// Harness orchestrates stateful, multi-turn agent conversations and tool
// execution over a compiled Loom workflow graph.
type Harness interface {
	Stream(ctx context.Context, input Input) iter.Seq2[any, error]
	LoadState(ctx context.Context) (State, error)
	Configure(opts NodeOptions) error
}

type harness struct {
	id     string
	logger log.Interface
	graph  *graph.Graph[State]
	nodes  []Node
	model  *llm.Model
	tools  tools.Container
	queue  Queue
	static StaticContext
	limits models.Limits
}

var _ Harness = (*harness)(nil)

// New constructs and compiles a new [Harness] from the provided [Config].
func New(cfg Config) (Harness, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cp, err := sqlite.NewCheckpointer(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("harness: initialize sqlite checkpointer: %w", err)
	}

	logger := cfg.Logger.ForComponent("harness")

	g, nodes, err := buildGraph(GraphConfig{
		Checkpointer: cp,
		Permissions:  cfg.Permissions,
		Queue:        cfg.Queue,
		Bash:         cfg.Bash,
		Spiller:      cfg.Spiller,
		Logger:       logger,
		Metrics:      cfg.Metrics,
	})
	if err != nil {
		return nil, err
	}

	return &harness{
		id:     strings.TrimSpace(cfg.ID),
		logger: logger,
		graph:  g,
		nodes:  nodes,
		queue:  cfg.Queue,
	}, nil
}

// Configure updates the dynamic model, tool container, system prompt, limits, and static context
// across the harness and all workflow graph nodes in place.
func (h *harness) Configure(opts NodeOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	h.model = opts.Model
	h.tools = opts.Tools
	h.static = opts.Static
	h.limits = opts.Limits
	for _, n := range h.nodes {
		n.Configure(opts)
	}
	return nil
}

// Stream executes a conversational turn on the configured session ID, returning an
// iterator that yields incremental events: *message.AssistantChunk, message.Message,
// CommandCompleted, CompactionCompleted, Interrupted, Completed, and api.* events.
func (h *harness) Stream(ctx context.Context, input Input) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if err := input.Validate(); err != nil {
			yield(nil, err)
			return
		}
		state, err := h.LoadState(ctx)
		if err != nil {
			yield(nil, err)
			return
		}

		update, inputMsg, err := h.prepareInput(input, state)
		if err != nil {
			yield(nil, err)
			return
		}
		if inputMsg != nil && !yield(inputMsg, nil) {
			return
		}

		runCtx := scope.With(ctx, scope.Scope{SessionID: h.id})
		stream, err := h.graph.Stream(runCtx, update, &graph.Location{ThreadID: h.id})
		if err != nil {
			yield(nil, err)
			return
		}

		h.consumeGraphStream(stream, yield)
	}
}

func (h *harness) prepareInput(input Input, state State) (graph.Update[State], message.Message, error) {
	if !input.HasInteractiveContent() {
		if len(state.PendingPermissions) > 0 {
			return nil, nil, ErrPendingPermissionsUnresolved
		}
		if len(state.PendingQuestions) > 0 {
			return nil, nil, ErrPendingQuestionsUnresolved
		}
		if input.Compact != nil {
			return prepareCompaction(input.Compact), nil, nil
		}
		if !h.queue.HasItems() {
			return nil, nil, ErrNilMessage
		}
		return graph.Update[State](func(s State) State { return s }), nil, nil
	}
	if input.HasResponses() {
		update, err := prepareResponses(input, state)
		return update, nil, err
	}
	return h.prepareMessage(input.Message, state)
}

func prepareCompaction(input *api.CompactRequest) graph.Update[State] {
	return graph.Update[State](func(s State) State {
		s.PendingCompaction = input
		return s
	})
}

func prepareResponses(input Input, state State) (graph.Update[State], error) {
	if len(state.PendingPermissions) == 0 && len(state.PendingQuestions) == 0 {
		if input.Answers != nil {
			return nil, ErrNoPendingQuestions
		}
		return nil, ErrNoPendingPermissions
	}

	normalized := make(map[string]api.PermissionDecision, len(state.PendingPermissions)*2)
	for _, req := range state.PendingPermissions {
		dec, ok := lookupDecision(input.Decisions, req)
		if !ok {
			return nil, ErrPendingPermissionsUnresolved
		}
		dec.RequestID = req.ID
		dec.ToolCallID = req.ToolCallID
		normalized[req.ID] = dec
		normalized[req.ToolCallID] = dec
	}
	answers := make(map[string]api.QuestionResponse, len(state.PendingQuestions)*2)
	for _, req := range state.PendingQuestions {
		response, ok := lookupQuestionResponse(input.Answers, req)
		if !ok {
			return nil, ErrPendingQuestionsUnresolved
		}
		response = *response.Clone()
		response.RequestID, response.ToolCallID = req.ID, req.ToolCallID
		answers[req.ID] = response
		answers[req.ToolCallID] = response
	}
	return graph.Update[State](func(s State) State {
		s.PermissionDecisions = normalized
		s.QuestionResponses = answers
		return s
	}), nil
}

func (h *harness) prepareMessage(msg message.Message, state State) (graph.Update[State], message.Message, error) {
	in := message.CloneMessage(msg)
	var inputMsg message.Message
	_, _, unfulfilled, _ := state.ActiveToolCalls()
	if len(unfulfilled) == 0 {
		if strings.TrimSpace(in.GetID()) == "" {
			in.SetID(uuid.NewString())
		}
		inputMsg = message.CloneMessage(in)
	}
	return graph.Update[State](func(s State) State {
		return s.AppendMessage(in, MessageUpdateOptions{Static: h.static, Limits: h.limits, SkipContextRecalculation: len(unfulfilled) > 0})
	}), inputMsg, nil
}

func (h *harness) consumeGraphStream(stream iter.Seq2[graph.StreamEvent, error], yield func(any, error) bool) {
	var stopped bool
	stream(func(ev graph.StreamEvent, err error) bool {
		if stopped {
			return false
		}
		if err != nil {
			stopped = true
			yield(nil, err)
			return false
		}

		if ev.Event == graph.EventLLMChunk {
			if ev.Node != "agent" {
				return true
			}
			chunk, ok := ev.Data.(message.AssistantChunk)
			if !ok {
				return true
			}
			chunkCopy := message.CloneAssistantChunk(chunk)
			if !yield(&chunkCopy, nil) {
				stopped = true
				return false
			}
			return true
		}

		switch custom := ev.Data.(type) {
		case message.Message,
			api.CommandStartedEvent,
			CommandCompleted,
			api.TodosUpdatedEvent,
			api.CompactionStartedEvent,
			CompactionCompleted,
			api.CacheBrokenRecord,
			api.ContextUpdatedEvent:
			if !yield(custom, nil) {
				stopped = true
				return false
			}
			return true
		}

		switch ev.Event {
		case graph.EventInterrupted:
			snap, ok := ev.Data.(graph.Snapshot[State])
			if !ok {
				return true
			}
			if !yield(Interrupted{
				Permissions: clonePermissionRequests(snap.State.PendingPermissions),
				Questions:   cloneQuestionRequests(snap.State.PendingQuestions),
			}, nil) {
				stopped = true
				return false
			}

		case graph.EventCompleted:
			snap, ok := ev.Data.(graph.Snapshot[State])
			if !ok || len(snap.State.Messages) == 0 {
				if !yield(Completed{}, nil) {
					stopped = true
					return false
				}
				return true
			}
			lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
			if SystemKindOf(lastMsg) == SystemKindCommand || SystemKindOf(lastMsg) == SystemKindCompaction {
				if !yield(Completed{}, nil) {
					stopped = true
					return false
				}
				return true
			}
			if asst, ok := lastMsg.(*message.Assistant); ok && asst != nil {
				if !yield(Completed{
					Message: message.CloneMessage(asst),
				}, nil) {
					stopped = true
					return false
				}
				return true
			}
			if !yield(Completed{}, nil) {
				stopped = true
				return false
			}
		}
		return true
	})
}

// LoadState retrieves the latest checkpointed [State] for this session.
// If no checkpoint exists, it returns an empty [State] and nil error.
func (h *harness) LoadState(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	snap, err := h.graph.Load(ctx, graph.Location{ThreadID: h.id})
	if err != nil {
		if errors.Is(err, graph.ErrCheckpointNotFound) {
			return State{}, nil
		}
		return State{}, err
	}
	if snap == nil {
		return State{}, nil
	}
	return snap.State.Copy(), nil
}
