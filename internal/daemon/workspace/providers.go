package workspace

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/warp"
)

// ErrProviderReadOnly marks a patch attempt against a provider whose manifest
// is not writable on disk (built-in or embedded resources).
var ErrProviderReadOnly = errors.New("workspace: provider manifest is read-only")

// ErrProviderNotFound marks operations naming a provider the workspace does not
// declare in any tier.
var ErrProviderNotFound = errors.New("workspace: provider not found")

// ProviderModelEdit is one model entry written into a provider manifest.
type ProviderModelEdit struct {
	ID      string
	Name    string
	Label   string
	Aliases []string
	Context int
	Output  int
}

// ProviderRecord is one provider name as the workspace resolves it across
// tiers: the winning (highest-priority) manifest, whether it is enabled, and
// whether it shadows or is shadowed by another tier.
type ProviderRecord struct {
	Name             string
	Def              *warp.ModelProvider // winning manifest definition
	Namespace        string              // winning manifest namespace
	Path             string              // winning manifest file path ("" for embedded)
	Enabled          bool                // spec.enabled on the winner (true when omitted)
	Shadowed         bool                // winner overrides a lower-tier same-name manifest
	InheritedEnabled bool                // spec.enabled on the shadowed lower-tier winner
	InheritedPath    string              // file path of the shadowed lower-tier winner ("" when unknown)
	Writable         bool                // winner manifest may be patched in place
	Overridable      bool                // winner lives in the user tier (a workspace shadow can mute it)
	Inherited        []string            // namespaces of lower-priority same-name manifests
}

// providerManifestDir returns the workspace-tier providers directory, where
// shadow overrides and workspace-scoped creations land.
func providerManifestDir(rootDir string) string {
	return filepath.Join(rootDir, warp.DefaultAgentsDir, "providers")
}

func writableProviderNamespace(ns string) bool {
	switch strings.ToLower(strings.TrimSpace(ns)) {
	case warp.NamespaceUser, warp.NamespaceWorkspace, warp.NamespaceLocal:
		return true
	default:
		return false
	}
}

// providerTierPriority ranks one manifest tier for the current project scope.
// The active project slug outranks every standard namespace (WARP's scoped
// resolution gives it 200), then workspace > user > system.
func providerTierPriority(cat *Catalog, ns string) int {
	if proj := cat.Project(); proj != nil && ns == proj.Name {
		return 200
	}
	return warp.NamespacePriority(ns)
}

// providerRecords groups every ModelProvider resource visible in the current
// project scope — including disabled and shadowed tiers — by metadata name and
// resolves the winning manifest for each name by scoped namespace priority.
// WARP's own effective listing filters disabled manifests *after*
// deduplication, so the browser needs the raw tiers to tell "disabled" apart
// from "shadowed".
func (w *Workspace) providerRecords() []ProviderRecord {
	cat := w.Catalog()
	resolver := cat.Resolver()
	if resolver == nil {
		return nil
	}

	byName := make(map[string][]*warp.ModelProvider)
	for _, res := range resolver.ListResources(warp.QueryOptions{
		Kinds:           []warp.Kind{warp.KindModelProvider},
		IncludeDisabled: true,
	}) {
		mp, ok := res.(*warp.ModelProvider)
		if !ok {
			continue
		}
		name := strings.TrimSpace(mp.Metadata.Name)
		if name != "" {
			byName[name] = append(byName[name], mp)
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)

	records := make([]ProviderRecord, 0, len(names))
	for _, name := range names {
		tiers := byName[name]
		slices.SortFunc(tiers, func(a, b *warp.ModelProvider) int {
			return cmp.Compare(providerTierPriority(cat, b.Namespace), providerTierPriority(cat, a.Namespace))
		})
		winner := tiers[0]
		path := strings.TrimSpace(winner.GetPath())
		ns := cat.ScopeNamespace(winner.BaseResource)

		var inherited []string
		inheritedEnabled := false
		inheritedPath := ""
		if len(tiers) > 1 {
			for _, t := range tiers[1:] {
				inherited = append(inherited, cat.ScopeNamespace(t.BaseResource))
			}
			inheritedEnabled = tiers[1].Spec.IsEnabled()
			inheritedPath = strings.TrimSpace(tiers[1].GetPath())
		}

		writable := writableProviderNamespace(ns) && path != ""
		if writable {
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				writable = false
			}
		}

		records = append(records, ProviderRecord{
			Name:             name,
			Def:              winner,
			Namespace:        ns,
			Path:             path,
			Enabled:          winner.Spec.IsEnabled(),
			Shadowed:         len(inherited) > 0 && writableProviderNamespace(ns),
			InheritedEnabled: inheritedEnabled,
			InheritedPath:    inheritedPath,
			Writable:         writable,
			Overridable:      strings.EqualFold(strings.TrimSpace(ns), warp.NamespaceUser),
			Inherited:        inherited,
		})
	}
	return records
}

