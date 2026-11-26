package gateway

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/warp"
)

// DecisionQuestions translates a WARP Decision manifest's spec.questions into
// Loom [decision.Question] values. When rubric is non-empty, it is prepended
// once to the question named rubricTarget (or the first question when
// rubricTarget is empty) so multi-question evaluations do not duplicate shared
// rubric instructions across every question.
func DecisionQuestions(
	dec *warp.Decision,
	rubric string,
	rubricTarget string,
) ([]decision.Question, error) {
	if dec == nil || len(dec.Spec.Questions) == 0 {
		return nil, fmt.Errorf("gateway: decision manifest has no questions")
	}

	rubric = strings.TrimSpace(rubric)
	targetIdx := 0
	if rubricTarget != "" {
		found := false
		for i, q := range dec.Spec.Questions {
			if q.Name == rubricTarget {
				targetIdx = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("gateway: rubric target question %q not found in decision", rubricTarget)
		}
	}

	out := make([]decision.Question, 0, len(dec.Spec.Questions))
	for i, q := range dec.Spec.Questions {
		instructions := strings.TrimSpace(q.Instructions)
		if rubric != "" && i == targetIdx {
			if instructions != "" {
				instructions = rubric + "\n\n" + instructions
			} else {
				instructions = rubric
			}
		}

		mapped, err := mapDecisionQuestion(q, instructions)
		if err != nil {
			return nil, err
		}
		out = append(out, mapped)
	}

	return out, nil
}

func mapDecisionQuestion(q warp.DecisionQuestion, instructions string) (decision.Question, error) {
	switch q.Type {
	case warp.DecisionQuestionPredicate:
		dq := decision.Predicate(q.Name, instructions)
		if q.Criteria != nil {
			dq = dq.WithCriteria(q.Criteria.True, q.Criteria.False)
		}
		return dq, nil

	case warp.DecisionQuestionChoice:
		opts := make([]decision.Option, 0, len(q.Options))
		for _, opt := range q.Options {
			opts = append(opts, decision.Opt(opt.Value, opt.Description))
		}
		return decision.Choice(q.Name, instructions, opts...), nil

	case warp.DecisionQuestionScore:
		levels := make([]decision.Level, 0, len(q.Levels))
		for _, lvl := range q.Levels {
			levels = append(levels, decision.Lvl(lvl.Label, lvl.Description))
		}
		return decision.Score(q.Name, instructions, levels...), nil

	default:
		return decision.Question{}, fmt.Errorf(
			"gateway: unsupported decision question %q type %q",
			q.Name,
			q.Type,
		)
	}
}
