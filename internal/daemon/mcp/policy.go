package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/warp"
	"gopkg.in/yaml.v3"
)

// Policy section keys under spec.policies, in the order they are declared.
const (
	policyKeyTools     = "tools"
	policyKeyPrompts   = "prompts"
	policyKeyResources = "resources"
)

// ErrManifestReadOnly reports that an MCP server's declaration cannot be edited
// on disk, either because it is built-in, has no backing file, or the file is not
// writable.
var ErrManifestReadOnly = errors.New("mcp manifest is not writable")

// policySection maps an item kind to the spec.policies key governing it.
func policySection(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "tool":
		return policyKeyTools, nil
	case "prompt":
		return policyKeyPrompts, nil
	case "resource":
		return policyKeyResources, nil
	default:
		return "", fmt.Errorf("unknown MCP policy kind %q", kind)
	}
}

// PolicyChange describes an applied manifest policy edit.
type PolicyChange struct {
	// Section is the spec.policies section that was edited.
	Section string
	// Value is the item the manifest now permits.
	Value string
	// RemovedExclude is an exclude pattern deleted because it named the value
	// exactly.
	RemovedExclude string
	// Included is true when the value was appended to the section's include list.
	Included bool
	// Unchanged is true when the manifest already permitted the value.
	Unchanged bool
}

// String renders a short human-readable summary of the edit.
func (c PolicyChange) String() string {
	switch {
	case c.Unchanged:
		return fmt.Sprintf("%q is already allowed by spec.policies.%s", c.Value, c.Section)
	case c.RemovedExclude != "" && c.Included:
		return fmt.Sprintf("removed exclude %q and added %q to spec.policies.%s include", c.RemovedExclude, c.Value, c.Section)
	case c.RemovedExclude != "":
		return fmt.Sprintf("removed exclude %q from spec.policies.%s", c.RemovedExclude, c.Section)
	default:
		return fmt.Sprintf("added %q to spec.policies.%s include", c.Value, c.Section)
	}
}

// AllowPolicyItem updates the manifest file backing def so value is permitted
// under the policy section for kind ("tool", "prompt", or "resource").
//
// The edit preserves comments and formatting by mutating the file's YAML node
// tree, and applies the smallest change that can help: an exclude entry naming
// the value exactly is removed, and when the value is restricted by an include
// list it is appended to that list. The edited document is re-parsed through the
// same loader the daemon uses and re-checked before anything is written, so a
// caller never reports a permission that is not actually in effect.
//
// Some blocks have no safe automatic edit. An exclude *pattern* such as delete_*
// keeps matching the value even after it is listed in include, because exclusion
// runs after inclusion, and deleting the pattern would silently expose every other
// item it covers. Those cases fail with an error wrapping [ErrNoSafePolicyEdit]
// naming the patterns to narrow by hand.
//
// Built-in servers and manifests without a writable local file are rejected with
// an error wrapping [ErrManifestReadOnly].
func AllowPolicyItem(def *warp.MCP, kind, value string) (PolicyChange, error) {
	if def == nil {
		return PolicyChange{}, errors.New("mcp definition is nil")
	}
	section, err := policySection(kind)
	if err != nil {
		return PolicyChange{}, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return PolicyChange{}, errors.New("MCP policy value must not be empty")
	}

	path, err := writableManifestPath(def)
	if err != nil {
		return PolicyChange{}, err
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return PolicyChange{}, fmt.Errorf("read MCP manifest %s: %w", path, err)
	}
	current, err := parseManifest(path, string(original))
	if err != nil {
		return PolicyChange{}, err
	}
	if manifestAllows(current, kind, value) {
		return PolicyChange{Section: section, Value: value, Unchanged: true}, nil
	}

	edited, change, err := editPolicyYAML(string(original), path, section, value)
	if err != nil {
		return PolicyChange{}, err
	}
	if change.Unchanged {
		// The value is blocked but no safe edit exists: nothing names it exactly in
		// exclude, and it is not restricted by an include list. Reporting success
		// would claim a permission that is not in effect.
		return PolicyChange{}, cannotPermitError(path, section, value, current)
	}

	updated, err := parseManifest(path, edited)
	if err != nil {
		return PolicyChange{}, fmt.Errorf("edited MCP manifest %s no longer parses: %w", path, err)
	}
	if !manifestAllows(updated, kind, value) {
		return PolicyChange{}, cannotPermitError(path, section, value, updated)
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(edited), mode); err != nil {
		return PolicyChange{}, fmt.Errorf("write MCP manifest %s: %w", path, err)
	}
	change.Value = value
	return change, nil
}