// ProviderRecords returns every configured provider grouped across tiers,
// including disabled and shadowed ones the effective listing hides.
func (w *Workspace) ProviderRecords() []ProviderRecord {
	return w.providerRecords()
}

// ProviderRecord resolves one provider name across tiers.
func (w *Workspace) ProviderRecord(name string) (ProviderRecord, bool) {
	name = strings.TrimSpace(name)
	for _, rec := range w.providerRecords() {
		if rec.Name == name {
			return rec, true
		}
	}
	return ProviderRecord{}, false
}

// loadProviderManifest re-reads the manifest file backing rec so patches apply
// to the on-disk truth rather than a possibly stale registry copy.
func loadProviderManifest(rec ProviderRecord) (*warp.ModelProvider, error) {
	data, err := os.ReadFile(rec.Path)
	if err != nil {
		return nil, fmt.Errorf("workspace: read provider manifest %s: %w", rec.Path, err)
	}
	res, err := warp.Parse(rec.Path, string(data))
	if err != nil {
		return nil, fmt.Errorf("workspace: parse provider manifest %s: %w", rec.Path, err)
	}
	mp, ok := res.Resource.(*warp.ModelProvider)
	if !ok {
		return nil, fmt.Errorf("workspace: %s is not a ModelProvider manifest", rec.Path)
	}
	return mp, nil
}

// writeProviderManifest serializes mp through warp.Format and writes it
// atomically, preserving the file's existing mode. The edited document is
// re-parsed before writing so a caller never reports a patch that leaves an
// unparseable manifest behind.
func writeProviderManifest(path string, mp *warp.ModelProvider) error {
	data, err := warp.Format(mp)
	if err != nil {
		return fmt.Errorf("workspace: format provider manifest: %w", err)
	}
	if _, err := warp.Parse(path, string(data)); err != nil {
		return fmt.Errorf("workspace: edited provider manifest %s no longer parses: %w", path, err)
	}
	if err := validateProviderSpec(mp); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := fsutils.WriteFileAtomic(path, data, mode); err != nil {
		return fmt.Errorf("workspace: write provider manifest %s: %w", path, err)
	}
	return nil
}

// validateProviderSpec mirrors WARP's per-provider alias rules (empty,
// reserved, duplicate, or colliding aliases) so /providers never writes a
// manifest that would surface a validation warning on the next load.
func validateProviderSpec(mp *warp.ModelProvider) error {
	seenAliases := make(map[string]string)
	idsAndNames := make(map[string]string)
	for _, m := range mp.Spec.Models {
		if m.ID != "" {
			idsAndNames[m.ID] = m.ID
			if m.Name != "" {
				idsAndNames[m.Name] = m.ID
			}
		}
	}
	for _, m := range mp.Spec.Models {
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("workspace: provider %s declares a model without an id", mp.Metadata.Name)
		}
		for _, a := range m.Aliases {
			norm := strings.TrimSpace(strings.TrimPrefix(a, "@"))
			if norm == "" {
				return fmt.Errorf("workspace: provider %s model %q declares an empty alias", mp.Metadata.Name, m.ID)
			}
			if strings.EqualFold(norm, "inherit") {
				return fmt.Errorf("workspace: provider %s model %q declares reserved alias %q", mp.Metadata.Name, m.ID, a)
			}
			if existing, dup := seenAliases[norm]; dup {
				return fmt.Errorf("workspace: provider %s declares duplicate alias %q on models %q and %q", mp.Metadata.Name, a, existing, m.ID)
			}
			seenAliases[norm] = m.ID
			if colliding, clash := idsAndNames[norm]; clash && colliding != m.ID {
				return fmt.Errorf("workspace: provider %s alias %q on model %q collides with model %q", mp.Metadata.Name, a, m.ID, colliding)
			}
		}
	}
	return nil
}

