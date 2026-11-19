package command

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type providersCatalogPaneProps struct {
	client   *client.Client
	provider api.WorkspaceProvider
	onBack   func()
	// onAdd commits the picked entries as one manifest write.
	onAdd func(entries []api.ProviderModelRequest, message string)
}

// providersCatalogPane browses the provider type's model catalog (loom
// models.dev metadata, or ollama's live endpoint) and adds the checked
// entries to the manifest.
var providersCatalogPane = trail.FC("ProvidersCatalogPane", func(props providersCatalogPaneProps) trail.Node {
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()
	entries, setEntries := kitex.UseState[[]api.ProviderCatalogEntry](nil)
	loading, setLoading := kitex.UseState(true)
	loadErr, setLoadErr := kitex.UseState("")
	picked, setPicked := kitex.UseState[map[string]bool](nil)

	provider := props.provider
	kitex.UseEffect(func() {
		if props.client == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			list, err := props.client.ListProviderCatalog(ctx, provider.Name)
			if err != nil {
				setLoadErr(err.Error())
			} else {
				setEntries(list)
			}
			setLoading(false)
		}()
	}, []any{provider.Name})

	toggle := func(id string) {
		next := map[string]bool{}
		maps.Copy(next, picked())
		next[id] = !next[id]
		setPicked(next)
	}

	commit := func() {
		var chosen []api.ProviderCatalogEntry
		for _, e := range entries() {
			if picked()[e.ID] && !e.Listed {
				chosen = append(chosen, e)
			}
		}
		if len(chosen) == 0 {
			return
		}
		var reqs []api.ProviderModelRequest
		for _, e := range chosen {
			name := strings.TrimSpace(e.Name)
			if name == "" {
				name = e.ID
			}
			reqs = append(reqs, api.ProviderModelRequest{ID: e.ID, Name: name, Context: e.ContextLimit, Output: e.OutputLimit})
		}
		message := fmt.Sprintf("Added %d model(s) to %s.", len(reqs), provider.Name)
		setPicked(map[string]bool{})
		props.onAdd(reqs, message)
	}

	if loading() {
		return loadingHint("Loading " + provider.Type + " catalog…")
	}

	title := "Catalog · " + provider.Name
	subtitle := provider.Type
	if errText := loadErr(); errText != "" {
		subtitle += " · catalog unavailable: " + errText
	}
	pickCount := 0
	for _, e := range entries() {
		if picked()[e.ID] && !e.Listed {
			pickCount++
		}
	}
	if pickCount > 0 {
		subtitle += fmt.Sprintf(" · %d selected (ctrl+s adds)", pickCount)
	}

	options := make([]uicomponents.Option, 0, len(entries()))
	for _, e := range entries() {
		label := strings.TrimSpace(e.Name)
		if label == "" {
			label = e.ID
		}
		var parts []string
		if e.ContextLimit > 0 {
			parts = append(parts, format.Tokens(e.ContextLimit)+" ctx")
		}
		if e.Pricing != nil && (e.Pricing.Input > 0 || e.Pricing.Output > 0) {
			parts = append(parts, fmt.Sprintf("$%s/$%s /M", trimPrice(e.Pricing.Input), trimPrice(e.Pricing.Output)))
		}
		if e.Listed {
			parts = append(parts, "declared")
		} else if picked()[e.ID] {
			parts = append(parts, "selected")
		}
		icon := "  "
		iconStyle := style.S().Foreground(scheme.Color.Text.Muted)
		if e.Listed {
			icon = "✔"
			iconStyle = style.S().Foreground(scheme.Color.Text.Muted)
		} else if picked()[e.ID] {
			icon = "☑"
			iconStyle = style.S().Foreground(scheme.Color.Text.Success)
		}
		options = append(options, uicomponents.Option{
			Value:       e.ID,
			Label:       label,
			Description: strings.Join(parts, " · "),
			Search:      e.ID + " " + e.Description,
			Cells:       []string{label, strings.Join(parts, " · ")},
			Icon:        mcpIconNode(icon, iconStyle),
			Current:     false,
		})
	}
	if len(options) == 0 {
		hint := "press n in the model list to enter a custom id"
		if errText := loadErr(); errText != "" {
			hint = "catalog unavailable (" + errText + ") — press n in the model list to enter a custom id"
		}
		options = []uicomponents.Option{{Value: emptyModelsRow, Label: "(no catalog entries — " + hint + ")"}}
	}

	pickerProps := components.NewPicker(title, options, func(opt uicomponents.Option) {
		if opt.Value == emptyModelsRow {
			return
		}
		for _, e := range entries() {
			if e.ID == opt.Value && !e.Listed {
				toggle(e.ID)
				return
			}
		}
	})
	pickerProps.Subtitle = subtitle
	pickerProps.Filterable = true
	// The catalog holds hundreds of entries, so the filter owns every printable
	// key; committing runs on the ctrl+s chord and toggling on enter.
	pickerProps.FilterSafeKeys = true
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0, Align: style.TextAlignLeft},
		{Width: min(max(winSize.Width-34, 30), 56), Align: style.TextAlignLeft},
	}
	pickerProps.Footer = func(opt uicomponents.Option) string {
		for _, e := range entries() {
			if e.ID == opt.Value {
				return format.CompactSingleLine(strings.TrimSpace(e.Description), 100)
			}
		}
		return ""
	}
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "type", Label: "Filter"},
		{Key: "enter", Label: "Toggle"},
		{Key: "ctrl+s", Label: "Add selected"},
		{Key: "esc", Label: "Back"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"ctrl+s": func(_ uicomponents.Option) { commit() },
	}
	pickerProps.OnCancel = func() {
		if props.onBack != nil {
			props.onBack()
		}
	}

	return components.Picker(pickerProps)
})
