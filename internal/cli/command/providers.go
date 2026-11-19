package command

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerProviderCommands() {
	r.Register(Command{
		Name:        "providers",
		Aliases:     []string{"provider"},
		Usage:       "/providers",
		Description: "Browse, toggle, and manage configured model providers",
		Handler:     handleProviders,
	})
}

func handleProviders(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/providers does not accept arguments (run /providers to browse and manage configured providers)")
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return providersBrowser(providersBrowserProps{slotID: slotID})
		},
	})
	return nil
}

// providerStatusIcon renders the browser row glyph for a provider status.
func providerStatusIcon(status string) string {
	switch status {
	case "active":
		return "✔"
	case "local":
		return "●"
	case "missing_auth":
		return "⚠"
	default:
		return "○"
	}
}

func providerStatusIconStyle(scheme *theme.Scheme, status string) style.Style {
	switch status {
	case "active":
		return style.S().Foreground(scheme.Color.Text.Success)
	case "local":
		return style.S().Foreground(scheme.Color.Text.Info)
	case "missing_auth":
		return style.S().Foreground(scheme.Color.Text.Warning)
	default:
		return style.S().Foreground(scheme.Color.Text.Muted)
	}
}

func providerStatusLabel(status string) string {
	switch status {
	case "active":
		return "Active"
	case "local":
		return "Local"
	case "disabled":
		return "Disabled"
	case "missing_auth":
		return "Missing Auth"
	default:
		return status
	}
}

func providerStatusRowStyle(scheme *theme.Scheme, status string) style.Style {
	switch status {
	case "missing_auth":
		return style.S().Foreground(scheme.Color.Text.Warning)
	default:
		return style.S().Foreground(scheme.Color.Text.Muted)
	}
}

// providerScopeName maps a provider scope to its display badge name.
func providerScopeName(p api.WorkspaceProvider) string {
	switch strings.ToLower(strings.TrimSpace(p.Scope)) {
	case "user":
		return "User"
	case "workspace":
		return "Workspace"
	case "local":
		return "Local"
	case "system":
		return "Builtin"
	default:
		return "Workspace"
	}
}

var providerScopeOrder = []string{"Workspace", "Local", "User", "Builtin"}

func providerScopeRank(p api.WorkspaceProvider) int {
	name := providerScopeName(p)
	if idx := slices.Index(providerScopeOrder, name); idx >= 0 {
		return idx
	}
	return len(providerScopeOrder)
}

// providerGroupOrders providers workspace-scope first, then by name.
func providerGroupOrders(providers []api.WorkspaceProvider) []api.WorkspaceProvider {
	ordered := slices.Clone(providers)
	slices.SortStableFunc(ordered, func(a, b api.WorkspaceProvider) int {
		if rank := providerScopeRank(a) - providerScopeRank(b); rank != 0 {
			return rank
		}
		return strings.Compare(a.Name, b.Name)
	})
	return ordered
}

// providerRowStatus summarizes a provider row's right column: status word,
// endpoint/type detail, and model count.
func providerRowStatus(p api.WorkspaceProvider) string {
	label := providerStatusLabel(p.Status)
	// "(shadow)" marks a mute: only a disabled workspace override that shadows
	// an enabled inherited manifest earns it — an enabled full override is just
	// that provider's live config.
	if p.Shadowed && !p.Enabled {
		label += " (shadow)"
	}
	detail := strings.TrimSpace(p.Type)
	if detail == "" {
		detail = "provider"
	}
	if endpoint := strings.TrimSpace(p.Endpoint); endpoint != "" && p.Status == "missing_auth" {
		detail = endpoint
	}
	count := len(p.Models)
	models := "no models"
	if count == 1 {
		models = "1 model"
	} else if count > 1 {
		models = fmt.Sprintf("%d models", count)
	}
	return fmt.Sprintf("%-12s %s · %s", label, detail, models)
}

// providerScopeHeading names one scope group with the directory its manifests
// live in, taken from the first provider in the group.
func providerScopeHeading(p api.WorkspaceProvider) string {
	title := providerScopeName(p) + " providers"
	manifest := strings.TrimSpace(p.Path)
	if manifest == "" {
		return title
	}
	dir := path.Dir(filepath.ToSlash(manifest))
	if dir == "." || dir == "/" || dir == "" {
		return title
	}
	return title + " (" + components.ShortenHomePath(filepath.FromSlash(dir)) + ")"
}

func providerByName(list []api.WorkspaceProvider, name string) api.WorkspaceProvider {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return api.WorkspaceProvider{Name: name}
}

func providerCountSubtitle(count int) string {
	if count == 1 {
		return "1 provider"
	}
	return fmt.Sprintf("%d providers", count)
}

type providersBrowserProps struct {
	slotID string
}

type providersPane int

const (
	providersPaneBrowser providersPane = iota
	providersPaneModels
	providersPaneCatalog
	providersPaneAdd
	providersPaneAlias
)

type providersBrowserState struct {
	pane     providersPane
	provider *api.WorkspaceProvider
	model    *api.ProviderModelInfo
}

