package highlight

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
	"github.com/masterkeysrd/tasksmith/internal/tui/styles"
)

// styleMap is a map of highlight group names to Kite styles.
type styleMap map[string]style.Style

// StyleState wraps the style map in a pointer to satisfy comparable for kitex context.
type StyleState struct {
	styles styleMap
}

// highlightCtx is the Kite context used to propagate theme styles.
var highlightCtx = kitex.CreateContext[*StyleState](nil)

// Props defines the properties for the highlight Provider.
type Props struct {
	Theme *colorscheme.Colorscheme
}

// Provider is a Kite component that resolves a colorscheme and provides
// the resulting styles to its children.
func Provider(props Props, children ...kitex.Node) kitex.Node {
	// Resolve colorscheme and build styles
	resolved := colorscheme.Resolve(props.Theme)
	built := styles.BuildFrom(resolved)
	if built == nil {
		built = make(styleMap)
	}

	state := &StyleState{
		styles: styleMap(built),
	}

	return highlightCtx.Provider(state, children...)
}

// Use returns the current style for the given highlight group.
// It is a Kite hook and must be called within a component's render function.
func Use(group Group) style.Style {
	state := kitex.UseContext(highlightCtx)
	if state == nil || state.styles == nil {
		return style.S()
	}

	if s, ok := state.styles[group.name]; ok {
		return s
	}

	return style.S()
}