// isMuteOnlyShadow reports whether mp carries nothing but the enable flag (plus
// the kind/name/type needed to parse): the exact shape writeProviderShadow
// authors to mute an inherited provider locally. A full workspace override —
// custom endpoint, auth, headers, config, defaults, or models — is user content
// and must never be deleted to re-enable a provider.
func isMuteOnlyShadow(mp *warp.ModelProvider) bool {
	if mp == nil {
		return false
	}
	spec := mp.Spec
	return strings.TrimSpace(spec.Endpoint) == "" && spec.Auth == nil &&
		len(spec.Headers) == 0 && len(spec.Config) == 0 &&
		spec.DefaultModels == nil && len(spec.Models) == 0
}

// SetProviderEnabled patches spec.enabled for the named provider.
//
// Disabling a provider that lives outside the workspace tier (typically the
// user tier) writes a workspace-tier shadow manifest carrying only the disable
// flag, so the inherited endpoint, auth, aliases, and models stay intact and
// the mute is scoped to this repository. Re-enabling drops that mute-only
// shadow when the inherited manifest is already enabled (clean restore), and
// promotes the inherited definition into a full workspace override when the
// inherited manifest is itself disabled. A full workspace override is only ever
// edited in place — never deleted.
func (w *Workspace) SetProviderEnabled(name string, enabled bool) error {
	rec, ok := w.ProviderRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	if rec.Enabled == enabled {
		return nil
	}

	if enabled && rec.Shadowed && rec.Writable {
		shadow, err := loadProviderManifest(rec)
		if err != nil {
			return err
		}
		if !isMuteOnlyShadow(shadow) {
			// A real workspace override: clear the flag where it stands.
			shadow.Spec.Enabled = nil
			return writeProviderManifest(rec.Path, shadow)
		}
		if rec.InheritedEnabled {
			if err := os.Remove(rec.Path); err != nil {
				return fmt.Errorf("workspace: remove provider shadow %s: %w", rec.Path, err)
			}
			return nil
		}
		// The inherited manifest is itself disabled, and a shadow can only
		// mute, never un-mute it: promote the inherited definition into a full
		// workspace override that carries an explicit enable.
		return w.promoteInheritedProvider(rec)
	}

	if rec.Writable && !rec.Overridable {
		mp, err := loadProviderManifest(rec)
		if err != nil {
			return err
		}
		mp.Spec.Enabled = enabledPtr(enabled)
		return writeProviderManifest(rec.Path, mp)
	}

	if enabled {
		// A global provider disabled in its own manifest is re-enabled there
		// directly: a workspace shadow could only mute, never un-mute globally.
		if rec.Writable {
			mp, err := loadProviderManifest(rec)
			if err != nil {
				return err
			}
			mp.Spec.Enabled = nil
			return writeProviderManifest(rec.Path, mp)
		}
		return fmt.Errorf("%w: provider %s has no manifest file to enable", ErrProviderReadOnly, name)
	}

	if rec.Def == nil {
		return fmt.Errorf("%w: provider %s has no definition to disable", ErrProviderNotFound, name)
	}
	if !writableProviderNamespace(rec.Namespace) {
		return fmt.Errorf("%w: provider %s is built-in and cannot be disabled from the workspace", ErrProviderReadOnly, name)
	}
	// Muting a global provider locally: author the workspace shadow rather than
	// editing the user manifest, which every other repository inherits.
	return w.writeProviderShadow(name, false)
}

