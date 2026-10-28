package harness

import (
	"errors"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestInputHasResponses(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  bool
	}{
		{name: "nil maps"},
		{name: "message only", input: Input{Message: message.NewUserText("hello")}},
		{name: "compact only", input: Input{Compact: &CompactInput{}}},
		{name: "empty decisions", input: Input{Decisions: map[string]tools.PermissionDecision{}}, want: true},
		{name: "empty answers", input: Input{Answers: map[string]tools.QuestionResponse{}}, want: true},
		{name: "populated decisions", input: Input{Decisions: map[string]tools.PermissionDecision{"id": {Kind: tools.DecisionReject}}}, want: true},
		{name: "populated answers", input: Input{Answers: map[string]tools.QuestionResponse{"id": {Canceled: true}}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.HasResponses(); got != tt.want {
				t.Errorf("HasResponses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInputValidation(t *testing.T) {
	msg := message.NewUserText("hello")
	decisions := map[string]tools.PermissionDecision{}
	answers := map[string]tools.QuestionResponse{}
	compact := &CompactInput{}
	tests := []struct {
		name        string
		input       Input
		interactive bool
		errorText   string
	}{
		{name: "empty"},
		{name: "compact", input: Input{Compact: compact}},
		{name: "message", input: Input{Message: msg}, interactive: true},
		{name: "empty decisions", input: Input{Decisions: decisions}, interactive: true},
		{name: "empty answers", input: Input{Answers: answers}, interactive: true},
		{name: "both responses", input: Input{Decisions: decisions, Answers: answers}, interactive: true},
		{name: "compact and message", input: Input{Compact: compact, Message: msg}, interactive: true, errorText: "harness: invalid configuration: cannot specify both compact and message/responses"},
		{name: "compact and decisions", input: Input{Compact: compact, Decisions: decisions}, interactive: true, errorText: "harness: invalid configuration: cannot specify both compact and message/responses"},
		{name: "compact and answers", input: Input{Compact: compact, Answers: answers}, interactive: true, errorText: "harness: invalid configuration: cannot specify both compact and message/responses"},
		{name: "message and decisions", input: Input{Message: msg, Decisions: decisions}, interactive: true, errorText: "harness: invalid configuration: cannot specify both message and responses"},
		{name: "message and answers", input: Input{Message: msg, Answers: answers}, interactive: true, errorText: "harness: invalid configuration: cannot specify both message and responses"},
		{name: "all fields", input: Input{Compact: compact, Message: msg, Decisions: decisions, Answers: answers}, interactive: true, errorText: "harness: invalid configuration: cannot specify both compact and message/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.HasInteractiveContent(); got != tt.interactive {
				t.Errorf("HasInteractiveContent() = %v, want %v", got, tt.interactive)
			}
			err := tt.input.Validate()
			if tt.errorText == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidConfig) || err.Error() != tt.errorText {
				t.Fatalf("Validate() = %v, want %q wrapping ErrInvalidConfig", err, tt.errorText)
			}
		})
	}
}
