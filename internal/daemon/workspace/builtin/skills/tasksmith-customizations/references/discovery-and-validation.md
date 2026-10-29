# Discovery, references, and validation

## Discovery and parsing

Workspace loading finds the workspace root, maps project directories, and then loads each project's context and resources. See [workspace and project context](workspace-and-context.md) for the workspace search, project mapping, and flat-root ownership rules. A project's namespace is derived from its lowercase relative path with separators replaced by hyphens, for example `services/api` becomes `services-api`; a final `.md`, `.yaml`, or `.yml` suffix is removed. The root project's namespace uses the lowercase root directory name. An absent `AGENTS.md` produces no project context.

Resource directories are walked recursively for `.md`, `.yaml`, and `.yml` files. Markdown resources normally require YAML frontmatter followed by a Markdown body; YAML files contain a YAML manifest. The parser can infer a Workspace or Context from a plain Markdown file named `WORKSPACE.md` or `AGENTS.md`, respectively. The parser recognizes those special names case-insensitively, but workspace discovery opens the exact `WORKSPACE.md` spelling, subject to filesystem behavior; project context discovery checks `AGENTS.md` case-insensitively.

Ordinary Markdown files without frontmatter are skipped during resource scanning. Other parse failures produce loader warnings rather than necessarily stopping discovery. Workspace, Context, and Plugin are structural resources, not ordinary resources loaded from a project's `.agents/` scan.

## References and shadowing

Agent skill, command, subagent, and rule selectors can use short names, qualified names, paths, and patterns. For example, `system/Skill/finance` selects a system Skill explicitly, while `finance` resolves among visible scopes. Agent selectors have additional wildcard and empty-list behavior described in [agents](agents.md).

In a project scope, the search order is current project, workspace, user, system, then plugin namespaces. At workspace-root scope, project-specific resources are excluded. `local/Kind/name` is a virtual project alias with workspace fallback; concrete project-qualified names use the project slug. Other qualified names are looked up directly. WARP also supports a relative-path fallback: if direct lookup fails, a path such as `skills/go.md` can match a resource name derived from its path or basename. It is not a filesystem read at reference time.

An effective query chooses a winner for a short name by namespace priority. Filter by kind when looking for a particular resource type: WARP's effective query deduplicates by name within the selected results, so a mixed-kind query can shadow different kinds with the same name. Equal-priority ties use the lexicographically first qualified name; prefer qualified names when selecting among plugins.

## Validation versus runtime resolution

WARP exposes an explicit registry `Validate()` operation; TaskSmith's normal workspace loading does not invoke it. Successful discovery therefore does not prove every manifest or cross-reference is valid. Agent resolution can skip missing selectors even though an explicit validation pass would reject missing non-glob references. Do not assume invalid references will always fail the session at startup.

| Validation area | Checks performed by WARP's registry validation |
| --- | --- |
| Base metadata | `apiVersion`, a recognized `kind`, and `metadata.name` must be present. |
| Agents | Parent references and non-glob skill, command, subagent, and rule references must resolve to the expected kind; inheritance cycles fail. A declared model found in provider metadata must support completion. |
| Workspace and providers | Checks default agent, default provider when providers exist, rule references, and declared model role capabilities. Project paths are checked during mapping. |
| Decisions | Question names are nonempty and unique; types and probability bounds are valid; choices and scores have at least two named options or levels. A declared model found in provider metadata must support decisions. |
| Rules | Decision and non-glob tool or toolkit trigger references must resolve. |
| Skills and commands | Non-glob tool references and command agent references must resolve; command context must be `inline`, `fork`, or `clean`. |
| Toolkits and MCP | Toolkit references must resolve and inline entries need a name and command; MCP transport must be recognized and have its required command or endpoint. |
| Plugins and tools | Structural export patterns are rejected, and standalone tools are checked against workspace tool policy. |

Glob selectors are generally skipped by reference validation rather than required to match. Model capability checks do not prove that an undeclared model exists or is available. Validation reports the first error and does not replace runtime permissions, sandboxing, or provider connectivity checks.