// writableManifestPath resolves the on-disk file backing def, rejecting built-in
// declarations, embedded resources without a real path, and non-manifest files.
func writableManifestPath(def *warp.MCP) (string, error) {
	name := strings.TrimSpace(def.Metadata.Name)
	path := strings.TrimSpace(def.GetPath())
	if strings.EqualFold(strings.TrimSpace(def.Namespace), warp.NamespaceSystem) ||
		strings.EqualFold(strings.TrimSpace(def.Namespace), "builtin") {
		return "", fmt.Errorf("%w: MCP server %q is a built-in resource", ErrManifestReadOnly, name)
	}
	if path == "" {
		return "", fmt.Errorf("%w: MCP server %q has no manifest file on disk", ErrManifestReadOnly, name)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: MCP manifest %q for server %q is not a local file", ErrManifestReadOnly, path, name)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" && ext != ".md" {
		return "", fmt.Errorf("%w: MCP manifest %s has unsupported extension %q", ErrManifestReadOnly, path, ext)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrManifestReadOnly, path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrManifestReadOnly, path)
	}
	if info.Mode().Perm()&0o200 == 0 {
		return "", fmt.Errorf("%w: %s is not writable", ErrManifestReadOnly, path)
	}
	return path, nil
}

// parseManifest decodes manifest content through the workspace loader so policy
// checks use exactly the semantics the daemon enforces.
func parseManifest(path, content string) (*warp.MCP, error) {
	res, err := warp.Parse(path, content)
	if err != nil {
		return nil, fmt.Errorf("parse MCP manifest %s: %w", path, err)
	}
	m, ok := res.Resource.(*warp.MCP)
	if !ok {
		return nil, fmt.Errorf("MCP manifest %s does not declare an MCP resource", path)
	}
	return m, nil
}

// manifestAllows reports whether def permits value for the given item kind.
func manifestAllows(def *warp.MCP, kind, value string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "tool":
		return def.AllowsTool(value)
	case "prompt":
		return def.AllowsPrompt(value)
	case "resource":
		return def.AllowsResource(value)
	default:
		return false
	}
}

