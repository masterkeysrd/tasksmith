package command

import (
	"cmp"
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

type providersModelsPaneProps struct {
	provider api.WorkspaceProvider
	busy     string
	notice   string
	onBack   func()
	// onCatalog opens the provider-type catalog to pick additional models.
	onCatalog func()
	// onSave commits the staged edits: model ids to remove, entries to add,
	// and the model id to make the manifest default ("" keeps it).
	onSave    func(remove []string, add []api.ProviderModelRequest, defaultModel string)
	onAliases func(model api.ProviderModelInfo)
}

// modelsPaneStaged is the pending edit set of the models pane. Removals and the
// pending default are marked in-place; additions commit immediately through
// onSave (the catalog pane and the custom-ID prompt each own their write).
type modelsPaneStaged struct {
	remove       map[string]bool
	defaultModel string
}

// providersModelsPane lists a provider's declared models with per-row alias
// editing (f2), removal marking (space), default selection (d), a custom model
// id prompt (n), catalog browsing (c), and saving (s).
var providersModelsPane = trail.FC("ProvidersModelsPane", func(props providersModelsPaneProps) trail.Node {
	width := trail.UseWindowSize().Width
	staged, setStaged := kitex.UseState(modelsPaneStaged{remove: map[string]bool{}})

	provider := props.provider
	writable := provider.Writable
	rows := provider.Models

	// A save (or catalog add) swaps in the refreshed provider; drop staged
	// marks so removals and the pending default never replay against the new
	// listing.
	sigParts := make([]string, 0, len(rows)+1)
	sigParts = append(sigParts, provider.DefaultModel)
	for _, m := range rows {
		sigParts = append(sigParts, m.ID)
	}
	modelSignature := strings.Join(sigParts, "|")
	kitex.UseEffect(func() {
		setStaged(modelsPaneStaged{remove: map[string]bool{}})
	}, []any{modelSignature})

	cloneStaged := func() modelsPaneStaged {
		next := modelsPaneStaged{remove: map[string]bool{}, defaultModel: staged().defaultModel}
		maps.Copy(next.remove, staged().remove)
		return next
	}

	toggleRemove := func(id string) {
		next := cloneStaged()
		next.remove[id] = !staged().remove[id]
		setStaged(next)
	}

	markDefault := func(id string) {
		next := cloneStaged()
		next.defaultModel = id
		setStaged(next)
	}

	save := func() {
		var remove []string
		for id, marked := range staged().remove {
			if marked {
				remove = append(remove, id)
			}
		}
		props.onSave(remove, nil, staged().defaultModel)
	}

	options := make([]uicomponents.Option, 0, len(rows))
	var idWidth, detailWidth int
	for _, m := range rows {
		aliasText := strings.Join(modelAliasRefs(m.Aliases), " ")
		capacity := ""
		if m.Context > 0 {
			capacity = format.Tokens(m.Context) + " ctx"
		}
		marked := staged().remove[m.ID]
		pendingDefault := staged().defaultModel == m.ID
		isDefault := pendingDefault || (staged().defaultModel == "" && m.ID == provider.DefaultModel)

		right := aliasText
		switch {
		case right == "":
			right = capacity
		case capacity != "":
			right = capacity + " · " + right
		}
		label := cmp.Or(strings.TrimSpace(m.Label), strings.TrimSpace(m.Name), m.ID)
		if isDefault {
			label += " (default)"
		}
		if marked {
			label += " (will remove)"
		}
		idWidth = max(idWidth, utf8.RuneCountInString(m.ID)+2)
		detailWidth = max(detailWidth, utf8.RuneCountInString(right)+2)
		options = append(options, uicomponents.Option{
			Value: m.ID,
			Label: label,
			Cells: []string{label, m.ID, right},
		})
	}

	title := "Models · " + provider.Name
	subtitle := providerScopeName(provider) + " · " + provider.Type
	if len(rows) == 0 {
		subtitle += " · no models declared"
	}
	if note := strings.TrimSpace(props.notice); note != "" {
		subtitle += " · " + note
	}
	if props.busy != "" {
		subtitle += " · saving…"
	}

	empty := len(options) == 0
	if empty {
		options = []uicomponents.Option{{
			Value:       emptyModelsRow,
			Label:       "(no models declared — press c to browse the catalog or n to enter an id)",
			Description: "",
		}}
	}

	pickerProps := components.NewPicker(title, options, func(uicomponents.Option) {})
	pickerProps.Subtitle = subtitle
	if !empty {
		if width > 0 {
			idWidth = min(idWidth, max(3, width/3))
			detailWidth = min(detailWidth, max(3, width/3))
		}
		for i := range options {
			cells := options[i].Cells
			cells[1] = format.CompactSingleLine(cells[1], idWidth-2)
			cells[2] = format.CompactSingleLine(cells[2], max(1, detailWidth-2))
			options[i].CellNodes = []trail.Node{
				nil,
				nil,
				uicomponents.Badge(uicomponents.BadgeProps{Label: cells[2], Tone: uicomponents.ToneMuted}),
			}
		}
		pickerProps.Columns = []uicomponents.Column{
			{Width: 0, Align: style.TextAlignLeft},
			{Width: idWidth, Align: style.TextAlignLeft},
			{Width: detailWidth, Align: style.TextAlignLeft},
		}
	}
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "space", Label: "Mark remove"},
		{Key: "d", Label: "Set default"},
		{Key: "c", Label: "Catalog"},
		{Key: "n", Label: "Custom id"},
		{Key: "f2", Label: "Aliases"},
		{Key: "s", Label: "Save"},
		{Key: "esc", Label: "Back"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"space": func(opt uicomponents.Option) {
			if opt.Value == emptyModelsRow || !writable {
				return
			}
			toggleRemove(opt.Value)
		},
		"d": func(opt uicomponents.Option) {
			if opt.Value == emptyModelsRow || !writable {
				return
			}
			markDefault(opt.Value)
		},
		"c": func(_ uicomponents.Option) {
			if !writable {
				return
			}
			if props.onCatalog != nil {
				props.onCatalog()
			}
		},
		"s": func(_ uicomponents.Option) {
			if !writable || props.busy != "" {
				return
			}
			save()
		},
	}
	if props.onAliases != nil {
		pickerProps.Keys["f2"] = func(opt uicomponents.Option) {
			if opt.Value == emptyModelsRow || !writable {
				return
			}
			for _, m := range rows {
				if m.ID == opt.Value {
					props.onAliases(m)
					return
				}
			}
		}
	}
	pickerProps.InputActions = map[string]components.InputAction{
		"n": {
			Prompt: "Custom model id to add:",
			DisabledReason: func(_ uicomponents.Option) string {
				if writable {
					return ""
				}
				return "This provider's manifest is read-only, so models cannot be added."
			},
			OnSubmit: func(opt uicomponents.Option, value string) {
				id := strings.TrimSpace(value)
				if id == "" || !writable || props.busy != "" {
					return
				}
				add := []api.ProviderModelRequest{{ID: id, Name: id}}
				props.onSave(nil, add, "")
			},
		},
	}
	pickerProps.OnCancel = props.onBack

	return components.Picker(pickerProps)
})

// emptyModelsRow is the placeholder row value shown when a provider declares no
// models; keys that act on a model ignore it.
const emptyModelsRow = "__empty"