// providersBrowser renders the interactive provider browser inside <Slot />.
var providersBrowser = trail.FC("ProvidersBrowser", func(props providersBrowserProps) trail.Node {
	c := store.Client()
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	providers, setProviders := kitex.UseState[[]api.WorkspaceProvider](nil)
	loading, setLoading := kitex.UseState(true)
	notice, setNotice := kitex.UseState("")
	busy, setBusy := kitex.UseState("")
	state, setState := kitex.UseState(providersBrowserState{})

	clientReady := c != nil
	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(props.slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			list, err := c.ListWorkspaceProviders(ctx)
			if err != nil {
				setNotice("Failed to load providers: " + err.Error())
			} else {
				setProviders(list)
			}
			setLoading(false)
		}()
	}, []any{clientReady})

	runBusy := func(key string, fn func(ctx context.Context, report func(string))) {
		if busy() != "" || c == nil {
			return
		}
		setBusy(key)
		goMCPBackground(func() {
			defer setBusy("")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fn(ctx, setNotice)
		})
	}

	// liveProvider re-resolves the pane's provider by name so panes always show
	// the state after the most recent mutation, not the snapshot taken on entry.
	liveProvider := func(p *api.WorkspaceProvider) (api.WorkspaceProvider, bool) {
		for _, candidate := range providers() {
			if candidate.Name == p.Name {
				return candidate, true
			}
		}
		return *p, false
	}

	toggleEnabled := func(p api.WorkspaceProvider) {
		if !p.Writable && !p.Overridable {
			setNotice("Provider " + p.Name + " is not writable from this workspace.")
			return
		}
		target := p.Enabled
		runBusy("enable:"+p.Name, func(ctx context.Context, report func(string)) {
			list, err := c.SetProviderEnabled(ctx, p.Name, !target)
			if err != nil {
				report("Failed to update " + p.Name + ": " + err.Error())
				return
			}
			setProviders(list)
			refreshProviderWorkspaceContext(ctx)
			state := "enabled"
			if target {
				state = "disabled"
			}
			report("Provider " + p.Name + " " + state + ".")
		})
	}

	// refetchProviders refreshes the listing after a pane mutation that returns
	// no body of its own.
	refetchProviders := func() {
		if c == nil {
			return
		}
		goMCPBackground(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			list, err := c.ListWorkspaceProviders(ctx)
			if err != nil {
				return
			}
			setProviders(list)
			refreshProviderWorkspaceContext(ctx)
		})
	}

	current := state()

	switch current.pane {
	case providersPaneModels:
		if current.provider == nil {
			break
		}
		live, ok := liveProvider(current.provider)
		if !ok {
			setState(providersBrowserState{})
			return nil
		}
		return providersModelsPane(providersModelsPaneProps{
			provider: live,
			busy:     busy(),
			notice:   notice(),
			onBack:   func() { setNotice(""); setState(providersBrowserState{}) },
			onCatalog: func() {
				setNotice("")
				setState(providersBrowserState{pane: providersPaneCatalog, provider: &live})
			},
			onSave: func(remove []string, add []api.ProviderModelRequest, defaultModel string) {
				runBusy("models:"+live.Name, func(ctx context.Context, report func(string)) {
					removed := make(map[string]bool, len(remove))
					for _, id := range remove {
						removed[id] = true
					}
					var models []api.ProviderModelRequest
					for _, m := range live.Models {
						if removed[m.ID] {
							continue
						}
						models = append(models, api.ProviderModelRequest(m))
					}
					models = append(models, add...)
					req := api.UpdateProviderModelsRequest{Models: models}
					if defaultModel != "" {
						req.DefaultModel = defaultModel
						req.DefaultSet = true
					}
					list, err := c.UpdateProviderModels(ctx, live.Name, req)
					if err != nil {
						report("Failed to update models: " + err.Error())
						return
					}
					setProviders(list)
					refreshProviderWorkspaceContext(ctx)
					updated := providerByName(list, live.Name)
					setState(providersBrowserState{pane: providersPaneModels, provider: &updated})
					report("Models updated for " + live.Name + ".")
				})
			},
			onAliases: func(m api.ProviderModelInfo) {
				setState(providersBrowserState{pane: providersPaneAlias, provider: &live, model: &m})
			},
		})
	case providersPaneCatalog:
		if current.provider == nil {
			break
		}
		live, ok := liveProvider(current.provider)
		if !ok {
			setState(providersBrowserState{pane: providersPaneModels, provider: current.provider})
			return nil
		}
		return providersCatalogPane(providersCatalogPaneProps{
			client:   c,
			provider: live,
			onBack:   func() { setState(providersBrowserState{pane: providersPaneModels, provider: &live}) },
			onAdd: func(entries []api.ProviderModelRequest, message string) {
				runBusy("models:"+live.Name, func(ctx context.Context, report func(string)) {
					models := make([]api.ProviderModelRequest, 0, len(live.Models)+len(entries))
					for _, m := range live.Models {
						models = append(models, api.ProviderModelRequest(m))
					}
					models = append(models, entries...)
					list, err := c.UpdateProviderModels(ctx, live.Name, api.UpdateProviderModelsRequest{Models: models})
					if err != nil {
						report("Failed to add models: " + err.Error())
						return
					}
					setProviders(list)
					refreshProviderWorkspaceContext(ctx)
					updated := providerByName(list, live.Name)
					setNotice(message)
					setState(providersBrowserState{pane: providersPaneModels, provider: &updated})
				})
			},
		})
	case providersPaneAdd:
		return providersAddPane(providersAddPaneProps{
			client: c,
			onBack: func() { setNotice(""); setState(providersBrowserState{}) },
			onCreated: func(created api.WorkspaceProvider, verdict string) {
				store.RemoveSlot(props.slotID)
				refreshProviderWorkspaceContext(context.Background())
				store.AppendCommandOutput("/providers", "Added provider "+created.Name+" ("+providerStatusLabel(created.Status)+")"+verdict+".")
			},
		})
	case providersPaneAlias:
		if current.provider == nil || current.model == nil {
			break
		}
		live, ok := liveProvider(current.provider)
		if !ok {
			setState(providersBrowserState{pane: providersPaneModels, provider: current.provider})
			return nil
		}
		return modelAliasEditorSlot(modelAliasEditorSlotProps{
			ProviderName: live.Name,
			ModelID:      current.model.ID,
			ModelName:    firstNonEmpty(current.model.Label, current.model.Name, current.model.ID),
			onDone: func(message string, err error) {
				refetchProviders()
				if err != nil {
					setNotice(err.Error())
					return
				}
				// Land back on the models pane so the save confirmation is
				// visible instead of hidden behind the editor.
				setNotice(message)
				setState(providersBrowserState{pane: providersPaneModels, provider: &live})
			},
			onCancel: func() {
				setState(providersBrowserState{pane: providersPaneModels, provider: &live})
			},
		})
	}

	if loading() {
		return loadingHint("Loading providers…")
	}
	if c == nil {
		return nil
	}

	if len(providers()) == 0 {
		message := "No providers configured. Press a to add one."
		if note := notice(); note != "" {
			message = note
		}
		return mcpEmptyNotice(mcpEmptyNoticeProps{
			title:   "Manage model providers (0 configured)",
			message: message,
			action: func() {
				store.RemoveSlot(props.slotID)
				store.AppendCommandOutput("/providers", "Exited /providers command")
			},
			onAdd:    func() { setNotice(""); setState(providersBrowserState{pane: providersPaneAdd}) },
			addLabel: "Add provider",
		})
	}

	listed := providerGroupOrders(providers())
	headings := make(map[string]string, len(providerScopeOrder))
	options := make([]uicomponents.Option, len(listed))
	for i, p := range listed {
		scope := providerScopeName(p)
		if _, seen := headings[scope]; !seen {
			headings[scope] = providerScopeHeading(p)
		}
		status := providerRowStatus(p)
		options[i] = uicomponents.Option{
			Value:       p.Name,
			Label:       p.Name,
			Description: status,
			Search:      p.Type + " " + p.Endpoint,
			Cells:       []string{p.Name, status},
			Icon:        mcpIconNode(providerStatusIcon(p.Status), providerStatusIconStyle(scheme, p.Status)),
			CellNodes: []trail.Node{
				nil,
				mcpBadgeNode(status, providerStatusRowStyle(scheme, p.Status)),
			},
		}
	}

	title := "Manage model providers"
	subtitle := providerCountSubtitle(len(listed))
	if note := strings.TrimSpace(notice()); note != "" {
		subtitle = subtitle + " · " + note
	}

	enterModels := func(name string) {
		p := providerByName(listed, name)
		setNotice("")
		setState(providersBrowserState{pane: providersPaneModels, provider: &p})
	}

	pickerProps := components.NewPicker(title, options, func(opt uicomponents.Option) {
		enterModels(opt.Value)
	})
	pickerProps.Subtitle = subtitle
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0, Align: style.TextAlignLeft},
		{Width: min(max(winSize.Width-34, 30), 56), Align: style.TextAlignLeft},
	}
	pickerProps.GroupLabel = func(opt uicomponents.Option) string {
		for _, p := range listed {
			if p.Name == opt.Value {
				return headings[providerScopeName(p)]
			}
		}
		return ""
	}
	// The browser stays unfiltered so printable keys reach the actions below,
	// mirroring the /mcp browser's first screen.
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Manage models"},
		{Key: "space / e", Label: "Enable/Disable"},
		{Key: "m", Label: "Manage models"},
		{Key: "a / n", Label: "Add provider"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"space": func(opt uicomponents.Option) { toggleEnabled(providerByName(listed, opt.Value)) },
		"e":     func(opt uicomponents.Option) { toggleEnabled(providerByName(listed, opt.Value)) },
		"m":     func(opt uicomponents.Option) { enterModels(opt.Value) },
		"a":     func(_ uicomponents.Option) { setNotice(""); setState(providersBrowserState{pane: providersPaneAdd}) },
		"n":     func(_ uicomponents.Option) { setNotice(""); setState(providersBrowserState{pane: providersPaneAdd}) },
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(props.slotID)
		store.AppendCommandOutput("/providers", "Exited /providers command")
	}

	return components.Picker(pickerProps)
})
