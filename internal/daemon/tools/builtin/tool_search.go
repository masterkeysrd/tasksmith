package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// NewToolSearch constructs the built-in tool_search tool. It is backed by
// Loom's BM25 keyword index over the deferred (DeferLoading) tools'
// definitions and returns the matching tool names as references to be loaded
// into the model's active toolset. It is registered by the tool container
// whenever the session resolves at least one deferred tool.
//
// When the workspace catalog provides the tool_search manifest, its full
// <how_it_works> / <when_to_use> instructions and safety annotations are used as
// the tool description passed to the LLM, matching the built-in tool wiring in
// [New]. The built-in defaults are used when the catalog has no entry.
func NewToolSearch(defs []tool.Definition, ws *workspace.Workspace) tools.Tool {
	loomTools := make([]*tool.Tool, 0, len(defs))
	for i := range defs {
		loomTools = append(loomTools, &tool.Tool{Definition: defs[i]})
	}

	bm25, err := tool.NewBM25Search(
		loomTools,
		tool.WithSearchName(ToolToolSearch),
		tool.WithSearchTopK(5),
	)
	if err != nil {
		bm25 = nil
	}

	title := "Tool Search"
	description := "Search available (deferred) tools by name, description, or parameters and load the matches into context."
	annotation := tool.Annotation{
		IsReadOnly:   true,
		IsIdempotent: true,
		UserHint:     "Search available tools",
	}

	if ws != nil && ws.Catalog() != nil {
		if wsTool, ok := ws.Catalog().GetTool(ToolToolSearch); ok {
			if wsTool.Metadata.DisplayName != "" {
				title = wsTool.Metadata.DisplayName
			} else if wsTool.Metadata.Name != "" {
				title = wsTool.Metadata.Name
			}
			if wsTool.Spec.Instructions != "" {
				description = wsTool.Spec.Instructions
			} else {
				description = wsTool.Metadata.Description
			}
			if wsTool.Spec.Annotations != nil {
				annotation = tool.Annotation{
					IsOpenWorld:  wsTool.Spec.Annotations.IsOpenWorld,
					IsDangerous:  wsTool.Spec.Annotations.IsDangerous,
					IsReadOnly:   wsTool.Spec.Annotations.IsReadOnly,
					IsIdempotent: wsTool.Spec.Annotations.IsIdempotent,
					UserHint:     wsTool.Spec.Annotations.UserHint,
				}
			}
		}
	}

	return tools.NewHandler(ToolToolSearch, tools.Metadata{
		Title:       title,
		Description: description,
		Annotation:  annotation,
	}, &toolSearchHandler{bm25: bm25})
}

// toolSearchHandler is the two-phase handler for the tool_search tool. It
// delegates keyword matching to Loom's BM25 tool and collects the returned
// tool-reference names.
type toolSearchHandler struct {
	bm25 *tool.Tool
}

func (h *toolSearchHandler) Prepare(_ context.Context, in api.ToolSearchInput) (*tools.Plan, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", std.ErrInvalidArgument)
	}

	return &tools.Plan{
		ToolName: ToolToolSearch,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Summary:  fmt.Sprintf("Search tools for %q", query),
		ReadOnly: true,
	}, nil
}

func (h *toolSearchHandler) Execute(ctx context.Context, in api.ToolSearchInput) (api.ToolSearchOutput, error) {
	if h.bm25 == nil {
		return api.ToolSearchOutput{}, nil
	}

	res, err := tool.NewContainer(h.bm25).Call(ctx, &message.ToolCall{
		Name: ToolToolSearch,
		Args: map[string]any{"query": in.Query},
	})
	if err != nil {
		return api.ToolSearchOutput{}, err
	}

	var out api.ToolSearchOutput
	for _, b := range res.Content {
		if ref, ok := b.(*message.ToolReferenceBlock); ok {
			out.Tools = append(out.Tools, ref.ToolName)
		}
	}
	return out, nil
}
