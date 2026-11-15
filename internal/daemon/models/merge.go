package models

import (
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/ptr"
	"github.com/masterkeysrd/warp"
)

// Default context and output limits applied when neither layer sets them.
const (
	defaultContextLimit = 128000
	defaultOutputLimit  = 16384
)

// Merge builds a Model from at most two layers, highest priority first:
//
//  1. Workspace manifest (a workspace.ProviderModel, from warp Provider resources).
//  2. Built-in profile (a loom provider GetProfile result, models.dev data).
//
// The rules are:
//
//   - Scalars (Name, limits): first non-zero value, by priority.
//   - Capabilities (*bool): first non-nil value, by priority.
//   - Lists (Thinking, Dimensions, modalities): first non-nil list, by priority
//     (lists are replaced, never merged item by item).
//   - Pricing: taken whole from the highest-priority layer that has any pricing
//     (tiers are never mixed across layers).
//   - Limits fallback: if neither layer sets Context/Output, 128000/16384,
//     applied once here.
func Merge(id ID, providerType string, manifest *warp.ProviderModel, base *profile.Profile) Model {
	out := mergeModel(fromManifest(manifest, id, providerType), fromProfile(base, id, providerType))
	if out.Name == "" {
		out.Name = id.Name()
	}
	if out.Limits.Dimensions == 0 && len(out.Capabilities.Dimensions) > 0 {
		out.Limits.Dimensions = out.Capabilities.Dimensions[0]
	}
	if out.Limits.Context == 0 {
		out.Limits.Context = defaultContextLimit
	}
	if out.Limits.Output == 0 {
		out.Limits.Output = defaultOutputLimit
	}
	return out
}

// mergeModel merges layers in priority order (first argument highest priority).
// It does not apply the limits fallback; the caller does that once.
func mergeModel(layers ...Model) Model {
	var out Model
	for _, l := range layers {
		out.ID = firstNonEmptyID(out.ID, l.ID)
		out.Name = firstNonEmpty(out.Name, l.Name)
		out.Label = firstNonEmpty(out.Label, l.Label)
		out.ProviderType = firstNonEmpty(out.ProviderType, l.ProviderType)

		out.Limits.Context = firstNonZero(out.Limits.Context, l.Limits.Context)
		out.Limits.Output = firstNonZero(out.Limits.Output, l.Limits.Output)
		out.Limits.Dimensions = firstNonZero(out.Limits.Dimensions, l.Limits.Dimensions)

		if out.Capabilities.Completion == nil {
			out.Capabilities.Completion = l.Capabilities.Completion
		}
		if out.Capabilities.Decision == nil {
			out.Capabilities.Decision = l.Capabilities.Decision
		}
		if out.Capabilities.Embedding == nil {
			out.Capabilities.Embedding = l.Capabilities.Embedding
		}
		if out.Capabilities.Tools == nil {
			out.Capabilities.Tools = l.Capabilities.Tools
		}
		if out.Capabilities.Attachment == nil {
			out.Capabilities.Attachment = l.Capabilities.Attachment
		}
		if out.Capabilities.StructuredOutput == nil {
			out.Capabilities.StructuredOutput = l.Capabilities.StructuredOutput
		}
		if out.Capabilities.Temperature == nil {
			out.Capabilities.Temperature = l.Capabilities.Temperature
		}
		if out.Capabilities.Dimensions == nil {
			out.Capabilities.Dimensions = l.Capabilities.Dimensions
		}
		if out.Capabilities.Thinking == nil {
			out.Capabilities.Thinking = l.Capabilities.Thinking
		}

		if out.Modalities.Input == nil {
			out.Modalities.Input = l.Modalities.Input
		}
		if out.Modalities.Output == nil {
			out.Modalities.Output = l.Modalities.Output
		}

		if out.Pricing == nil {
			out.Pricing = l.Pricing
		}
		if out.Aliases == nil {
			out.Aliases = l.Aliases
		}

		out.Source.BuiltIn = out.Source.BuiltIn || l.Source.BuiltIn
		out.Source.Manifest = out.Source.Manifest || l.Source.Manifest
	}
	return out
}

