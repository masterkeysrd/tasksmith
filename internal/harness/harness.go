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
)

// Config holds the mandatory dependencies required to construct a [Harness].
type Config struct {
	DB           *sql.DB
	Model        *llm.Model
	SystemPrompt string
	Logger       log.Interface
}

// Validate checks that all required configuration dependencies are present.
func (c Config) Validate() error {
	if c.DB == nil {
		return fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	if c.Model == nil {
		return fmt.Errorf("%w: model is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return nil
}

// Harness orchestrates stateful, multi-turn agent conversations over a compiled
// Loom workflow graph.
type Harness struct {
	logger log.Interface
	graph  *graph.Graph[State]
}

// New constructs and compiles a new [Harness] from the provided [Config].
func New(cfg Config) (*Harness, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cp, err := sqlite.NewCheckpointer(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("harness: initialize sqlite checkpointer: %w", err)
	}

	logger := cfg.Logger.ForComponent("harness")

	g, err := buildGraph(cp, cfg.Model, cfg.SystemPrompt, logger)
	if err != nil {
		return nil, err
	}

	return &Harness{
		logger: logger,
		graph:  g,
	}, nil
}

// Stream executes a single conversational turn on the specified threadID,
// returning an iterator that yields incremental [EventChunk] notifications
// followed by a terminal [EventCompleted] event carrying the completed
// [*message.Assistant] reply.
func (h *Harness) Stream(ctx context.Context, threadID string, input message.Message) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if strings.TrimSpace(threadID) == "" {
			yield(Event{}, ErrEmptyThreadID)
			return
		}
		if input == nil {
			yield(Event{}, ErrNilMessage)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(Event{}, err)
			return
		}

		in := message.CloneMessage(input)
		if in.GetID() == "" {
			in.SetID(uuid.NewString())
		}

		update := graph.Update[State](func(s State) State {
			s.Messages = append(s.Messages, in)
			return s
		})

		stream, err := h.graph.Stream(ctx, update, &graph.Location{ThreadID: threadID})
		if err != nil {
			yield(Event{}, err)
			return
		}

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

			case graph.EventCompleted:
				snap, ok := ev.Data.(graph.Snapshot[State])
				if !ok || len(snap.State.Messages) == 0 {
					continue
				}
				lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
				if asst, ok := message.CloneMessage(lastMsg).(*message.Assistant); ok {
					if !yield(Event{
						Type:    EventCompleted,
						Message: asst,
					}, nil) {
						return
					}
				}
			}
		}
	}
}

// LoadState retrieves the latest checkpointed [State] for threadID.
// If no checkpoint exists for threadID, it returns an empty [State] and nil error.
func (h *Harness) LoadState(ctx context.Context, threadID string) (State, error) {
	if strings.TrimSpace(threadID) == "" {
		return State{}, ErrEmptyThreadID
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	snap, err := h.graph.Load(ctx, graph.Location{ThreadID: threadID})
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
