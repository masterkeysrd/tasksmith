package command

import (
	"context"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

type modelAliasEditorSlotProps struct {
	// ProviderName is the WARP provider name and ModelID the raw manifest
	// model id; the slot resolves the owning manifest itself so /model (which
	// only knows the qualified "provider/model" id) and /providers share it.
	ProviderName string
	ModelID      string
	ModelName    string
	// OnDone reports the save outcome (or a load/save failure via err) so the
	// parent refreshes its listing and surfaces the message.
	onDone   func(message string, err error)
	onCancel func()
}

// modelAliasEditorSlot is the data-fetching wrapper around the shared
// components.ModelAliasEditor card. It resolves the owning provider manifest,
// loads the workspace-wide alias report, and performs the atomic-move save, so
// /model and /providers drive one editor and one daemon endpoint.
var modelAliasEditorSlot = trail.FC("ModelAliasEditorSlot", func(props modelAliasEditorSlotProps) trail.Node {
	c := store.Client()
	providers, setProviders := kitex.UseState[[]api.WorkspaceProvider](nil)
	assignments, setAssignments := kitex.UseState[[]api.AliasAssignment](nil)
	loading, setLoading := kitex.UseState(true)
	saving, setSaving := kitex.UseState(false)
	errorMessage, setErrorMessage := kitex.UseState("")

	kitex.UseEffect(func() {
		if c == nil {
			setLoading(false)
			setErrorMessage("Client unavailable.")
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			list, aliasList, err := loadAliasEditorContext(ctx, c)
			if err != nil {
				setErrorMessage(err.Error())
			} else {
				setProviders(list)
				setAssignments(aliasList)
			}
			setLoading(false)
		}()
	}, []any{c != nil, props.ProviderName})

	provider := providerByName(providers(), props.ProviderName)

	return components.ModelAliasEditor(components.ModelAliasEditorProps{
		Provider:     props.ProviderName,
		ModelID:      props.ModelID,
		ModelName:    firstNonEmpty(props.ModelName, props.ModelID),
		ManifestPath: provider.Path,
		Scope:        providerScopeName(provider),
		DefaultModel: provider.DefaultModel,
		Assignments:  assignments(),
		Loading:      loading(),
		Saving:       saving(),
		ErrorMessage: errorMessage(),
		OnSave: func(req api.AssignModelAliasesRequest) {
			if c == nil || saving() {
				return
			}
			setSaving(true)
			setErrorMessage("")
			goMCPBackground(func() {
				defer setSaving(false)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				resp, err := c.AssignModelAliases(ctx, req)
				if err != nil {
					setErrorMessage(err.Error())
					if props.onDone != nil {
						props.onDone("", err)
					}
					return
				}
				setProviders(resp.Providers)
				setAssignments(resp.Aliases)
				var moved []string
				for _, alias := range req.Aliases {
					moved = append(moved, "@"+alias)
				}
				if req.DefaultHere {
					moved = append(moved, "@default")
				}
				message := "no aliases"
				if len(moved) > 0 {
					message = strings.Join(moved, ", ")
				}
				if props.onDone != nil {
					props.onDone("Saved aliases on "+req.Provider+"/"+req.Model+": "+message, nil)
				}
			})
		},
		OnCancel: func() {
			if props.onCancel != nil {
				props.onCancel()
			}
		},
	})
})

// loadAliasEditorContext fetches the provider listing and the workspace alias
// report in one place so every caller of the editor loads the same context.
func loadAliasEditorContext(ctx context.Context, c aliasEditorClient) ([]api.WorkspaceProvider, []api.AliasAssignment, error) {
	list, err := c.ListWorkspaceProviders(ctx)
	if err != nil {
		return nil, nil, err
	}
	aliases, err := c.ListAliasAssignments(ctx)
	if err != nil {
		return nil, nil, err
	}
	return list, aliases, nil
}

// aliasEditorClient is the slice of the daemon client the editor wrapper uses,
// kept as an interface so tests can drive it without an HTTP transport.
type aliasEditorClient interface {
	ListWorkspaceProviders(ctx context.Context) ([]api.WorkspaceProvider, error)
	ListAliasAssignments(ctx context.Context) ([]api.AliasAssignment, error)
	AssignModelAliases(ctx context.Context, req api.AssignModelAliasesRequest) (api.AssignModelAliasesResponse, error)
}

// refreshProviderWorkspaceContext re-pulls the workspace warnings and the
// session model preview after a provider or alias mutation, so duplicate-alias
// diagnostics and the model badge reflect the patched manifests. With no
// session yet it refreshes the draft preview instead of skipping it. Callers
// run it off the render thread (inside a background callback).
func refreshProviderWorkspaceContext(ctx context.Context) {
	c := store.Client()
	if c == nil {
		return
	}
	if ws, err := c.GetWorkspace(ctx); err == nil {
		store.SetWorkspaceWarnings(ws.Warnings)
	}
	if state := store.GetSession(); state.Session != nil {
		if updated, err := c.GetSession(ctx, state.Session.ID); err == nil && updated != nil {
			store.SetSession(updated)
		}
		return
	}
	store.RefreshDraftModelPreview(ctx)
}
