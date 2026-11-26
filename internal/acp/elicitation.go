package acp

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func questionProperty(q api.Question) (sdk.ElicitationPropertySchema, error) {
	var p sdk.ElicitationPropertySchema
	var choices []sdk.EnumOption
	for _, o := range q.Options {
		choices = append(choices, sdk.EnumOption{
			Const:       o.ID,
			Title:       o.Label,
			Description: &o.Description,
		})
	}

	switch api.QuestionType(q.Type) {
	case api.QuestionTypeSingle, api.QuestionTypeText:
		p.String = &sdk.ElicitationPropertySchemaString{
			Title:       &q.Question,
			Description: &q.Description,
			OneOf:       choices,
		}
	case api.QuestionTypeMulti:
		p.Array = &sdk.ElicitationPropertySchemaArray{
			Title:       &q.Question,
			Description: &q.Description,
			Items: sdk.MultiSelectItems{
				Titled: &sdk.TitledMultiSelectItems{AnyOf: choices},
			},
		}
	case api.QuestionTypeConfirm:
		p.Boolean = &sdk.ElicitationPropertySchemaBoolean{
			Title:       &q.Question,
			Description: &q.Description,
		}
	default:
		return p, fmt.Errorf("unsupported daemon question type %q", q.Type)
	}

	return p, nil
}

func decodeAnswer(q api.Question, v sdk.ElicitationContentValue) (api.QuestionAnswer, error) {
	answer := api.QuestionAnswer{Question: q.Question}
	switch {
	case q.Type == "single" && v.String != nil:
		answer.Selected = []string{string(*v.String)}
	case q.Type == "multi" && v.StringArray != nil:
		answer.Selected = []string(*v.StringArray)
	case q.Type == "text" && v.String != nil:
		answer.Text = string(*v.String)
	case q.Type == "confirm" && v.Boolean != nil:
		answer.Confirmed = new(bool(*v.Boolean))
	default:
		return answer, sdk.NewInvalidParams("missing or invalid elicitation answer")
	}

	for _, selected := range answer.Selected {
		if !slices.ContainsFunc(q.Options, func(o api.QuestionOption) bool { return o.ID == selected }) {
			return answer, sdk.NewInvalidParams("unknown elicitation choice")
		}
	}

	return answer, nil
}

func (a *agent) form(
	ctx context.Context,
	id sdk.SessionId,
	call, message string,
	schema sdk.ElicitationSchema,
) (*sdk.CreateElicitationAccept, error) {
	res, err := a.conn.CreateElicitation(ctx, sdk.CreateElicitationRequest{
		Form: &sdk.CreateElicitationForm{
			Session: &sdk.ElicitationSessionScope{
				SessionId:  id,
				ToolCallId: new(sdk.ToolCallId(call)),
			},
			Message:         message,
			RequestedSchema: schema,
		},
	})
	if err != nil {
		return nil, err
	}
	if res.Accept == nil && res.Cancel == nil && res.Decline == nil {
		return nil, sdk.NewInvalidParams("unknown elicitation response")
	}

	return res.Accept, nil
}

func (a *agent) question(ctx context.Context, c *client.Client, route interactionRoute, q api.QuestionRequest) error {
	schema := sdk.ElicitationSchema{
		Type:        sdk.ElicitationSchemaTypeObject,
		Properties:  make(map[string]sdk.ElicitationPropertySchema),
		Description: &q.Description,
	}

	for i, item := range q.Questions {
		key := fmt.Sprintf("question_%d", i+1)
		p, err := questionProperty(item)
		if err != nil {
			return err
		}
		schema.Properties[key] = p
		schema.Required = append(schema.Required, key)
	}

	accepted, err := a.form(ctx, route.editor, q.ToolCallID, cmp.Or(q.Title, "Answer the following questions"), schema)
	if err != nil {
		return err
	}

	response := api.QuestionResponse{
		RequestID:  q.ID,
		ToolCallID: q.ToolCallID,
		Canceled:   accepted == nil,
	}
	if accepted != nil {
		for i, key := range schema.Required {
			answer, err := decodeAnswer(q.Questions[i], accepted.Content[key])
			if err != nil {
				return err
			}
			response.Answers = append(response.Answers, answer)
		}
	}

	return c.Send(ctx, string(route.target), api.SendRequest{Question: &response})
}

func (a *agent) review(ctx context.Context, c *client.Client, route interactionRoute, q api.PlanReviewRequest) error {
	choices := []string{
		"approve_accept_edits",
		"approve_default",
		"approve_auto",
		"revise",
	}
	schema := sdk.ElicitationSchema{
		Type:     sdk.ElicitationSchemaTypeObject,
		Required: []string{"decision"},
		Properties: map[string]sdk.ElicitationPropertySchema{
			"decision": {
				String: &sdk.ElicitationPropertySchemaString{Enum: choices},
			},
			"feedback": {
				String: &sdk.ElicitationPropertySchemaString{},
			},
		},
	}

	accepted, err := a.form(ctx, route.editor, q.ToolCallID, "Review plan: "+q.Path+"\n\n"+q.Content, schema)
	if err != nil {
		return err
	}

	decision := api.PlanReviewDecision{
		RequestID:  q.ID,
		ToolCallID: q.ToolCallID,
		Canceled:   accepted == nil,
	}
	if accepted != nil {
		value := accepted.Content["decision"].String
		if value == nil || !slices.Contains(choices, string(*value)) {
			return sdk.NewInvalidParams("unknown plan decision")
		}
		if mode, ok := strings.CutPrefix(string(*value), "approve_"); ok {
			decision.Approved = true
			decision.Mode = api.PermissionMode(mode)
		}
		if feedback := accepted.Content["feedback"].String; feedback != nil {
			decision.Feedback = string(*feedback)
		}
	}

	return c.Send(ctx, string(route.target), api.SendRequest{PlanReview: &decision})
}
