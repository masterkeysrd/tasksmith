# Plugin manifest (`PLUGIN.md` or `PLUGIN.yaml`)

A plugin publishes resources from a repository. Its manifest selects what to export; the consuming workspace chooses the source, version, namespace and imports. `warp.lock` verifies file contents, not resource names.

## Publisher fields

WARP looks for `PLUGIN.md` at the plugin root, then `PLUGIN.yaml` if the Markdown file is absent. Both use the [common envelope](../manifests.md#common-metadata-envelope).

| `spec` field | Type | Default and meaning |
| --- | --- | --- |
| `resourceDir` | string | `.agents`. Directory scanned relative to the plugin root. |
| `exports` | string array | Omitted, `[]` or `["*"]` exports all loadable resources. Explicit `Workspace`, `Context` or `Plugin` export patterns are rejected. |
| `hooks` | object | Optional lifecycle metadata. |
| `hooks.postInstall` | array of string arrays | One executable and its arguments per entry. WARP discovery returns the declarations; `InstallPlugin` and TaskSmith workspace loading do not execute them. |
| `instructions` | string | Populated from the Markdown body; not automatically injected into the workspace prompt. |

```markdown
---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: shared
  displayName: Shared engineering resources
  description: Review skills and engineering agents
spec:
  resourceDir: resources
  exports:
    - Agent/*
    - Skill/review
  hooks:
    postInstall:
      - [go, version]
---

The review skill accompanies the engineering agents.
```

Here the publisher scans `resources/` rather than `.agents/`. A consumer cannot use an import pattern to expose a skill other than `review`.

## Consumer fields (`WORKSPACE.md`)

| `spec.plugins[]` field | Type | Default and meaning |
| --- | --- | --- |
| `source` | string, required | Remote repository or local directory. A relative source beginning with `.` resolves from the workspace root. Local sources are read directly; remote sources are cached by source and version. |
| `version` | string | Defaults to `latest`; pin a version for stable selection. |
| `namespace` | string | Inferred from `source` when omitted. Must be a valid, non-reserved namespace; `local`, `workspace`, `user`, and `system` are unavailable. |
| `imports.include` | string array | Empty includes all exports. A nonempty list requires a match. |
| `imports.exclude` | string array | Applied after inclusion. A match removes the resource even if explicitly included. |

Export globs match case-insensitively against a short name, `Kind/name`, or the resource-relative path with or without its final extension. Imports also match `namespace/name` and `namespace/Kind/name`.

```yaml
spec:
  plugins:
    - source: github.com/acme/shared
      version: v1.2.0
      namespace: acme
      imports:
        include: [Agent/*, Skill/review]
        exclude: [Agent/experimental]
```

The imported skill is addressable as `acme/Skill/review`. The workspace can exclude `Agent/experimental` without changing the publisher's exports.

## Lockfile (`<workspace-root>/warp.lock`)

WARP installation writes this line-oriented file, not YAML. Each entry contains three whitespace-separated values. The two lines below show the format; the angle-bracket values stand for 64 hexadecimal digits each.

```text
github.com/acme/shared v1.2.0 h1:<resource-directory-sha256>
github.com/acme/shared v1.2.0/PLUGIN.md h1:<manifest-sha256>
```

| Token | Meaning |
| --- | --- |
| Source | Matches the declared `source`, including for local plugins. |
| Version or `version/manifest` | The bare version pins the resource tree; the second key includes the actual filename (`PLUGIN.md` or `PLUGIN.yaml`). |
| Digest | `h1:` followed by hexadecimal SHA-256. The tree digest covers sorted relative paths and file contents, excluding hidden entries except `.agents`; the manifest digest covers its file contents. |

On load, WARP checks each matching entry independently **before** registering resources. A mismatch fails workspace loading; an absent entry skips that check. Pinning a version alone does not verify its contents. TaskSmith copies an existing `warp.lock` when promoting workspace configuration to a worktree.

## See Also

- [Workspace fields and complete consumer example](./workspace.md)
- [Workspaces guide](../../guides/workspaces.md)
- [Manifest hierarchy and metadata](../manifests.md)
