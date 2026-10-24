package harness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/checkpoint/sqlite"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Config holds the mandatory dependencies required to construct a [Harness].
type Config struct {
	ID           string
	DB           *sql.DB
	Model        *llm.Model
	Tools        tools.Container
	SystemPrompt string
	Logger       log.Interface
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

	g, err := buildGraph(cp, cfg.Model, cfg.Tools, cfg.SystemPrompt, logger)
	if err != nil {
		return nil, err
	}

	return &harness{
		id:     strings.TrimSpace(cfg.ID),
		logger: logger,
		graph:  g,
	}, nil
}

// Stream executes a conversational turn on the configured session ID, returning an
// iterator that yields incremental [EventChunk], [EventMessage], [EventInterrupted],
// and [EventCompleted] notifications.
//
// If input.Decisions is provided, it resumes an interrupted execution awaiting permission
// decisions. If input.Message is provided, it streams a new user message.
func (h *harness) Stream(ctx context.Context, input Input) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(Event{}, err)
			return
		}
		if input.Decisions != nil && input.Message != nil {
			yield(Event{}, fmt.Errorf("%w: cannot specify both message and decisions", ErrInvalidConfig))
			return
		}
		if input.Decisions == nil && input.Message == nil {
			yield(Event{}, ErrNilMessage)
			return
		}

		state, err := h.LoadState(ctx)
		if err != nil {
			yield(Event{}, err)
			return
		}

		var update graph.Update[State]
		if input.Decisions != nil {
			if len(state.PendingPermissions) == 0 {
				yield(Event{}, ErrNoPendingPermissions)
				return
			}
			normalized := make(map[string]tools.PermissionDecision, len(state.PendingPermissions)*2)
			for _, req := range state.PendingPermissions {
				dec, ok := lookupDecision(input.Decisions, req)
				if !ok {
					yield(Event{}, ErrPendingPermissionsUnresolved)
					return
				}
				dec.RequestID = req.ID
				dec.ToolCallID = req.ToolCallID
				normalized[req.ID] = dec
				normalized[req.ToolCallID] = dec
			}
			update = graph.Update[State](func(s State) State {
				s.PermissionDecisions = normalized
				return s
			})
		} else {
			in := message.CloneMessage(input.Message)
			_, _, unfulfilled, _ := state.ActiveToolCalls()
			if len(unfulfilled) == 0 {
				id, err := uuid.NewV7()
				if err != nil {
					yield(Event{}, fmt.Errorf("harness: generate user message id: %w", err))
					return
				}
				in.SetID(id.String())
				if !yield(Event{
					Type:    EventMessage,
					Message: message.CloneMessage(in),
				}, nil) {
					return
				}
			}
			update = graph.Update[State](func(s State) State {
				s.Messages = append(s.Messages, in)
				return s
			})
		}

		stream, err := h.graph.Stream(ctx, update, &graph.Location{ThreadID: h.id})
		if err != nil {
			yield(Event{}, err)
			return
		}

		h.consumeGraphStream(stream, yield)
	}
}

func (h *harness) consumeGraphStream(stream iter.Seq2[graph.StreamEvent, error], yield func(Event, error) bool) {
	for ev, err := range stream {
		if err != nil {
			yield(Event{}, err)
			return
		}

		switch ev.Event {
		case graph.EventLLMChunk:
			chunk, ok := ev.Data.(message.AssistantChunk)
			if !ok {
				continue
			}
			chunkCopy := message.CloneAssistantChunk(chunk)
			if !yield(Event{
				Type:  EventChunk,
				Chunk: &chunkCopy,
			}, nil) {
				return
			}

		case eventNodeMessage:
			msg, ok := ev.Data.(message.Message)
			if !ok || msg == nil {
				continue
			}
			if !yield(Event{
				Type:    EventMessage,
				Message: message.CloneMessage(msg),
			}, nil) {
				return
			}

		case graph.EventInterrupted:
			snap, ok := ev.Data.(graph.Snapshot[State])
			if !ok {
				continue
			}
			if !yield(Event{
				Type:        EventInterrupted,
				Permissions: clonePermissionRequests(snap.State.PendingPermissions),
			}, nil) {
				return
			}

		case graph.EventCompleted:
			snap, ok := ev.Data.(graph.Snapshot[State])
			if !ok || len(snap.State.Messages) == 0 {
				continue
			}
			lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
			if !yield(Event{
				Type:    EventCompleted,
				Message: message.CloneMessage(lastMsg),
			}, nil) {
				return
			}
		}
	}
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
