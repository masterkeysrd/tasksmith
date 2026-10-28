package harness

import (
	"reflect"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func TestMessageUpdateContextOptions(t *testing.T) {
	first := message.NewUserText("first")
	second := message.NewUserText("second")
	third := message.NewUserText("third")
	cases := []struct {
		name   string
		update func(State, MessageUpdateOptions) State
		want   message.MessageList
	}{
		{"prepend", func(s State, opts MessageUpdateOptions) State { return s.PrependMessage(first, opts) }, message.MessageList{first, second}},
		{"append", func(s State, opts MessageUpdateOptions) State { return s.AppendMessage(third, opts) }, message.MessageList{second, third}},
		{"append multiple", func(s State, opts MessageUpdateOptions) State {
			return s.AppendMessages(message.MessageList{first, third}, opts)
		}, message.MessageList{second, first, third}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initial := State{Messages: message.MessageList{second}, Context: api.ContextState{UsedTokens: 999, CachedTokens: 12}}
			deferred := tc.update(initial.Copy(), MessageUpdateOptions{SkipContextRecalculation: true})
			if !reflect.DeepEqual(deferred.Messages, tc.want) || !reflect.DeepEqual(deferred.Context, initial.Context) {
				t.Fatalf("deferred update changed context or message order: %+v", deferred)
			}

			opts := MessageUpdateOptions{Static: StaticContext{SystemPrompt: 42}, Limits: workspace.ModelLimits{Context: 100000, Output: 1000}}
			updated := tc.update(initial.Copy(), opts)
			if !reflect.DeepEqual(updated.Messages, tc.want) || !reflect.DeepEqual(updated.Context, computeContextState(opts.Static, updated, opts.Limits)) {
				t.Fatalf("update did not recalculate context: %+v", updated)
			}
			defaults := tc.update(initial.Copy(), MessageUpdateOptions{})
			if defaults.Context.MaxTokens != 128000 {
				t.Fatalf("zero-value options did not recalculate with default limits: %+v", defaults.Context)
			}
		})
	}
}

func TestDeferredMessageUpdatesBeforeCompaction(t *testing.T) {
	static := StaticContext{SystemPrompt: 10}
	limits := workspace.ModelLimits{Context: 100000, Output: 1000}
	original := State{PendingCompaction: &CompactInput{}, Context: api.ContextState{UsedTokens: 999, CachedTokens: 80, Compactions: 2}}
	summary := message.NewUserText("summary")
	first := message.NewUserText("first")
	second := message.NewUserText("second")
	deferred := MessageUpdateOptions{SkipContextRecalculation: true}
	state := original.AppendMessage(first, deferred)
	state = state.AppendMessages(message.MessageList{second}, deferred)
	state = state.PrependMessage(summary, deferred)
	if !reflect.DeepEqual(state.Context, original.Context) {
		t.Fatalf("context changed during deferred updates: %+v", state.Context)
	}
	state = state.ApplyCompaction(static, limits)
	if !reflect.DeepEqual(state.Messages, message.MessageList{summary, first, second}) {
		t.Fatalf("unexpected message order: %+v", state.Messages)
	}
	if state.PendingCompaction != nil || state.Context.CachedTokens != 0 || state.Context.Compactions != 3 || state.Context.LastCompacted.IsZero() {
		t.Fatalf("unexpected compaction state: %+v", state)
	}
	if want := computeContextState(static, state, limits); !reflect.DeepEqual(state.Context, want) {
		t.Fatalf("context not recalculated after compaction: got %+v, want %+v", state.Context, want)
	}
}