func enabledPtr(enabled bool) *bool {
	if enabled {
		return nil
	}
	off := false
	return &off
}

// writeProviderShadow creates (or patches) the workspace-tier shadow manifest
// for an inherited provider, carrying only the enable flag.
func (w *Workspace) writeProviderShadow(name string, enabled bool) error {
	dir := providerManifestDir(w.RootDir())
	path := filepath.Join(dir, name+".provider.yaml")

	var mp *warp.ModelProvider
	if data, err := os.ReadFile(path); err == nil {
		res, parseErr := warp.Parse(path, string(data))
		if parseErr != nil {
			return fmt.Errorf("workspace: parse provider shadow %s: %w", path, parseErr)
		}
		var ok bool
		if mp, ok = res.Resource.(*warp.ModelProvider); !ok {
			return fmt.Errorf("workspace: %s is not a ModelProvider manifest", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("workspace: read provider shadow %s: %w", path, err)
	}

	if mp == nil {
		rec, found := w.ProviderRecord(name)
		if !found || rec.Def == nil {
			return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
		}
		mp = &warp.ModelProvider{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindModelProvider,
				Metadata:   warp.Metadata{Name: name},
			},
			Spec: warp.ModelProviderSpec{Type: rec.Def.Spec.Type},
		}
	}
	mp.Spec.Enabled = enabledPtr(enabled)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: create providers directory: %w", err)
	}
	return writeProviderManifest(path, mp)
}

// promoteInheritedProvider replaces a mute-only workspace shadow with a full
// copy of the inherited manifest that carries an explicit enable, so
// re-enabling a provider whose global manifest is disabled yields a working
// workspace-scoped provider instead of an empty stub.
func (w *Workspace) promoteInheritedProvider(rec ProviderRecord) error {
	if rec.InheritedPath == "" {
		return fmt.Errorf("%w: provider %s has no writable inherited manifest to enable", ErrProviderReadOnly, rec.Name)
	}
	data, err := os.ReadFile(rec.InheritedPath)
	if err != nil {
		return fmt.Errorf("workspace: read inherited provider manifest %s: %w", rec.InheritedPath, err)
	}
	res, err := warp.Parse(rec.InheritedPath, string(data))
	if err != nil {
		return fmt.Errorf("workspace: parse inherited provider manifest %s: %w", rec.InheritedPath, err)
	}
	inherited, ok := res.Resource.(*warp.ModelProvider)
	if !ok {
		return fmt.Errorf("workspace: %s is not a ModelProvider manifest", rec.InheritedPath)
	}
	promoted := inherited.DeepCopy()
	promoted.APIVersion = warp.APIVersion
	promoted.Kind = warp.KindModelProvider
	promoted.Metadata.Name = rec.Name
	promoted.Namespace = ""
	promoted.Path = ""
	promoted.Directory = ""
	promoted.Spec.Enabled = nil

	dir := providerManifestDir(w.RootDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: create providers directory: %w", err)
	}
	return writeProviderManifest(filepath.Join(dir, rec.Name+".provider.yaml"), promoted)
}

