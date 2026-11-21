package workspace

import (
	"maps"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

func (w *Workspace) FilterPresetAliases(provider, scope string, spec *warp.ModelProviderSpec, overrides map[string]string) []api.SkippedPresetAlias {
	type reservation struct {
		owner     string
		workspace bool
	}
	key := func(alias string) string {
		return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(alias), "@")))
	}
	reserved := make(map[string]reservation)
	for _, assignment := range w.AliasAssignments() {
		name := key(assignment.Alias)
		owner := assignment.Owners[0]
		if current, ok := reserved[name]; !ok || !current.workspace {
			reserved[name] = reservation{
				owner:     owner.Provider + "/" + owner.Model,
				workspace: assignment.Workspace,
			}
		}
	}
	cat := w.Catalog()
	if scope == "user" && cat.Resolver() != nil {
		for _, res := range cat.Resolver().ListResources(warp.QueryOptions{Kinds: []warp.Kind{warp.KindModelProvider}}) {
			mp := res.(*warp.ModelProvider)
			if cat.ScopeNamespace(mp.BaseResource) != warp.NamespaceUser || !mp.Spec.IsEnabled() {
				continue
			}
			for _, model := range mp.Spec.Models {
				for _, alias := range model.Aliases {
					name := key(alias)
					if _, exists := reserved[name]; !exists {
						reserved[name] = reservation{owner: mp.Metadata.Name + "/" + model.ID}
					}
				}
			}
		}
	}
	if ws := cat.WorkspaceSpec(); ws != nil && ws.Def != nil {
		for _, alias := range slices.Sorted(maps.Keys(ws.Def.Spec.ModelAliases)) {
			name := key(alias)
			if !reserved[name].workspace {
				reserved[name] = reservation{owner: strings.TrimSpace(ws.Def.Spec.ModelAliases[alias]), workspace: true}
			}
		}
	}
	for _, alias := range slices.Sorted(maps.Keys(overrides)) {
		name := key(alias)
		if !reserved[name].workspace || strings.TrimSpace(overrides[alias]) != "" {
			reserved[name] = reservation{owner: strings.TrimSpace(overrides[alias]), workspace: true}
		}
	}

	var skipped []api.SkippedPresetAlias
	for i := range spec.Models {
		model := &spec.Models[i]
		id := provider + "/" + model.ID
		model.Aliases = slices.DeleteFunc(model.Aliases, func(alias string) bool {
			name := key(alias)
			owner, taken := reserved[name]
			if !taken || owner.owner == id {
				return false
			}
			skipped = append(skipped, api.SkippedPresetAlias{Alias: "@" + name, ModelID: id, Owner: owner.owner, WorkspaceOverride: owner.workspace})
			if d := spec.DefaultModels; d != nil {
				for _, ref := range []*string{&d.Completion, &d.Decision, &d.Embedding} {
					if strings.HasPrefix(*ref, "@") && key(*ref) == name {
						*ref = model.ID
					}
				}
			}
			return true
		})
	}
	return skipped
}
