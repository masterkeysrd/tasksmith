// Package metrics provides an asynchronous, SQLite-backed system for recording
// and querying operational measurements across LLM calls, tool executions, and
// file changes, and for aggregating them into per-session and cross-session
// usage summaries.
//
// # Core Types
//
// A [Record] is one observed occurrence with a [Kind] (llm_call, tool_call,
// or file_change), a [Status], descriptive [Attr] dimensions, and numeric
// [Measurement] values. A [Recorder] persists records; the [Store] is the
// SQLite-backed implementation.
//
// A [Entry] is a [Record] after attribution: it carries the identity,
// timestamp, and scope fields ([scope.Scope]) that the recorder fills in.
// Callers never construct an [Entry] directly; they call [NewEntry] or use
// the convenience helpers [RecordLLMCall], [RecordToolCall], and
// [RecordFileChange].
//
// # Asynchronous Batched Writes
//
// [Store] (created via [NewStore]) queues records in a bounded in-memory
// channel and writes them in batched transactions on a background goroutine.
// [Store.Record] never blocks: when the queue is full the record is dropped
// with a warning. [Store.Flush] blocks until every queued record is written,
// and [Store.Close] flushes and stops the writer.
//
// # Querying & Aggregation
//
// [Query] describes a generic aggregation over records and measurements,
// supporting [AggSum], [AggMin], and [AggMax] over named [Metric] values,
// grouping by [Dimension] columns or attribute keys, and optional time
// [Bucket]ing. [Store.Query] (or the standalone [RunQuery]) executes a query
// and returns a [Result] of [Row]s.
//
// # Usage Accumulation
//
// [Usage] accumulates the records of one session into an
// [github.com/masterkeysrd/tasksmith/api.SessionUsage], tracking LLM token
// counts, costs, latency, tool-call statistics, code-change totals, and
// prompt-cache behavior. [Store.SessionUsage] loads and accumulates all
// records for a session and its children.
//
// [Store.Summary] aggregates LLM, tool, and code-change usage across sessions
// for a workspace or globally, with grouping by model, provider, agent,
// purpose, cost tier, or day, plus top-session rankings and period-over-period
// comparison.
//
// # Live Totals
//
// [Live] decorates a [Store] with running per-session totals. After each
// record it updates the affected sessions' [Usage] accumulators and calls a
// publish callback with the updated [github.com/masterkeysrd/tasksmith/api.SessionUsage],
// enabling real-time usage display without polling the database.
package metrics