// UpdateProviderModels replaces spec.models (and optionally the completion
// default) on the manifest owning the named provider. Edits to a model that is
// already declared are applied onto a deep copy of its existing entry, so
// capabilities, cost tiers, and dimension limits the editor does not surface
// survive a model add/remove or default change. Only the completion default is
// touched by defaultSet, and a completion pointing at a removed model is
// cleared instead of left dangling.
func (w *Workspace) UpdateProviderModels(name string, models []ProviderModelEdit, defaultModel string, defaultSet bool) error {
	rec, ok := w.ProviderRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	if !rec.Writable {
		return fmt.Errorf("%w: provider %s manifest is not writable", ErrProviderReadOnly, name)
	}

	mp, err := loadProviderManifest(rec)
	if err != nil {
		return err
	}

	// Resolve the default roles against the pre-edit model list so removals
	// can be detected precisely.
	prevCompletion, prevCompletionFound := "", false
	prevDecision, prevDecisionFound := "", false
	prevEmbedding, prevEmbeddingFound := "", false
	if dm := mp.Spec.DefaultModels; dm != nil {
		if ref := dm.ForRole(warp.ModelRoleCompletion); ref != "" {
			prevCompletion, prevCompletionFound = resolveProviderModelRef(mp, ref)
		}
		if ref := dm.ForRole(warp.ModelRoleDecision); ref != "" {
			prevDecision, prevDecisionFound = resolveProviderModelRef(mp, ref)
		}
		if ref := dm.ForRole(warp.ModelRoleEmbedding); ref != "" {
			prevEmbedding, prevEmbeddingFound = resolveProviderModelRef(mp, ref)
		}
	}

	existing := make(map[string]*warp.ProviderModel, len(mp.Spec.Models))
	for i := range mp.Spec.Models {
		if id := strings.TrimSpace(mp.Spec.Models[i].ID); id != "" {
			existing[id] = &mp.Spec.Models[i]
		}
	}

	seen := make(map[string]bool, len(models))
	edited := make([]warp.ProviderModel, 0, len(models))
	for _, m := range models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			return errors.New("workspace: model id must not be empty")
		}
		if seen[id] {
			return fmt.Errorf("workspace: duplicate model id %q for provider %s", id, name)
		}
		seen[id] = true
		pm := warp.ProviderModel{}
		if cur, declared := existing[id]; declared {
			pm = *cur.DeepCopy()
		}
		pm.ID = id
		pm.Name = cmp.Or(strings.TrimSpace(m.Name), id)
		pm.Label = strings.TrimSpace(m.Label)
		pm.Aliases = normalizeAliasList(m.Aliases)
		if m.Context > 0 || m.Output > 0 {
			if m.Context > 0 {
				pm.Limits.Context = m.Context
			}
			if m.Output > 0 {
				pm.Limits.Output = m.Output
			}
		}
		edited = append(edited, pm)
	}
	mp.Spec.Models = edited

	if defaultSet {
		if strings.TrimSpace(defaultModel) == "" {
			clearCompletionDefault(mp)
		} else {
			if mp.Spec.DefaultModels == nil {
				mp.Spec.DefaultModels = &warp.ModelDefaults{}
			}
			mp.Spec.DefaultModels.Completion = strings.TrimSpace(defaultModel)
		}
	} else if prevCompletionFound && !seen[prevCompletion] {
		// A staged removal of the current default must not leave a dangling
		// completion reference behind even when defaultSet was not sent.
		clearCompletionDefault(mp)
	}
	// Staged removals of the other default roles must not leave dangling
	// references either.
	if mp.Spec.DefaultModels != nil {
		if prevDecisionFound && !seen[prevDecision] {
			mp.Spec.DefaultModels.Decision = ""
		}
		if prevEmbeddingFound && !seen[prevEmbedding] {
			mp.Spec.DefaultModels.Embedding = ""
		}
		if *mp.Spec.DefaultModels == (warp.ModelDefaults{}) {
			mp.Spec.DefaultModels = nil
		}
	}

	return writeProviderManifest(rec.Path, mp)
}

// clearCompletionDefault drops spec.defaultModels.completion, removing the
// whole defaults block once no role references a model anymore.
func clearCompletionDefault(mp *warp.ModelProvider) {
	if mp.Spec.DefaultModels == nil {
		return
	}
	mp.Spec.DefaultModels.Completion = ""
	if *mp.Spec.DefaultModels == (warp.ModelDefaults{}) {
		mp.Spec.DefaultModels = nil
	}
}

