package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/warp"
)

// ErrToolNotFound marks operations naming a tool the workspace does not declare.
var ErrToolNotFound = errors.New("workspace: tool not found")

// ErrMCPNotFound marks operations naming an MCP server the workspace does not
// declare.
var ErrMCPNotFound = errors.New("workspace: MCP server not found")

// ErrManifestReadOnly marks a toggle attempt against a resource whose manifest
// is not writable on disk (built-in, embedded, or read-only). Such resources are
// excluded per repository through WORKSPACE.md policies instead.
var ErrManifestReadOnly = errors.New("workspace: manifest is read-only")

// ErrNoSafePolicyEdit reports that no automatic WORKSPACE.md edit can change a
// resource's availability, because a glob pattern would keep matching it.
var ErrNoSafePolicyEdit = errors.New("workspace: no safe policy edit")

// ResourceRecord is one tool or MCP server as the workspace resolves it: which
// manifest defines it, whether it is available, and where a toggle would land.
type ResourceRecord struct {
	Name       string
	Kind       warp.Kind
	Def        warp.Resource
	Namespace  string
	Path       string
	Available  bool
	DisabledBy api.DisabledReason
	Writable   bool // the owning manifest may be patched in place
}

// toolRecords lists every tool snapshot in scope, including tools disabled
// through spec.enabled or WORKSPACE.md policies, since listings and toggling
// must see what availability filtering hides.
func (w *Workspace) toolRecords() []ResourceRecord {
	cat := w.Catalog()
	snaps := cat.ToolSnapshots()
	out := make([]ResourceRecord, 0, len(snaps))
	for _, snap := range snaps {
		if snap.Def == nil {
			continue
		}
		out = append(out, w.resourceRecord(cat, snap.Name, warp.KindTool, snap.Def, snap.Namespace, snap.Def.GetPath(), snap.Enabled, snap.DisabledBy))
	}
	return out
}

// mcpRecords lists every MCP server snapshot in scope, disabled servers included.
func (w *Workspace) mcpRecords() []ResourceRecord {
	cat := w.Catalog()
	snaps := cat.MCPSnapshots()
	out := make([]ResourceRecord, 0, len(snaps))
	for _, snap := range snaps {
		if snap.Def == nil {
			continue
		}
		out = append(out, w.resourceRecord(cat, snap.Name, warp.KindMCP, snap.Def, snap.Namespace, snap.Def.GetPath(), snap.Enabled, snap.DisabledBy))
	}
	return out
}

func (w *Workspace) resourceRecord(cat *Catalog, name string, kind warp.Kind, def warp.Resource, namespace, path string, available bool, disabledBy api.DisabledReason) ResourceRecord {
	path = strings.TrimSpace(path)
	writable := writableResourceNamespace(cat, namespace) && path != "" && filepath.IsAbs(path)
	if writable {
		if info, err := os.Stat(path); err != nil || info.IsDir() || info.Mode().Perm()&0o200 == 0 {
			writable = false
		}
	}
	return ResourceRecord{
		Name:       name,
		Kind:       kind,
		Def:        def,
		Namespace:  namespace,
		Path:       path,
		Available:  available,
		DisabledBy: disabledBy,
		Writable:   writable,
	}
}

// writableResourceNamespace reports whether a resource of the given namespace
// has a workspace-editable manifest. Built-in (system) resources never do;
// workspace and project tiers do.
func writableResourceNamespace(cat *Catalog, ns string) bool {
	ns = strings.ToLower(strings.TrimSpace(ns))
	if proj := cat.Project(); proj != nil && ns == strings.ToLower(proj.Name) {
		return true
	}
	switch ns {
	case warp.NamespaceWorkspace, warp.NamespaceLocal:
		return true
	default:
		return false
	}
}

// ToolRecord resolves one tool across the workspace listing.
func (w *Workspace) ToolRecord(name string) (ResourceRecord, bool) {
	name = strings.TrimSpace(name)
	for _, rec := range w.toolRecords() {
		if rec.Name == name {
			return rec, true
		}
	}
	return ResourceRecord{}, false
}

// MCPRecord resolves one MCP server across the workspace listing.
func (w *Workspace) MCPRecord(name string) (ResourceRecord, bool) {
	name = strings.TrimSpace(name)
	for _, rec := range w.mcpRecords() {
		if rec.Name == name {
			return rec, true
		}
	}
	return ResourceRecord{}, false
}

// SetToolEnabled makes a tool available or unavailable for this workspace.
//
// Availability is deterministic from manifests, so the toggle writes wherever
// that resource's opt-out can live:
//
//   - Disabling a tool with a writable manifest patches spec.enabled: false into
//     that manifest, so the opt-out travels with the resource.
//   - Disabling a built-in (or any tool with no writable manifest) adds the name
//     to WORKSPACE.md spec.policies.tools.exclude, scoping the exclusion to this
//     repository without touching a global or built-in file.
//   - Enabling clears whichever exclusion exists: the WORKSPACE.md entry first,
//     then the manifest flag when the manifest itself is disabled.
func (w *Workspace) SetToolEnabled(name string, enabled bool) error {
	rec, ok := w.ToolRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if serverName, _, isMCP := warp.ParseMCPToolName(rec.Name); isMCP {
		return fmt.Errorf("%w: tool %q is exposed by MCP server %q; enable or disable the whole server instead", ErrToolNotFound, rec.Name, serverName)
	}
	return w.setResourceEnabled(rec, enabled)
}

