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
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config holds the mandatory dependencies required to construct a [Harness].
type Config struct {
	ID           string
	DB           *sql.DB
	Model        *llm.Model
	Tools        tools.Container
	Bash         BashRunner
	Spiller      tools.Spiller
	Permissions  permissions.SessionManager
	SystemPrompt string
	Queue        Queue
	Logger       log.Interface
	Limits       workspace.ModelLimits
	Static       StaticContext
}

// Validate checks that all required configuration dependencies are present.
func (c Config) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidConfig)
	}
	if c.DB == nil {
		return fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	if c.Model == nil {
		return fmt.Errorf("%w: model is required", ErrInvalidConfig)
	}
	if c.Tools == nil {
		return fmt.Errorf("%w: tools container is required", ErrInvalidConfig)
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
	Stream(ctx context.Context, input Input) iter.Seq2[Event, error]
	LoadState(ctx context.Context) (State, error)
}

type harness struct {
	id     string
	logger log.Interface
	graph  *graph.Graph[State]
	queue  Queue
	static StaticContext
	limits workspace.ModelLimits
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

	g, err := buildGraph(GraphConfig{
		Checkpointer: cp,
		Model:        cfg.Model,
		Container:    cfg.Tools,
		Permissions:  cfg.Permissions,
		SystemPrompt: cfg.SystemPrompt,
		Queue:        cfg.Queue,
		Bash:         cfg.Bash,
		Spiller:      cfg.Spiller,
		Limits:       cfg.Limits,
		Static:       cfg.Static,
		Logger:       logger,
	})
	if err != nil {
		return nil, err
	}

	return &harness{
		id:     strings.TrimSpace(cfg.ID),
		logger: logger,
		graph:  g,
		queue:  cfg.Queue,
		static: cfg.Static,
		limits: cfg.Limits,
	}, nil
}

// Stream executes a conversational turn on the configured session ID, returning an
// iterator that yields incremental [EventChunk], [EventMessage], [EventInterrupted],
// and [EventCompleted] notifications.
//
// If input.Decisions is provided, it resumes an interrupted execution awaiting permission
// decisions. If input.Message is provided, it streams a new user message. If both are nil
// and an inbox function is configured, it executes a turn draining pending inbox messages.
func (h *harness) Stream(ctx context.Context, input Input) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(Event{}, err)
			return
		}
		if err := input.Validate(); err != nil {
			yield(Event{}, err)
			return
		}
		state, err := h.LoadState(ctx)
		if err != nil {
			yield(Event{}, err)
			return
		}

		update, event, err := h.prepareInput(input, state)
		if err != nil {
			yield(Event{}, err)
			return
		}
		if event != nil && !yield(*event, nil) {
			return
		}

		stream, err := h.graph.Stream(ctx, update, &graph.Location{ThreadID: h.id})
		if err != nil {
			yield(Event{}, err)
			return
		}

		h.consumeGraphStream(stream, yield)
	}
}

func (h *harness) prepareInput(input Input, state State) (graph.Update[State], *Event, error) {
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
		if _, ok := h.queue.Peek(); !ok {
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

func prepareCompaction(input *CompactInput) graph.Update[State] {
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

	normalized := make(map[string]tools.PermissionDecision, len(state.PendingPermissions)*2)
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
	answers := make(map[string]tools.QuestionResponse, len(state.PendingQuestions)*2)
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

func (h *harness) prepareMessage(msg message.Message, state State) (graph.Update[State], *Event, error) {
	in := message.CloneMessage(msg)
	var event *Event
	_, _, unfulfilled, _ := state.ActiveToolCalls()
	if len(unfulfilled) == 0 {
		if strings.TrimSpace(in.GetID()) == "" {
			in.SetID(uuid.New())
		}
		event = &Event{Type: EventMessage, Message: message.CloneMessage(in)}
	}
	return graph.Update[State](func(s State) State {
		return s.AppendMessage(in, MessageUpdateOptions{Static: h.static, Limits: h.limits, SkipContextRecalculation: len(unfulfilled) > 0})
	}), event, nil
}

func (h *harness) consumeGraphStream(stream iter.Seq2[graph.StreamEvent, error], yield func(Event, error) bool) {
	var stopped bool
	stream(func(ev graph.StreamEvent, err error) bool {
		if stopped {
			return false
		}
		if err != nil {
			stopped = true
			yield(Event{}, err)
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
			if !yield(Event{
				Type:  EventChunk,
				Chunk: &chunkCopy,
			}, nil) {
				stopped = true
				return false
			}
			return true
		}

		if se, ok := ev.Data.(StreamEvent); ok && se != nil {
			if !yield(se.ToEvent(), nil) {
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
			if !yield(Event{
				Type:        EventInterrupted,
				Permissions: clonePermissionRequests(snap.State.PendingPermissions),
				Questions:   cloneQuestionRequests(snap.State.PendingQuestions),
			}, nil) {
				stopped = true
				return false
			}

		case graph.EventCompleted:
			snap, ok := ev.Data.(graph.Snapshot[State])
			if !ok || len(snap.State.Messages) == 0 {
				if !yield(Event{
					Type: EventCompleted,
				}, nil) {
					stopped = true
					return false
				}
				return true
			}
			lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
			if SystemKindOf(lastMsg) == SystemKindCommand || SystemKindOf(lastMsg) == SystemKindCompaction {
				if !yield(Event{
					Type: EventCompleted,
				}, nil) {
					stopped = true
					return false
				}
				return true
			}
			if !yield(Event{
				Type:    EventCompleted,
				Message: message.CloneMessage(lastMsg),
			}, nil) {
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