// AssignModelAliases saves the exact alias set the target model should own.
// Every alias in the set is first stripped from any other model across all
// writable provider manifests (the atomic move that keeps bare aliases 1-to-1),
// then written onto the target model. "default" is owned by the manifest's
// spec.defaultModels.completion rather than the aliases list, so defaultHere
// routes it there. All touched manifests are validated before any file is
// written, so a rejected save leaves no partial edit behind.
func (w *Workspace) AssignModelAliases(providerName, modelID string, aliases []string, defaultHere bool) error {
	providerName = strings.TrimSpace(providerName)
	modelID = strings.TrimSpace(modelID)
	rec, ok := w.ProviderRecord(providerName)
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, providerName)
	}
	if !rec.Writable {
		return fmt.Errorf("%w: provider %s manifest is not writable", ErrProviderReadOnly, providerName)
	}

	wanted := normalizeAliasList(aliases)
	for _, a := range wanted {
		if strings.EqualFold(a, "inherit") {
			return errors.New("workspace: @inherit is reserved and cannot be assigned")
		}
	}

	target, err := loadProviderManifest(rec)
	if err != nil {
		return err
	}
	targetIdx := -1
	for i := range target.Spec.Models {
		if target.Spec.Models[i].ID == modelID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return fmt.Errorf("workspace: provider %s has no model %q", providerName, modelID)
	}

	type pendingWrite struct {
		path string
		mp   *warp.ModelProvider
	}
	var writes []pendingWrite

	for _, other := range w.providerRecords() {
		if !other.Writable || other.Name == providerName {
			continue
		}
		otherMP, loadErr := loadProviderManifest(other)
		if loadErr != nil {
			return loadErr
		}
		changed := false
		for i := range otherMP.Spec.Models {
			before := len(otherMP.Spec.Models[i].Aliases)
			otherMP.Spec.Models[i].Aliases = removeAliases(otherMP.Spec.Models[i].Aliases, wanted)
			changed = changed || len(otherMP.Spec.Models[i].Aliases) != before
		}
		if changed {
			writes = append(writes, pendingWrite{path: other.Path, mp: otherMP})
		}
	}

	for i := range target.Spec.Models {
		if i == targetIdx {
			continue
		}
		target.Spec.Models[i].Aliases = removeAliases(target.Spec.Models[i].Aliases, wanted)
	}
	target.Spec.Models[targetIdx].Aliases = slices.Clone(wanted)

	if defaultHere {
		if target.Spec.DefaultModels == nil {
			target.Spec.DefaultModels = &warp.ModelDefaults{}
		}
		target.Spec.DefaultModels.Completion = modelID
	} else if target.Spec.DefaultModels != nil && target.Spec.DefaultModels.Completion == modelID {
		clearCompletionDefault(target)
	}

	if err := validateProviderSpec(target); err != nil {
		return err
	}
	for _, pw := range writes {
		if err := validateProviderSpec(pw.mp); err != nil {
			return err
		}
	}
	for _, pw := range writes {
		if err := writeProviderManifest(pw.path, pw.mp); err != nil {
			return err
		}
	}
	return writeProviderManifest(rec.Path, target)
}

// AliasOwner is one "provider/model" pair declaring an alias.
type AliasOwner struct {
	Provider string
	Model    string
}

// AliasAssignment is one alias with its current owners across enabled
// providers. More than one owner means manual YAML edits collided.
type AliasAssignment struct {
	Alias      string
	Owners     []AliasOwner
	Workspace  bool // workspace.spec.modelAliases shadows the bare alias
	Duplicates []AliasOwner
}

