package metrics

import (
	"context"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

// Recorder persists metric records. Implementations must not block the caller.
type Recorder interface {
	Record(ctx context.Context, r Record) error
}

// Record is one observed occurrence and its measurements.
type Record struct {
	Kind         Kind
	Status       Status
	Attrs        []Attr
	Measurements []Measurement
}

// Kind identifies what a record describes (free-form).
type Kind string

// Status is the outcome of the recorded occurrence.
type Status string

// Status values.
const (
	StatusOK       Status = "ok"
	StatusError    Status = "error"
	StatusCanceled Status = "canceled"
	StatusDenied   Status = "denied" // tool calls rejected by permissions
)

// Attr is a descriptive string dimension of a record.
type Attr struct {
	Key   string
	Value string
}

// Metric is the name of a measurement (free-form).
type Metric string

// Measurement is a single integer value of a metric.
type Measurement struct {
	Metric Metric
	Value  int64
}

// Purpose identifies why an LLM call was made.
type Purpose string

// Purpose values.
const (
	PurposeChat       Purpose = "chat"
	PurposeCompaction Purpose = "compaction"
	PurposeTitle      Purpose = "title"
)

// Kinds written by TaskSmith.
const (
	KindLLMCall    Kind = "llm_call"
	KindToolCall   Kind = "tool_call"
	KindFileChange Kind = "file_change"
)

// Attribute keys written by TaskSmith.
const (
	AttrProvider   = "provider"
	AttrModel      = "model"
	AttrPurpose    = "purpose"
	AttrCostTier   = "cost_tier"
	AttrPriced     = "priced"
	AttrErrorKind  = "error_kind"
	AttrTool       = "tool"
	AttrToolCallID = "tool_call_id"
	AttrPath       = "path"
	AttrSource     = "source"
	AttrChange     = "change"
	AttrRevert     = "revert"
)

// Metric names written by TaskSmith.
const (
	TokensInput      Metric = "tokens.input"
	TokensOutput     Metric = "tokens.output"
	TokensCacheRead  Metric = "tokens.cache_read"
	TokensCacheWrite Metric = "tokens.cache_write"
	TokensReasoning  Metric = "tokens.reasoning"

	CostInput      Metric = "cost.input"
	CostOutput     Metric = "cost.output"
	CostCacheRead  Metric = "cost.cache_read"
	CostCacheWrite Metric = "cost.cache_write"
	CostReasoning  Metric = "cost.reasoning"
	CostTotal      Metric = "cost.total"

	LatencyDuration   Metric = "latency.duration_ms"
	LatencyTTFT       Metric = "latency.ttft_ms"
	LatencyProcessing Metric = "latency.processing_ms"
	LatencyGeneration Metric = "latency.generation_ms"

	LinesAdded   Metric = "lines.added"
	LinesRemoved Metric = "lines.removed"
	FilesChanged Metric = "files.changed"
)

// Entry is a [Record] after attribution: it carries the identity, timestamp
// and scope fields that the [Recorder] fills in. Callers never construct it.
type Entry struct {
	ID              string
	CreatedAt       time.Time
	WorkspaceRoot   string
	SessionID       string
	ParentSessionID string
	TurnID          string
	MessageID       string
	Agent           string
	Record
}

// NewEntry attributes r using the [scope.Scope] in ctx.
func NewEntry(ctx context.Context, r Record) Entry {
	s, _ := scope.From(ctx)
	return Entry{
		ID:              uuid.NewString(),
		CreatedAt:       time.Now().UTC(),
		WorkspaceRoot:   s.WorkspaceRoot,
		SessionID:       s.SessionID,
		ParentSessionID: s.ParentSessionID,
		TurnID:          s.TurnID,
		MessageID:       s.MessageID,
		Agent:           s.Agent,
		Record:          r,
	}
}

// Value returns the value of metric m on the record, or zero.
func (r Record) Value(m Metric) int64 {
	for _, ms := range r.Measurements {
		if ms.Metric == m {
			return ms.Value
		}
	}
	return 0
}

// Attr returns the value of the attribute with key, or "".
func (r Record) Attr(key string) string {
	for _, a := range r.Attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}