// splitManifestFrontMatter separates a manifest's YAML document from any Markdown
// body that follows it, and reports whether the file is delimited by front-matter
// fences. Plain YAML manifests return their whole document with wrapped false.
func splitManifestFrontMatter(path, content string) (frontMatter, body string, wrapped bool, err error) {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".md" {
		return content, "", false, nil
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "" {
		return "", "", false, fmt.Errorf("MCP manifest %s has no YAML front matter to edit", path)
	}
	return parts[1], strings.TrimPrefix(parts[2], "\n"), true, nil
}

// joinManifestFrontMatter reassembles a manifest from its YAML document and
// Markdown body, restoring front-matter fences when the file used them.
func joinManifestFrontMatter(frontMatter, body string, wrapped bool) string {
	if !wrapped {
		return frontMatter
	}
	trimmed := strings.Trim(frontMatter, "\n")
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(trimmed)
	out.WriteString("\n---\n")
	if body != "" {
		out.WriteString(strings.TrimPrefix(body, "\n"))
	}
	return out.String()
}

// editPolicyYAML applies the minimal policy edit that permits value and returns
// the new file content. When removing an exact exclude entry already suffices and
// no include list is active, nothing is appended and [PolicyChange.Included]
// stays false.
func editPolicyYAML(content, path, section, value string) (string, PolicyChange, error) {
	frontMatter, body, wrapped, err := splitManifestFrontMatter(path, content)
	if err != nil {
		return "", PolicyChange{}, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(frontMatter), &doc); err != nil {
		return "", PolicyChange{}, fmt.Errorf("parse YAML in %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return "", PolicyChange{}, fmt.Errorf("YAML document in %s is empty", path)
	}

	change := PolicyChange{Section: section}
	sectionNode := mappingValue(mappingValue(doc.Content[0], "spec"), "policies")
	filter := mappingValue(sectionNode, section)
	if filter == nil {
		// The manifest declares no filter for this section, so every item is
		// permitted; the caller's pre-check already handles that case.
		return "", PolicyChange{}, fmt.Errorf("MCP manifest %s declares no spec.policies.%s to edit", path, section)
	}

	if seq := mappingValue(filter, "exclude"); seq != nil && seq.Kind == yaml.SequenceNode {
		for i, item := range seq.Content {
			if strings.TrimSpace(item.Value) == value {
				seq.Content = slices.Delete(seq.Content, i, i+1)
				change.RemovedExclude = value
				break
			}
		}
	}

	// When an include allowlist is active and does not already match value, the
	// value must also be appended to include — even if an exact exclude entry was
	// just removed — so the resulting policy permits it.
	if include := mappingValue(filter, "include"); include != nil && include.Kind == yaml.SequenceNode && len(include.Content) > 0 && !includeAllows(include, value) {
		include.Content = append(include.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		change.Included = true
	}

	if change.RemovedExclude == "" && !change.Included {
		change.Unchanged = true
		return content, change, nil
	}

	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", PolicyChange{}, fmt.Errorf("encode updated manifest %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return "", PolicyChange{}, fmt.Errorf("encode updated manifest %s: %w", path, err)
	}
	return joinManifestFrontMatter(out.String(), body, wrapped), change, nil
}

// ErrNoSafePolicyEdit reports that no automatic manifest edit can permit an item
// without also widening what the server exposes.
var ErrNoSafePolicyEdit = errors.New("no safe policy edit")

// cannotPermitError explains why an item stays blocked, naming the exclude
// patterns that still match it so the user knows what to narrow by hand.
func cannotPermitError(path, section, value string, def *warp.MCP) error {
	var patterns []string
	if filter := policyFilter(def, section); filter != nil {
		for _, pattern := range filter.Exclude {
			if pattern = strings.TrimSpace(pattern); pattern != "" {
				patterns = append(patterns, strconv.Quote(pattern))
			}
		}
	}
	if len(patterns) == 0 {
		return fmt.Errorf("%w: cannot permit %q in %s: spec.policies.%s blocks it in a way that has no safe automatic edit",
			ErrNoSafePolicyEdit, value, path, section)
	}
	return fmt.Errorf("%w: cannot permit %q in %s: spec.policies.%s.exclude still matches it through %s; narrow that pattern in the manifest",
		ErrNoSafePolicyEdit, value, path, section, strings.Join(patterns, ", "))
}

// policyFilter returns the filter a manifest declares for one policy section.
func policyFilter(def *warp.MCP, section string) *warp.ResourceFilter {
	if def == nil || def.Spec.Policies == nil {
		return nil
	}
	switch section {
	case policyKeyTools:
		return def.Spec.Policies.Tools
	case policyKeyPrompts:
		return def.Spec.Policies.Prompts
	case policyKeyResources:
		return def.Spec.Policies.Resources
	default:
		return nil
	}
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.TrimSpace(node.Content[i].Value) == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// includeAllows reports whether a non-empty include sequence already matches value.
func includeAllows(seq *yaml.Node, value string) bool {
	if seq == nil || len(seq.Content) == 0 {
		return true
	}
	patterns := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		if v := strings.TrimSpace(item.Value); v != "" {
			patterns = append(patterns, v)
		}
	}
	if len(patterns) == 0 {
		return true
	}
	probe := &warp.MCP{
		Spec: warp.MCPSpec{
			Policies: &warp.MCPPolicies{
				Tools: &warp.ResourceFilter{Include: patterns},
			},
		},
	}
	return probe.AllowsTool(value)
}