// AliasAssignments reports every bare alias declared across the workspace's
// effective providers, mirroring WARP's cross-provider duplicate detection and
// WORKSPACE.md overrides (spec.defaultProvider ordering, spec.defaultModels
// completion, and spec.modelAliases) so the alias editor can show live
// assignment state.
func (w *Workspace) AliasAssignments() []AliasAssignment {
	cat := w.Catalog()
	resolver := cat.Resolver()
	if resolver == nil {
		return nil
	}

	wsAliases := map[string]string{}
	var defaultProv, wsCompletion string
	if spec := cat.WorkspaceSpec(); spec != nil && spec.Def != nil {
		for name, target := range spec.Def.Spec.ModelAliases {
			key := strings.TrimPrefix(strings.TrimSpace(name), "@")
			if key == "" {
				continue
			}
			if target = strings.TrimSpace(target); target != "" {
				wsAliases[key] = target
			} else {
				wsAliases[key] = ""
			}
		}
		defaultProv = strings.TrimSpace(spec.Def.Spec.DefaultProvider)
		wsCompletion = strings.TrimSpace(spec.Def.Spec.DefaultModels.ForRole(warp.ModelRoleCompletion))
	}

	owners := make(map[string][]AliasOwner)
	seen := make(map[string]map[string]bool)
	providers := make([]*warp.ModelProvider, 0)
	for _, res := range resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindModelProvider},
		Effective: true,
	}) {
		mp, ok := res.(*warp.ModelProvider)
		if !ok || !mp.Spec.IsEnabled() {
			continue
		}
		providers = append(providers, mp)
	}
	// Resolution order is tier priority (project slug first, then workspace >
	// user > system), not the registry's alphabetical listing order, and a
	// WORKSPACE.md spec.defaultProvider outranks everything (warp's
	// effectiveProviders promotes it first).
	slices.SortStableFunc(providers, func(a, b *warp.ModelProvider) int {
		if d := cmp.Compare(providerTierPriority(cat, cat.ScopeNamespace(b.BaseResource)),
			providerTierPriority(cat, cat.ScopeNamespace(a.BaseResource))); d != 0 {
			return d
		}
		return strings.Compare(a.Metadata.Name, b.Metadata.Name)
	})
	if defaultProv != "" {
		slices.SortStableFunc(providers, func(a, b *warp.ModelProvider) int {
			return cmp.Compare(providerDefaultRank(a, defaultProv), providerDefaultRank(b, defaultProv))
		})
	}

	for _, mp := range providers {
		prov := strings.TrimSpace(mp.Metadata.Name)
		if seen[prov] == nil {
			seen[prov] = map[string]bool{}
		}
		for _, m := range mp.Spec.Models {
			for _, a := range m.Aliases {
				norm := strings.TrimSpace(strings.TrimPrefix(a, "@"))
				if norm == "" || norm == "default" || strings.EqualFold(norm, "inherit") || seen[prov][norm] {
					continue
				}
				seen[prov][norm] = true
				owners[norm] = append(owners[norm], AliasOwner{Provider: prov, Model: m.ID})
			}
		}
	}

	// The bare @default alias resolves through WORKSPACE.md first: a
	// modelAliases["default"] entry, then spec.defaultModels.completion, and
	// only then the first effective provider declaring defaultModels.completion
	// in resolution order.
	if ref, ok := wsAliasRef(wsAliases, "default"); ok {
		if owner, found := qualifyModelRef(providers, owners, ref); found {
			owners["default"] = []AliasOwner{owner}
		}
	} else if wsCompletion != "" {
		if owner, found := qualifyModelRef(providers, owners, wsCompletion); found {
			owners["default"] = []AliasOwner{owner}
		}
	}
	if owners["default"] == nil {
		for _, mp := range providers {
			dm := mp.Spec.DefaultModels.ForRole(warp.ModelRoleCompletion)
			if dm == "" {
				continue
			}
			if ref, ok := wsAliasRef(wsAliases, strings.TrimPrefix(dm, "@")); ok {
				dm = ref
			}
			if modelID, found := resolveProviderModelRef(mp, dm); found {
				owners["default"] = []AliasOwner{{Provider: strings.TrimSpace(mp.Metadata.Name), Model: modelID}}
				break
			}
			if owner, found := qualifyModelRef(providers, owners, dm); found {
				owners["default"] = []AliasOwner{owner}
				break
			}
		}
	}

	// WORKSPACE.md modelAliases shadow bare aliases outright: resolve every
	// declared target (against the manifest owners first, so aliases pointing
	// at another alias work) and let the workspace target own the alias even
	// when a manifest also declares it.
	wsResolved := make(map[string]AliasOwner, len(wsAliases))
	for name, target := range wsAliases {
		if name == "default" || target == "" {
			continue
		}
		if owner, found := qualifyModelRef(providers, owners, target); found {
			wsResolved[name] = owner
		}
	}
	for name, owner := range wsResolved {
		owners[name] = []AliasOwner{owner}
	}

	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]AliasAssignment, 0, len(names))
	for _, name := range names {
		all := owners[name]
		_, wsOverridden := wsAliases[name]
		// A WORKSPACE.md completion default outranks every manifest default,
		// so @default is workspace-owned in that case too: saving a manifest
		// cannot move it.
		if name == "default" && wsCompletion != "" {
			wsOverridden = true
		}
		aa := AliasAssignment{Alias: name, Owners: all, Workspace: wsOverridden}
		if len(all) > 1 {
			aa.Duplicates = all[1:]
		}
		out = append(out, aa)
	}
	return out
}

