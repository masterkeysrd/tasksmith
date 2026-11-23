package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

type providersAddPaneProps struct {
	client *client.Client
	onBack func()
	// onCreated closes the browser and reports the created provider together
	// with the connectivity verdict gathered after the write.
	onCreated func(created api.WorkspaceProvider, verdict string)
}

// providerAddStep tracks where in the add flow the form is.
type providerAddStep int

const (
	providerAddStepPreset providerAddStep = iota
	providerAddStepForm
)

// providerAddDraft accumulates the manifest fields before the write.
type providerAddDraft struct {
	preset   api.ProviderSummary
	isCustom bool
	scope    string // "workspace" or "user"
	name     string
	provType string
	endpoint string
	authEnv  string
	apiKey   string
}

const (
	customProviderPresetRow = "__custom"
	addRowScope             = "scope"
	addRowName              = "name"
	addRowType              = "type"
	addRowEndpoint          = "endpoint"
	addRowAuthEnv           = "auth_env"
	addRowAPIKey            = "api_key"
	addRowSubmit            = "__submit"
)

// providersAddPane first picks a preset (or a custom type) and then collects
// the manifest fields. Secrets go to the scope's .env exclusively; the daemon
// wires that plus the manifest write and hot reload.
var providersAddPane = trail.FC("ProvidersAddPane", func(props providersAddPaneProps) trail.Node {
	step, setStep := kitex.UseState(providerAddStepPreset)
	draft, setDraft := kitex.UseState(providerAddDraft{scope: "workspace"})
	presets, setPresets := kitex.UseState[[]api.ProviderSummary](nil)
	loading, setLoading := kitex.UseState(true)
	notice, setNotice := kitex.UseState("")
	saving, setSaving := kitex.UseState(false)

	kitex.UseEffect(func() {
		if props.client == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			list, err := props.client.ListProviders(ctx)
			if err != nil {
				setNotice("Failed to load presets: " + err.Error())
			} else {
				setPresets(list)
			}
			setLoading(false)
		}()
	}, []any{props.client != nil})

	if loading() {
		return loadingHint("Loading provider presets…")
	}

	if step() == providerAddStepPreset {
		options := make([]uicomponents.Option, 0, len(presets())+1)
		for _, p := range presets() {
			desc := strings.TrimSpace(p.AuthEnv)
			if desc == "" && p.AuthType != "" {
				desc = p.AuthType
			}
			if len(p.Models) > 0 {
				desc = strings.TrimSpace(desc + fmt.Sprintf(" · %d models", len(p.Models)))
			}
			label := components.ProviderDisplayName(p.Name, p.DisplayName)
			options = append(options, uicomponents.Option{
				Value:       p.Name,
				Label:       label,
				Description: desc,
				Search:      p.Name + " " + p.AuthEnv,
			})
		}
		options = append(options, uicomponents.Option{
			Value:       customProviderPresetRow,
			Label:       "Custom provider…",
			Description: "Author a ModelProvider manifest for any supported type",
		})

		pickerProps := components.NewPicker("Add provider", options, func(opt uicomponents.Option) {
			next := providerAddDraft{scope: draft().scope}
			if opt.Value == customProviderPresetRow {
				next.isCustom = true
			} else {
				for _, p := range presets() {
					if p.Name == opt.Value {
						next.preset = p
						next.name = p.Name
						next.provType = p.Name
						next.authEnv = p.AuthEnv
						break
					}
				}
			}
			setDraft(next)
			setNotice("")
			setStep(providerAddStepForm)
		})
		pickerProps.Subtitle = "Pick a built-in preset or author a custom manifest"
		pickerProps.Filterable = true
		pickerProps.Bindings = []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "Select"},
			{Key: "enter", Label: "Continue"},
			{Key: "esc", Label: "Back"},
		}
		pickerProps.OnCancel = func() {
			if props.onBack != nil {
				props.onBack()
			}
		}
		return components.Picker(pickerProps)
	}

	d := draft()
	browserOnly := d.preset.BrowserAuthLabel != ""
	fieldRow := func(key, label, value, hint string) uicomponents.Option {
		return uicomponents.Option{Value: key, Label: label, Description: value, Search: hint}
	}
	options := []uicomponents.Option{
		fieldRow(addRowScope, "Scope", scopeLabel(d.scope), "user or workspace scope for the manifest"),
	}
	submitDescription := "Write the manifest, store the secret, reload the workspace"
	if browserOnly {
		submitDescription = "Write the preset manifest and reload the workspace"
	} else {
		options = append(options, fieldRow(addRowName, "Name", d.name, "lowercase slug; the manifest file name"))
		if d.isCustom {
			options = append(options, fieldRow(addRowType, "Type", d.provType, "anthropic, openai, google-genai, ollama, …"))
		}
		options = append(options,
			fieldRow(addRowEndpoint, "Endpoint", d.endpoint, "leave empty to use the preset endpoint"),
			fieldRow(addRowAuthEnv, "Auth env var", d.authEnv, "leave empty to derive <NAME>_API_KEY"),
			fieldRow(addRowAPIKey, "API key", maskKey(d.apiKey), "stored only in the scope .env, never in YAML"),
		)
	}
	options = append(options, uicomponents.Option{Value: addRowSubmit, Label: "Create provider", Description: submitDescription})

	submit := func() {
		if props.client == nil || saving() {
			return
		}
		name := strings.TrimSpace(d.name)
		if name == "" {
			setNotice("A provider name is required (lowercase slug).")
			return
		}
		provType := strings.TrimSpace(d.provType)
		if provType == "" {
			setNotice("A provider type is required.")
			return
		}
		setSaving(true)
		goMCPBackground(func() {
			defer setSaving(false)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			req := api.CreateProviderRequest{Scope: d.scope, Name: name, Type: provType}
			if !browserOnly {
				req.Endpoint = strings.TrimSpace(d.endpoint)
				req.APIKey = strings.TrimSpace(d.apiKey)
				req.AuthEnv = strings.TrimSpace(d.authEnv)
			}
			created, err := props.client.CreateProvider(ctx, req)
			if err != nil {
				setNotice("Failed to create provider: " + err.Error())
				return
			}
			verdict := ""
			if browserOnly {
				if !created.HasKey {
					verdict = " · sign in with /login " + created.Name
				}
			} else if list, catalogErr := props.client.ListProviderCatalog(ctx, created.Name); catalogErr != nil {
				verdict = " · connectivity check failed: " + catalogErr.Error()
			} else if len(list) == 0 {
				verdict = " · no catalog models visible (OpenAI-compatible endpoints list models via Manage models)"
			} else {
				verdict = fmt.Sprintf(" · %d catalog models visible", len(list))
			}
			if props.onCreated != nil {
				props.onCreated(created, verdict)
			}
		})
	}

	titleName := firstNonEmpty(d.name, "custom")
	if browserOnly {
		titleName = components.ProviderDisplayName(d.name, d.preset.DisplayName)
	}
	pickerProps := components.NewPicker("Add provider · "+titleName, options, func(opt uicomponents.Option) {
		switch opt.Value {
		case addRowScope:
			next := d
			if next.scope == "workspace" {
				next.scope = "user"
			} else {
				next.scope = "workspace"
			}
			setNotice("")
			setDraft(next)
		case addRowSubmit:
			submit()
		}
	})
	subtitle := "enter toggles / submits · e edits the highlighted field"
	if browserOnly {
		subtitle = "Fixed browser OAuth preset · /login " + d.name
	}
	if saving() {
		subtitle = "Creating provider…"
	} else if note := strings.TrimSpace(notice()); note != "" {
		subtitle = note
	}
	pickerProps.Subtitle = subtitle
	pickerProps.Columns = []uicomponents.Column{
		{Width: 14, Align: style.TextAlignLeft},
		{Width: 0, Align: style.TextAlignLeft},
	}
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Toggle / create"},
	}
	pickerProps.OnCancel = func() {
		setNotice("")
		setStep(providerAddStepPreset)
	}
	if !browserOnly {
		pickerProps.Bindings = append(pickerProps.Bindings, uicomponents.KeyBinding{Key: "e", Label: "Edit field"})
	}
	pickerProps.Bindings = append(pickerProps.Bindings, uicomponents.KeyBinding{Key: "esc", Label: "Back"})
	if browserOnly {
		return components.Picker(pickerProps)
	}
	pickerProps.InputActions = map[string]components.InputAction{
		"e": {
			Prompt: "New value:",
			DisabledReason: func(opt uicomponents.Option) string {
				switch opt.Value {
				case addRowName, addRowType, addRowEndpoint, addRowAuthEnv, addRowAPIKey:
					return ""
				}
				return "The " + opt.Label + " row toggles or submits on enter; it has no free-text value."
			},
			InitialValue: func(opt uicomponents.Option) string {
				switch opt.Value {
				case addRowName:
					return d.name
				case addRowType:
					return d.provType
				case addRowEndpoint:
					return d.endpoint
				case addRowAuthEnv:
					return d.authEnv
				case addRowAPIKey:
					return d.apiKey
				}
				return ""
			},
			OnSubmit: func(opt uicomponents.Option, value string) {
				next := d
				switch opt.Value {
				case addRowName:
					next.name = strings.TrimSpace(value)
				case addRowType:
					next.provType = strings.TrimSpace(value)
				case addRowEndpoint:
					next.endpoint = strings.TrimSpace(value)
				case addRowAuthEnv:
					next.authEnv = strings.ToUpper(strings.TrimSpace(value))
				case addRowAPIKey:
					next.apiKey = strings.TrimSpace(value)
				default:
					return
				}
				setNotice("")
				setDraft(next)
			},
		},
	}

	return components.Picker(pickerProps)
})

func scopeLabel(scope string) string {
	if scope == "user" {
		return "User (~/.config/tasksmith/agents/providers)"
	}
	return "Workspace (.agents/providers)"
}

// maskKey keeps a typed secret out of the rendered row while confirming it is
// set and how long it is.
func maskKey(key string) string {
	if strings.TrimSpace(key) == "" {
		return "(not set)"
	}
	return strings.Repeat("•", min(len(key), 24))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
