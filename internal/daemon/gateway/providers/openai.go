package providers

import (
	"context"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/llm"
)

func OpenAIStrictResponseSchemaMiddleware() llm.Middleware {
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if req == nil || req.ResponseSchema == nil {
				return next(ctx, req)
			}
			cloned := *req
			cloned.ResponseSchema = req.ResponseSchema.CloneSchemas()
			normalizeStrictResponseSchema(cloned.ResponseSchema)
			return next(ctx, &cloned)
		}
	}
}

func normalizeStrictResponseSchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	for _, schemas := range []map[string]*jsonschema.Schema{
		s.Properties, s.PatternProperties, s.Defs, s.Definitions, s.DependencySchemas, s.DependentSchemas,
	} {
		for _, child := range schemas {
			normalizeStrictResponseSchema(child)
		}
	}
	for _, schemas := range [][]*jsonschema.Schema{
		s.ItemsArray, s.PrefixItems, s.AllOf, s.AnyOf, s.OneOf,
	} {
		for _, child := range schemas {
			normalizeStrictResponseSchema(child)
		}
	}
	for _, child := range []*jsonschema.Schema{
		s.AdditionalProperties, s.PropertyNames, s.UnevaluatedProperties,
		s.Items, s.AdditionalItems, s.Contains, s.UnevaluatedItems,
		s.Not, s.If, s.Then, s.Else, s.ContentSchema,
	} {
		normalizeStrictResponseSchema(child)
	}
	if s.AdditionalProperties == nil && (s.Type == "object" || slices.Contains(s.Types, "object") || len(s.Properties) > 0) {
		s.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
		if s.Properties == nil {
			s.Properties = map[string]*jsonschema.Schema{}
		}
	}
}