// providerDefaultRank orders WORKSPACE.md spec.defaultProvider first, matching
// warp's effectiveProviders promotion.
func providerDefaultRank(mp *warp.ModelProvider, defaultProv string) int {
	if mp.Metadata.Name == defaultProv || mp.QualifiedName() == defaultProv {
		return 0
	}
	return 1
}

// wsAliasRef looks up a WORKSPACE.md modelAliases entry by bare name, honouring
// both the plain and "@"-prefixed key forms.
func wsAliasRef(wsAliases map[string]string, name string) (string, bool) {
	if target, ok := wsAliases[name]; ok && target != "" {
		return target, true
	}
	if target, ok := wsAliases["@"+name]; ok && target != "" {
		return target, true
	}
	return "", false
}

// qualifyModelRef maps a WORKSPACE.md model reference ("provider/model", a bare
// model id/name, or another "@alias") to a concrete provider/model owner.
func qualifyModelRef(providers []*warp.ModelProvider, owners map[string][]AliasOwner, ref string) (AliasOwner, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return AliasOwner{}, false
	}
	if alias, isAlias := strings.CutPrefix(ref, "@"); isAlias {
		if list := owners[strings.TrimSpace(alias)]; len(list) > 0 {
			return list[0], true
		}
		return AliasOwner{}, false
	}
	if provName, modelID, ok := strings.Cut(ref, "/"); ok {
		for _, mp := range providers {
			if mp.Metadata.Name == provName || mp.QualifiedName() == provName {
				return AliasOwner{Provider: strings.TrimSpace(mp.Metadata.Name), Model: modelID}, true
			}
		}
		return AliasOwner{}, false
	}
	for _, mp := range providers {
		if modelID, found := resolveProviderModelRef(mp, ref); found {
			return AliasOwner{Provider: strings.TrimSpace(mp.Metadata.Name), Model: modelID}, true
		}
	}
	return AliasOwner{}, false
}

// resolveProviderModelRef maps a defaultModels reference (a raw model id or a
// bare "@alias") to the declaring model's id.
func resolveProviderModelRef(mp *warp.ModelProvider, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	if alias, ok := strings.CutPrefix(ref, "@"); ok {
		for _, m := range mp.Spec.Models {
			for _, a := range m.Aliases {
				if strings.EqualFold(strings.TrimPrefix(a, "@"), alias) {
					return m.ID, m.ID != ""
				}
			}
		}
		return "", false
	}
	for _, m := range mp.Spec.Models {
		if m.ID == ref || m.Name == ref {
			return m.ID, m.ID != ""
		}
	}
	return "", false
}

func normalizeAliasList(aliases []string) []string {
	out := make([]string, 0, len(aliases))
	seen := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		norm := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a), "@"))
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out
}

func removeAliases(aliases, drop []string) []string {
	if len(aliases) == 0 || len(drop) == 0 {
		return aliases
	}
	dropSet := make(map[string]bool, len(drop))
	for _, d := range drop {
		dropSet[d] = true
	}
	out := make([]string, 0, len(aliases))
	for _, a := range aliases {
		norm := strings.TrimSpace(strings.TrimPrefix(a, "@"))
		if dropSet[norm] {
			continue
		}
		out = append(out, a)
	}
	return out
}
