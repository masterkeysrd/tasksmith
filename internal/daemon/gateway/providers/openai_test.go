package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/llm"
)

func TestOpenAIStrictResponseSchemaMiddleware(t *testing.T) {
	leaf := &jsonschema.Schema{Type: "object"}
	leaves := []*jsonschema.Schema{leaf}
	leafMap := map[string]*jsonschema.Schema{"nested": leaf}
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"primitive": {Type: "string"},
			"union":     {Types: []string{"object", "null"}},
			"implicit":  {Properties: leafMap},
			"array":     {Type: "array", Items: leaf},
			"tuple":     {Type: "array", ItemsArray: leaves},
			"typed":     {Type: "object", AdditionalProperties: leaf},
			"open":      {Type: "object", AdditionalProperties: &jsonschema.Schema{}},
			"closed":    {Type: "object", AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}},
			"legacy":    {Definitions: leafMap, DependencySchemas: leafMap},
		},
		PatternProperties: leafMap,
		Defs:              leafMap,
		PrefixItems:       leaves,
		AllOf:             leaves,
		AnyOf:             leaves,
		OneOf:             leaves,
		Not:               leaf,
	}
	before, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	req := &llm.Request{Model: "test-model", ResponseSchema: schema}
	stream := OpenAIStrictResponseSchemaMiddleware()(func(_ context.Context, got *llm.Request) (llm.StreamResponse, error) {
		if got == req || got.ResponseSchema == schema || got.Model != req.Model {
			t.Fatal("expected a cloned request and schema with the original model")
		}
		s := got.ResponseSchema
		for name, node := range map[string]*jsonschema.Schema{
			"root":              s,
			"union":             s.Properties["union"],
			"implicit":          s.Properties["implicit"],
			"properties":        s.Properties["implicit"].Properties["nested"],
			"patternProperties": s.PatternProperties["nested"],
			"$defs":             s.Defs["nested"],
			"definitions":       s.Properties["legacy"].Definitions["nested"],
			"dependencies":      s.Properties["legacy"].DependencySchemas["nested"],
			"items":             s.Properties["array"].Items,
			"itemsArray":        s.Properties["tuple"].ItemsArray[0],
			"prefixItems":       s.PrefixItems[0],
			"allOf":             s.AllOf[0],
			"anyOf":             s.AnyOf[0],
			"oneOf":             s.OneOf[0],
			"not":               s.Not,
			"typedAdditional":   s.Properties["typed"].AdditionalProperties,
		} {
			raw, err := json.Marshal(node.AdditionalProperties)
			if err != nil || string(raw) != "false" || node.Properties == nil || node == leaf {
				t.Errorf("%s: expected a cloned object with properties and additionalProperties:false; got %s (err: %v)", name, node, err)
			}
		}
		for _, name := range []string{"primitive", "open", "closed"} {
			want := schema.Properties[name].AdditionalProperties
			got := s.Properties[name].AdditionalProperties
			if (got == nil) != (want == nil) || (got != nil && (got == want || (got.Not == nil) != (want.Not == nil))) {
				t.Errorf("%s: explicit additionalProperties or primitive schema changed", name)
			}
		}
		return nil, nil
	})
	if _, err := stream(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if req.ResponseSchema != schema || !bytes.Equal(before, after) {
		t.Fatal("middleware mutated the caller's request or schema")
	}
}

func TestOpenAIStrictResponseSchemaMiddlewarePassThrough(t *testing.T) {
	for _, req := range []*llm.Request{nil, {Model: "test-model"}} {
		stream := OpenAIStrictResponseSchemaMiddleware()(func(_ context.Context, got *llm.Request) (llm.StreamResponse, error) {
			if got != req {
				t.Fatal("request without a response schema should pass through unchanged")
			}
			return nil, nil
		})
		if _, err := stream(t.Context(), req); err != nil {
			t.Fatal(err)
		}
	}
}
