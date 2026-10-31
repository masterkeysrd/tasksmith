// Package permissions implements the daemon's permission gate: it evaluates
// planned tool invocations against workspace policies, the active permission
// mode, safety classifications, and allow/deny rules, minting a
// [github.com/masterkeysrd/tasksmith/api.PermissionRequest] whenever a human
// decision is required.
//
// # Core Types
//
// A [Permissions] value is a pair of allow and deny rule lists. Rules follow the
// "tool(pattern)" syntax produced by [FormatRule] and parsed by [ParseRule], and
// are matched against a [github.com/masterkeysrd/tasksmith/internal/daemon/tools.Plan]
// by [MatchRule].
//
// Two managers are exposed:
//   - [Manager] (created via [New]) coordinates the on-disk, cross-session
//     persistence of project- and global-scope rules and binds per-session
//     evaluators via [Manager.Attach].
//   - [SessionManager] (created via [Manager.Attach] or the detached [NewSession])
//     owns the active [github.com/masterkeysrd/tasksmith/api.PermissionMode], the
//     in-memory session rules, and the [SessionManager.Evaluate] decision logic.
//
// # Rule Syntax & Matching
//
// A rule names a tool and a glob-like pattern over the plan's target. Matching
// ([matchTarget]) supports exact equality, the wildcard "*" and "?", single-segment
// "*" and multi-segment "**" globs (via [globToRegex]), the directory-prefix
// "dir/**" form, and URL host matching when the target parses as a URL. [DeriveGrantRules]
// turns a planned invocation into candidate grant patterns for a
// [github.com/masterkeysrd/tasksmith/api.PermissionRequest.GrantPatterns] field.
//
// # Scopes & Persistence
//
// [Scope] values bound where rules live:
//   - [ScopeSession]: in-memory only, owned by the [SessionManager].
//   - [ScopeProject]: persisted under the workspace data directory
//     (`<workspace-data-dir>/permissions.json`).
//   - [ScopeGlobal]: persisted under the user config directory
//     (`<config-dir>/permissions.json`, resolved via xdg).
//
// [Manager.Get], [Manager.Set], [Manager.AddRule], and [Manager.RemoveRule]
// operate on the project and global scopes; a [ScopeSession] argument is rejected
// with [ErrInvalidScope]. Writes are validated ([Permissions.Validate]) and
// deduplicated before being flushed to disk.
//
// # Evaluation Flow
//
// [SessionManager.Evaluate] resolves a [github.com/masterkeysrd/tasksmith/internal/daemon/tools.Plan]
// through a fixed precedence:
//  1. Workspace tool policies ([warp.Policies] include/exclude globs,
//     allow-dangerous and allow-open-world flags, and tool annotations) — a
//     policy violation denies outright.
//  2. Deny rules take precedence: any matching deny rule (session, project, or
//     global) yields a denial.
//  3. The active mode and matching allow rules decide auto-approval:
//     [github.com/masterkeysrd/tasksmith/api.ModeAuto] allows everything;
//     [github.com/masterkeysrd/tasksmith/api.ModePlan] allows only read-only
//     safe plans; [github.com/masterkeysrd/tasksmith/api.ModeAsk] and
//     [github.com/masterkeysrd/tasksmith/api.ModeDefault] allow matching safe
//     plans; and [github.com/masterkeysrd/tasksmith/api.ModeAcceptEdits]
//     additionally auto-approves non-unsafe write actions.
//  4. Otherwise a UUIDv7 [github.com/masterkeysrd/tasksmith/api.PermissionRequest]
//     is minted and an "ask" decision is returned.
//
// # Decisions
//
// [SessionManager.ApplyDecision] records a user's
// [github.com/masterkeysrd/tasksmith/api.PermissionDecision]:
// [github.com/masterkeysrd/tasksmith/api.DecisionAllowOnce] is a no-op,
// [github.com/masterkeysrd/tasksmith/api.DecisionAllowSession] appends an allow
// rule at the decision's scope, and [github.com/masterkeysrd/tasksmith/api.DecisionReject]
// returns [github.com/masterkeysrd/tasksmith/internal/daemon/tools.ErrPermissionDenied].
//
// # Integration
//
// [github.com/masterkeysrd/tasksmith/internal/daemon/session.Session] holds a
// [Manager] and attaches a [SessionManager] per session (via [Manager.Attach]),
// which the [github.com/masterkeysrd/tasksmith/internal/daemon/harness] graph
// consumes as its permission gate through [SessionManager.Evaluate].
package permissions
