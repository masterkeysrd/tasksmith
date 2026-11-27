package command

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/masterkeysrd/kite/extras/flight"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
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

func providerStatusBadge(status string) trail.Node {
	badge := uicomponents.BadgeProps{Icon: "○", Tone: uicomponents.ToneMuted}
	switch status {
	case "active":
		badge.Icon, badge.Tone = "✔", uicomponents.ToneSuccess
	case "local":
		badge.Icon, badge.Tone = "●", uicomponents.ToneAccent
	case "missing_auth", "expired_auth", "invalid_auth", "invalid_config":
		badge.Icon, badge.Tone = "⚠", uicomponents.ToneWarning
	}
	return uicomponents.Badge(badge)
}

func providerStatusLabel(status string) string {
	switch status {
	case "active":
		return "Ready"
	case "local":
		return "Local"
	case "disabled":
		return "Disabled"
	case "missing_auth":
		return "Needs sign-in"
	case "expired_auth":
		return "Session expired"
	case "invalid_auth":
		return "Invalid key"
	case "invalid_config":
		return "Invalid config"
	default:
		return status
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

func providerRowStatus(p api.WorkspaceProvider) string {
	if p.Shadowed && !p.Enabled {
		return "Disabled (shadow)"
	}
	return providerStatusLabel(p.Status)
}

func providerModelCount(models []api.ProviderModelInfo) string {
	if models == nil {
		return ""
	}
	if len(models) == 1 {
		return "1 model"
	}
	return fmt.Sprintf("%d models", len(models))
}

// providerScopeHeading names one scope group with the directory its manifests
// live in, taken from the first provider in the group.
func providerScopeHeading(p api.WorkspaceProvider) string {
	title := providerScopeName(p)
	manifest := strings.TrimSpace(p.Path)
	if manifest == "" {
		return title
	}
	dir := path.Dir(filepath.ToSlash(manifest))
	if dir == "." || dir == "/" || dir == "" {
		return title
	}
	return title + " · " + components.ShortenHomePath(filepath.FromSlash(dir))
}

func providerByName(list []api.WorkspaceProvider, name string) api.WorkspaceProvider {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return api.WorkspaceProvider{Name: name}
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
	providersPaneAuth
)

type providerRoute struct {
	pane     providersPane
	provider string
	model    string
}

type providersScreenProps struct {
	slotID string
	route  providerRoute
}

var providersBrowser = trail.FC("ProvidersBrowser", func(props providersBrowserProps) trail.Node {
	common := styles.UseCommon()
	return flight.Stack(flight.StackProps{
		InitialRoute: providerRoute{pane: providersPaneBrowser},
		RenderRoute: func(route flight.Route) trail.Node {
			return trail.Box(trail.BoxProps{Style: common.Column}, providersScreen(providersScreenProps{
				slotID: props.slotID, route: route.(providerRoute),
			}))
		},
	})
})

var providersScreen = trail.FC("ProvidersScreen", func(props providersScreenProps) trail.Node {
	nav := flight.UseNavigation()
	c := store.Client()

	providers, setProviders := kitex.UseState[[]api.WorkspaceProvider](nil)
	loading, setLoading := kitex.UseState(true)
	notice, setNotice := kitex.UseState("")
	busy, setBusy := kitex.UseState("")

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

	route := props.route
	leftRoute := kitex.UseMemo(func() *atomic.Bool { return new(atomic.Bool) }, []any{route})
	kitex.UseLayoutEffectCleanup(func() func() { return func() { leftRoute.Store(true) } }, []any{route})
	popAfterSave := func() {
		if !leftRoute.Load() {
			nav.Pop()
		}
	}
	live := providerByName(providers(), route.provider)
	back := func() { setNotice(""); nav.Pop() }
	open := func(pane providersPane, provider string) {
		setNotice("")
		nav.Push(providerRoute{pane: pane, provider: provider})
	}
	missing := route.provider != "" && !slices.ContainsFunc(providers(), func(p api.WorkspaceProvider) bool { return p.Name == route.provider })
	kitex.UseEffect(func() {
		if missing {
			nav.Pop()
		}
	}, []any{route, missing})
	if missing {
		return nil
	}
	updateModels := func(remove []string, add []api.ProviderModelRequest, defaultModel, message string) {
		runBusy("models:"+live.Name, func(ctx context.Context, report func(string)) {
			req := api.UpdateProviderModelsRequest{DefaultModel: defaultModel, DefaultSet: defaultModel != ""}
			for _, m := range live.Models {
				if !slices.Contains(remove, m.ID) {
					req.Models = append(req.Models, api.ProviderModelRequest(m))
				}
			}
			req.Models = append(req.Models, add...)
			list, err := c.UpdateProviderModels(ctx, live.Name, req)
			if err != nil {
				prefix := "Failed to update models: "
				if route.pane == providersPaneCatalog {
					prefix = "Failed to add models: "
				}
				report(prefix + err.Error())
				return
			}
			setProviders(list)
			refreshProviderWorkspaceContext(ctx)
			report(message)
			if route.pane == providersPaneCatalog {
				popAfterSave()
			}
		})
	}
	switch route.pane {
	case providersPaneModels:
		return providersModelsPane(providersModelsPaneProps{
			provider:  live,
			busy:      busy(),
			notice:    notice(),
			onBack:    back,
			onCatalog: func() { open(providersPaneCatalog, live.Name) },
			onSave: func(remove []string, add []api.ProviderModelRequest, defaultModel string) {
				updateModels(remove, add, defaultModel, "Models updated for "+live.Name+".")
			},
			onAliases: func(m api.ProviderModelInfo) {
				nav.Push(providerRoute{pane: providersPaneAlias, provider: live.Name, model: m.ID})
			},
		})
	case providersPaneCatalog:
		return providersCatalogPane(providersCatalogPaneProps{
			client: c, provider: live, onBack: nav.Pop,
			onAdd: func(entries []api.ProviderModelRequest, message string) {
				updateModels(nil, entries, "", message)
			},
		})
	case providersPaneAdd:
		return providersAddPane(providersAddPaneProps{
			client: c,
			onBack: back,
			onCreated: func(created api.WorkspaceProvider, verdict string) {
				store.RemoveSlot(props.slotID)
				refreshProviderWorkspaceContext(context.Background())
				store.AppendCommandOutput("/providers", "Added provider "+created.Name+" ("+providerStatusLabel(created.Status)+")"+verdict+".")
				store.AppendSkippedPresetAliasNotices(created.SkippedPresetAliases)
			},
		})
	case providersPaneAlias:
		name := route.model
		for _, m := range live.Models {
			if m.ID == route.model {
				name = firstNonEmpty(m.Label, m.Name, m.ID)
				break
			}
		}
		return modelAliasEditorSlot(modelAliasEditorSlotProps{
			ProviderName: live.Name,
			ModelID:      route.model,
			ModelName:    name,
			onDone: func(message string, err error) {
				refetchProviders()
				if err != nil {
					setNotice(err.Error())
					return
				}
				// Land back on the models pane so the save confirmation is
				// visible instead of hidden behind the editor.
				setNotice(message)
				popAfterSave()
			},
			onCancel: nav.Pop,
		})
	case providersPaneAuth:
		return providerAuthView(providerAuthViewProps{
			slotID:   props.slotID,
			provider: route.provider,
			onSettled: func(_ bool, message string) {
				refetchProviders()
				setNotice(message)
				popAfterSave()
			},
			onCancel: func(inFlight bool) {
				if inFlight {
					setNotice("Left sign-in screen; browser flow continues in background.")
				}
				nav.Pop()
			},
		})
	}

	if loading() {
		return loadingHint("Loading providers…")
	}
	if c == nil {
		return nil
	}

	closeBrowser := func() {
		store.RemoveSlot(props.slotID)
		store.AppendCommandOutput("/providers", "Exited /providers command")
	}
	addProvider := func() { open(providersPaneAdd, "") }
	if len(providers()) == 0 {
		return emptyProviderList(emptyProviderListProps{notice: notice(), add: addProvider, close: closeBrowser})
	}

	listed := providerGroupOrders(providers())
	headings := make(map[string]string, len(providerScopeOrder))
	options := make([]uicomponents.Option, len(listed))
	statusWidth := len("Disabled (shadow)") + 2
	countWidth := len("10 models")
	for i, p := range listed {
		scope := providerScopeName(p)
		if _, seen := headings[scope]; !seen {
			headings[scope] = providerScopeHeading(p)
		}
		status, count := providerRowStatus(p), providerModelCount(p.Models)
		statusWidth = max(statusWidth, len(status)+2)
		countWidth = max(countWidth, len(count))
		options[i] = uicomponents.Option{
			Value: p.Name,
			Label: p.Name,
			Cells: []string{p.Name, status, count},
			Icon:  providerStatusBadge(p.Status),
		}
	}

	enterModels := func(name string) {
		open(providersPaneModels, name)
	}

	pickerProps := components.NewPicker("Manage model providers", options, func(opt uicomponents.Option) {
		enterModels(opt.Value)
	})
	pickerProps.Subtitle = strings.TrimSpace(notice())
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0, Align: style.TextAlignLeft},
		{Width: statusWidth, Align: style.TextAlignLeft},
		{Width: countWidth, Align: style.TextAlignRight},
	}
	pickerProps.Footer = func(opt uicomponents.Option) string {
		p := providerByName(listed, opt.Value)
		fields := slices.DeleteFunc([]string{p.Type, p.Account, p.Endpoint}, func(field string) bool { return field == "" })
		return strings.Join(fields, " · ")
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
		{Key: "l", Label: "Sign in"},
		{Key: "m", Label: "Manage models"},
		{Key: "a / n", Label: "Add provider"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"space": func(opt uicomponents.Option) { toggleEnabled(providerByName(listed, opt.Value)) },
		"e":     func(opt uicomponents.Option) { toggleEnabled(providerByName(listed, opt.Value)) },
		"l": func(opt uicomponents.Option) {
			p := providerByName(listed, opt.Value)
			if !strings.EqualFold(p.AuthType, "oauth2") {
				setNotice("Provider " + p.Name + " does not use OAuth sign-in.")
				return
			}
			open(providersPaneAuth, p.Name)
		},
		"m": func(opt uicomponents.Option) { enterModels(opt.Value) },
		"a": func(_ uicomponents.Option) { addProvider() },
		"n": func(_ uicomponents.Option) { addProvider() },
	}
	pickerProps.OnCancel = closeBrowser

	return components.Picker(pickerProps)
})

type emptyProviderListProps struct {
	notice     string
	add, close func()
}

var emptyProviderList = trail.FC("EmptyProviderList", func(props emptyProviderListProps) trail.Node {
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"a", "n"}, Label: "Add provider", OnPress: props.add},
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: props.close},
	}, true)
	return uicomponents.Panel(uicomponents.PanelProps{
		Title:    "Manage model providers",
		Notice:   firstNonEmpty(props.notice, "No providers configured. Press a to add one."),
		Bindings: bindings,
	})
})