// SetMCPEnabled makes an MCP server available or unavailable for this workspace.
// A disabled server never spawns and exposes zero tools, prompts, and resources.
// The landing rules match [Workspace.SetToolEnabled], with
// spec.policies.mcps.exclude as the workspace-level fallback.
func (w *Workspace) SetMCPEnabled(name string, enabled bool) error {
	rec, ok := w.MCPRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrMCPNotFound, name)
	}
	return w.setResourceEnabled(rec, enabled)
}

func (w *Workspace) setResourceEnabled(rec ResourceRecord, enabled bool) error {
	if rec.Available == enabled {
		return nil
	}
	if !enabled {
		if rec.Writable {
			return patchResourceManifestEnabled(rec, false)
		}
		return w.patchWorkspaceExclusion(rec, true)
	}

	manifestDisabled := rec.DisabledBy == api.DisabledByManifest
	if manifestDisabled && !rec.Writable {
		return fmt.Errorf("%w: %s is disabled in its owning manifest", ErrManifestReadOnly, rec.Name)
	}
	if resourcePolicyExcluded(w.Catalog().Policies(), rec) {
		if err := w.patchWorkspaceExclusion(rec, false); err != nil {
			return err
		}
	}
	if manifestDisabled {
		return patchResourceManifestEnabled(rec, true)
	}
	return nil
}

// patchResourceManifestEnabled writes spec.enabled through warp's document
// editor, which patches only the changed value into the original file so
// comments, key order, and unknown fields survive. The edited document is
// re-parsed before writing so a caller never reports a patch that leaves an
// unparseable manifest behind.
func patchResourceManifestEnabled(rec ResourceRecord, enabled bool) error {
	data, err := os.ReadFile(rec.Path)
	if err != nil {
		return fmt.Errorf("workspace: read manifest %s: %w", rec.Path, err)
	}
	doc, err := warp.ParseDocument(rec.Path, string(data))
	if err != nil {
		return fmt.Errorf("workspace: parse manifest %s: %w", rec.Path, err)
	}
	if doc.Resource.GetName() != rec.Name || doc.Resource.GetKind() != rec.Kind {
		return fmt.Errorf("workspace: resource changed in manifest %s", rec.Path)
	}
	switch res := doc.Resource.(type) {
	case *warp.Tool:
		res.Spec.Enabled = enabledPtr(enabled)
	case *warp.MCP:
		res.Spec.Enabled = enabledPtr(enabled)
	default:
		return fmt.Errorf("workspace: %s is not a Tool or MCP manifest", rec.Path)
	}
	out, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("workspace: format manifest %s: %w", rec.Path, err)
	}
	if _, err := warp.Parse(rec.Path, string(out)); err != nil {
		return fmt.Errorf("workspace: edited manifest %s no longer parses: %w", rec.Path, err)
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(rec.Path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := fsutils.WriteFileAtomic(rec.Path, out, mode); err != nil {
		return fmt.Errorf("workspace: write manifest %s: %w", rec.Path, err)
	}
	return nil
}

// SetWorkspaceToolExcluded adds or removes name in WORKSPACE.md
// spec.policies.tools.exclude, the workspace-scoped exclusion list that also
// covers built-in and global tools.
func (w *Workspace) SetWorkspaceToolExcluded(name string, excluded bool) error {
	rec, ok := w.ToolRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return w.patchWorkspaceExclusion(rec, excluded)
}

// SetWorkspaceMCPExcluded adds or removes name in WORKSPACE.md
// spec.policies.mcps.exclude.
func (w *Workspace) SetWorkspaceMCPExcluded(name string, excluded bool) error {
	rec, ok := w.MCPRecord(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrMCPNotFound, name)
	}
	return w.patchWorkspaceExclusion(rec, excluded)
}

