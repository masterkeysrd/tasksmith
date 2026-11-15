package metrics

import (
	"context"
	"strconv"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

// LLMCall describes one LLM request for [RecordLLMCall].
type LLMCall struct {
	Provider  string
	Model     string
	Purpose   Purpose
	Status    Status
	ErrorKind string

	Tokens   api.UsageTokens
	Cost     Cost   // nano-USD
	CostTier string // empty = base pricing
	Priced   bool

	Duration   time.Duration
	TTFT       time.Duration
	Processing time.Duration
	Generation time.Duration
}

// Cost is the nano-USD cost breakdown of an LLM call.
type Cost struct{ Input, Output, CacheRead, CacheWrite, Reasoning, Total int64 }

// ToolCall describes one tool execution for [RecordToolCall].
type ToolCall struct {
	Tool       string
	ToolCallID string
	Status     Status
	ErrorKind  string
	Duration   time.Duration
}

// RecordLLMCall records an llm_call. A nil recorder disables recording.
func RecordLLMCall(ctx context.Context, r Recorder, c LLMCall) error {
	if r == nil {
		return nil
	}
	rec := Record{
		Kind:   KindLLMCall,
		Status: c.Status,
		Attrs: attrs(
			AttrProvider, c.Provider,
			AttrModel, c.Model,
			AttrPurpose, string(c.Purpose),
			AttrCostTier, c.CostTier,
			AttrPriced, strconv.FormatBool(c.Priced),
			AttrErrorKind, c.ErrorKind,
		),
	}
	add := func(m Metric, v int64) {
		if v != 0 {
			rec.Measurements = append(rec.Measurements, Measurement{Metric: m, Value: v})
		}
	}
	add(TokensInput, c.Tokens.Input)
	add(TokensOutput, c.Tokens.Output)
	add(TokensCacheRead, c.Tokens.CacheRead)
	add(TokensCacheWrite, c.Tokens.CacheWrite)
	add(TokensReasoning, c.Tokens.Reasoning)
	add(CostInput, c.Cost.Input)
	add(CostOutput, c.Cost.Output)
	add(CostCacheRead, c.Cost.CacheRead)
	add(CostCacheWrite, c.Cost.CacheWrite)
	add(CostReasoning, c.Cost.Reasoning)
	add(CostTotal, c.Cost.Total)
	rec.Measurements = append(rec.Measurements, Measurement{Metric: LatencyDuration, Value: c.Duration.Milliseconds()})
	add(LatencyTTFT, c.TTFT.Milliseconds())
	add(LatencyProcessing, c.Processing.Milliseconds())
	add(LatencyGeneration, c.Generation.Milliseconds())
	return r.Record(ctx, rec)
}

// RecordToolCall records a tool_call. A nil recorder disables recording.
func RecordToolCall(ctx context.Context, r Recorder, c ToolCall) error {
	if r == nil {
		return nil
	}
	return r.Record(ctx, Record{
		Kind:   KindToolCall,
		Status: c.Status,
		Attrs: attrs(
			AttrTool, c.Tool,
			AttrToolCallID, c.ToolCallID,
			AttrErrorKind, c.ErrorKind,
		),
		Measurements: []Measurement{{Metric: LatencyDuration, Value: c.Duration.Milliseconds()}},
	})
}

// attrs builds attrs from key/value pairs, omitting empty values.
func attrs(kv ...string) []Attr {
	out := make([]Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			out = append(out, Attr{Key: kv[i], Value: kv[i+1]})
		}
	}
	return out
}

// RecordFileChange records a file_change. A nil recorder disables recording.
func RecordFileChange(ctx context.Context, r Recorder, c api.FileChange) error {
	if r == nil {
		return nil
	}
	revert := ""
	if c.Tool == "revert" {
		revert = "true"
	}
	rec := Record{
		Kind:   KindFileChange,
		Status: StatusOK,
		Attrs: attrs(
			AttrTool, c.Tool,
			AttrToolCallID, c.ToolCallID,
			AttrPath, c.Path,
			AttrSource, string(c.Source),
			AttrChange, string(c.Kind),
			AttrRevert, revert,
		),
		Measurements: []Measurement{{Metric: FilesChanged, Value: 1}},
	}
	if c.Additions != 0 {
		rec.Measurements = append(rec.Measurements, Measurement{Metric: LinesAdded, Value: int64(c.Additions)})
	}
	if c.Deletions != 0 {
		rec.Measurements = append(rec.Measurements, Measurement{Metric: LinesRemoved, Value: int64(c.Deletions)})
	}
	return r.Record(ctx, rec)
}
