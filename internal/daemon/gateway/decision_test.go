package gateway

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/warp"
)

func TestDecisionQuestionsAllTypesAndRubricPlacement(t *testing.T) {
	dec := &warp.Decision{
		Spec: warp.DecisionSpec{
			Questions: []warp.DecisionQuestion{
				{
					Name:         "is_urgent",
					Type:         warp.DecisionQuestionPredicate,
					Instructions: "Is this urgent?",
					Criteria: &warp.DecisionCriteria{
						True:  "Immediate action needed",
						False: "Can wait",
					},
				},
				{
					Name:         "safety",
					Type:         warp.DecisionQuestionChoice,
					Instructions: "Pick safety level.",
					Options: []warp.DecisionOption{
						{Value: "safe", Description: "Low harm"},
						{Value: "unsafe", Description: "High harm"},
					},
				},
				{
					Name:         "quality",
					Type:         warp.DecisionQuestionScore,
					Instructions: "Score quality.",
					Levels: []warp.DecisionLevel{
						{Label: "low", Description: "Needs work"},
						{Label: "high", Description: "Ready"},
					},
				},
			},
		},
	}

	qs, err := DecisionQuestions(dec, "Shared rubric body.", "safety")
	if err != nil {
		t.Fatalf("DecisionQuestions: %v", err)
	}
	if len(qs) != 3 {
		t.Fatalf("len(qs) = %d, want 3", len(qs))
	}

	if qs[0].Type != decision.QuestionPredicate || qs[0].TrueDescription != "Immediate action needed" {
		t.Fatalf("predicate mapping = %+v", qs[0])
	}
	if strings.Contains(qs[0].Instructions, "Shared rubric body.") {
		t.Fatalf("rubric should not be duplicated on non-target question: %q", qs[0].Instructions)
	}

	if qs[1].Type != decision.QuestionChoice || len(qs[1].Choices) != 2 {
		t.Fatalf("choice mapping = %+v", qs[1])
	}
	if qs[1].Instructions != "Shared rubric body.\n\nPick safety level." {
		t.Fatalf("target question instructions = %q", qs[1].Instructions)
	}

	if qs[2].Type != decision.QuestionScore || len(qs[2].Levels) != 2 || qs[2].Levels[1].Label != "high" {
		t.Fatalf("score mapping = %+v", qs[2])
	}
}

func TestDecisionQuestionsErrors(t *testing.T) {
	if _, err := DecisionQuestions(nil, "", ""); err == nil {
		t.Fatal("expected error for nil decision")
	}

	dec := &warp.Decision{
		Spec: warp.DecisionSpec{
			Questions: []warp.DecisionQuestion{
				{Name: "q1", Type: warp.DecisionQuestionType("bogus")},
			},
		},
	}
	if _, err := DecisionQuestions(dec, "rubric", "missing"); err == nil {
		t.Fatal("expected error for missing rubric target")
	}
	if _, err := DecisionQuestions(dec, "rubric", "q1"); err == nil {
		t.Fatal("expected error for unsupported question type")
	}
}