// fromManifest builds the workspace-manifest layer of a Model.
func fromManifest(manifest *warp.ProviderModel, id ID, providerType string) Model {
	var m Model
	m.ID = id
	m.ProviderType = providerType
	if manifest == nil {
		return m
	}
	m.Source.Manifest = true
	// Manifests routinely repeat the slug as `name`, which adds no display
	// information. Leaving it unset lets the built-in profile's human-readable
	// name ("Claude Sonnet 4.6") win the merge, with Merge's identifier
	// fallback restoring the slug when no profile supplies a name. A `name`
	// that differs from the id is a deliberate display override.
	switch {
	case manifest.Name == "":
		m.Name = manifest.Label
	case manifest.Name != manifest.ID:
		m.Name = manifest.Name
	}
	m.Label = manifest.Label
	m.Aliases = dedupeAliases(manifest.Aliases)
	m.Limits = Limits{
		Context:    manifest.Limits.Context,
		Output:     manifest.Limits.Output,
		Dimensions: manifest.Limits.Dimensions,
	}
	if manifest.Capabilities != nil {
		c := manifest.Capabilities
		m.Capabilities = Capabilities{
			Completion:       ptr.Clone(c.Completion),
			Decision:         ptr.Clone(c.Decision),
			Embedding:        ptr.Clone(c.Embedding),
			Tools:            ptr.Clone(c.Tools),
			Attachment:       ptr.Clone(c.Attachment),
			StructuredOutput: ptr.Clone(c.StructuredOutput),
			Temperature:      ptr.Clone(c.Temperature),
			Dimensions:       slices.Clone(c.Dimensions),
			Thinking:         fromThinkingCapabilities(c.Thinking),
		}
		if c.Modalities != nil {
			m.Modalities = Modalities{
				Input:  slices.Clone(c.Modalities.Input),
				Output: slices.Clone(c.Modalities.Output),
			}
		}
	}
	if manifest.Cost != nil {
		m.Pricing = &api.ModelPricing{
			Input:      manifest.Cost.Input,
			Output:     manifest.Cost.Output,
			CacheRead:  manifest.Cost.CacheRead,
			CacheWrite: manifest.Cost.CacheWrite,
		}
		for _, t := range manifest.Cost.Tiers {
			m.Pricing.Tiers = append(m.Pricing.Tiers, api.ModelPricingTier{
				Label:         t.Label,
				ConditionType: t.Tier.Type,
				ConditionSize: t.Tier.Size,
				Input:         t.Input,
				Output:        t.Output,
				CacheRead:     t.CacheRead,
				CacheWrite:    t.CacheWrite,
			})
		}
	}
	return m
}

func fromThinkingCapabilities(in []warp.ProviderModelThinkingCapability) []ThinkingOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]ThinkingOption, len(in))
	for i, t := range in {
		kind := strings.ToLower(strings.TrimSpace(string(t.Type)))
		if kind == "budget" {
			kind = "budget_tokens"
		}
		out[i] = ThinkingOption{
			Type:      kind,
			Values:    slices.Clone(t.AllowedEfforts),
			Default:   thinkingDefaultString(t.Default),
			Max:       t.MaxBudgetTokens,
			IsDefault: t.IsDefault,
		}
	}
	return out
}

// thinkingDefaultString renders a manifest thinking default, which YAML decodes
// into any of the scalar kinds below. A budget default such as `default: 4096`
// arrives as an integer, so it must not be dropped the way a bare type
// assertion on a string would drop it.
func thinkingDefaultString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// dedupeAliases returns the manifest aliases normalized to bare names (WARP
// matches "@fast" and "fast" alike), with duplicates and blanks removed and
// declaration order preserved.
func dedupeAliases(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = appendUnique(out, strings.TrimPrefix(strings.TrimSpace(a), "@"))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appendUnique appends value to out unless an equal entry is already present,
// preserving the order in which names were first seen.
func appendUnique(out []string, value string) []string {
	if value == "" || slices.Contains(out, value) {
		return out
	}
	return append(out, value)
}

func firstNonEmptyID(a, b ID) ID {
	if a != "" {
		return a
	}
	return b
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
