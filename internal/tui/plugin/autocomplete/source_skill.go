package autocomplete

import (
	"context"
)

// SkillSearchResult represents a skill match found by query.
type SkillSearchResult struct {
	Name        string
	Description string
}

// SkillSource implements autocomplete.Source for agent skills.
type SkillSource struct {
	queryFn func(ctx context.Context, sessionID, query string) ([]SkillSearchResult, error)
}

// NewSkillSource instantiates a new SkillSource with a query function.
func NewSkillSource(queryFn func(ctx context.Context, sessionID, query string) ([]SkillSearchResult, error)) *SkillSource {
	return &SkillSource{
		queryFn: queryFn,
	}
}

// Name returns the identifier for this source.
func (s *SkillSource) Name() string {
	return "skill"
}

// Query performs a skill search and maps the results to Items.
func (s *SkillSource) Query(ctx context.Context, req QueryReq) ([]Item, error) {
	results, err := s.queryFn(ctx, req.SessionID, req.Query)
	if err != nil {
		return nil, err
	}

	var items []Item
	for _, r := range results {
		items = append(items, Item{
			ID:          r.Name,
			Label:       r.Name,
			Sublabel:    r.Description,
			Badge:       "SKILL",
			Kind:        "skill",
			InsertValue: "@skill:" + r.Name,
		})
	}

	return items, nil
}