// patchWorkspaceExclusion edits WORKSPACE.md policies. A file with front matter
// goes through warp's document editor, which patches only the changed values
// into the original YAML tree so comments, key order, and blank lines survive.
// A file without front matter (an inferred WORKSPACE.md carrying only
// instructions) is rewritten as a full workspace definition that keeps those
// instructions, because a policy block cannot be attached to an
// instructions-only document.
func (w *Workspace) patchWorkspaceExclusion(rec ResourceRecord, excluded bool) error {
	rootDir := w.RootDir()
	wsPath := filepath.Join(rootDir, warp.WorkspaceFileName)

	data, err := os.ReadFile(wsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("workspace: read WORKSPACE.md: %w", err)
	}

	var out []byte
	if errors.Is(err, os.ErrNotExist) || len(strings.TrimSpace(string(data))) == 0 {
		def := &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: filepath.Base(rootDir)},
			},
			Spec: warp.WorkspaceDefSpec{Projects: []string{"."}},
		}
		if !applyWorkspaceExclusion(def, rec, excluded) {
			return policyConflict(rec, def.Spec.Policies)
		}
		out, err = warp.Format(def)
		if err != nil {
			return fmt.Errorf("workspace: format WORKSPACE.md: %w", err)
		}
	} else {
		doc, perr := warp.ParseDocument(wsPath, string(data))
		if perr != nil {
			return fmt.Errorf("workspace: parse WORKSPACE.md: %w", perr)
		}
		def, ok := doc.Resource.(*warp.WorkspaceDef)
		if !ok {
			return fmt.Errorf("workspace: %s is not a Workspace manifest", wsPath)
		}
		if doc.Inferred {
			// No front matter exists: author a full definition that carries the
			// original instructions plus the new policy block.
			fresh := &warp.WorkspaceDef{
				BaseResource: warp.BaseResource{
					APIVersion: warp.APIVersion,
					Kind:       warp.KindWorkspace,
					Metadata:   warp.Metadata{Name: filepath.Base(rootDir)},
				},
				Spec: warp.WorkspaceDefSpec{Instructions: def.Spec.Instructions},
			}
			if !applyWorkspaceExclusion(fresh, rec, excluded) {
				return policyConflict(rec, fresh.Spec.Policies)
			}
			out, err = warp.Format(fresh)
			if err != nil {
				return fmt.Errorf("workspace: format WORKSPACE.md: %w", err)
			}
		} else {
			if !applyWorkspaceExclusion(def, rec, excluded) {
				return policyConflict(rec, def.Spec.Policies)
			}
			out, err = doc.Bytes()
			if err != nil {
				return fmt.Errorf("workspace: format WORKSPACE.md: %w", err)
			}
		}
	}

	if _, err := warp.Parse(wsPath, string(out)); err != nil {
		return fmt.Errorf("workspace: edited WORKSPACE.md no longer parses: %w", err)
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(wsPath); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := fsutils.WriteFileAtomic(wsPath, out, mode); err != nil {
		return fmt.Errorf("workspace: write WORKSPACE.md: %w", err)
	}
	return nil
}

// applyWorkspaceExclusion adds or removes name from the kind's exclusion list
// and reports whether the resulting policy matches the caller's intent. A
// removal that leaves a glob pattern still matching the name reports false so
// the caller never writes a change that does not actually restore availability.
func applyWorkspaceExclusion(def *warp.WorkspaceDef, rec ResourceRecord, excluded bool) bool {
	if def.Spec.Policies == nil {
		def.Spec.Policies = &warp.Policies{}
	}
	policies := def.Spec.Policies
	switch rec.Kind {
	case warp.KindTool:
		if policies.Tools == nil {
			policies.Tools = &warp.ToolPolicies{}
		}
		policies.Tools.Exclude = applyExclusion(policies.Tools.Exclude, rec, excluded)
	case warp.KindMCP:
		if policies.MCPs == nil {
			policies.MCPs = &warp.ResourceFilter{}
		}
		policies.MCPs.Exclude = applyExclusion(policies.MCPs.Exclude, rec, excluded)
	}
	return resourcePolicyExcluded(policies, rec) == excluded
}

func resourcePolicyExcluded(policies *warp.Policies, rec ResourceRecord) bool {
	if policies == nil {
		return false
	}
	if rec.Kind == warp.KindTool {
		return toolPolicyExcluded(policies.Tools, rec.Def)
	}
	return mcpFilterExcluded(policies.MCPs, rec.Def)
}

func policyConflict(rec ResourceRecord, policies *warp.Policies) error {
	var patterns []string
	switch rec.Kind {
	case warp.KindTool:
		patterns = policies.Tools.Exclude
	case warp.KindMCP:
		patterns = policies.MCPs.Exclude
	}
	short, namespaced, qualified := resourceIdentities(rec.Def)
	for _, pattern := range patterns {
		if matchResourcePattern(pattern, short, namespaced, qualified) {
			return fmt.Errorf("%w: %s is still excluded by the WORKSPACE.md pattern %q",
				ErrNoSafePolicyEdit, rec.Name, pattern)
		}
	}
	return fmt.Errorf("%w: WORKSPACE.md still excludes %s; adjust its include or exclude patterns",
		ErrNoSafePolicyEdit, rec.Name)
}

// applyExclusion edits exact identities while preserving patterns that may
// affect other resources.
func applyExclusion(list []string, rec ResourceRecord, excluded bool) []string {
	short, namespaced, qualified := resourceIdentities(rec.Def)
	list = slices.DeleteFunc(slices.Clone(list), func(p string) bool {
		return p == short || (!excluded && (p == namespaced || p == qualified))
	})
	if excluded {
		list = append(list, short)
		slices.Sort(list)
	}
	return list
}
