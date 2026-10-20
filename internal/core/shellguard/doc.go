// Package shellguard provides static AST-based security analysis and command
// classification for shell execution in TaskSmith.
//
// # Architecture Overview
//
// A [Guard] instance is constructed via [New] with a dependency-injected [Config]
// (specifying WorkspaceDir, HomeDir, ProtectedDirs, MaxOperations, and a declarative
// [Policy]):
//
//  1. AST Parsing & Nested Extraction ([Parse]):
//     Uses mvdan.cc/sh/v3/syntax to parse shell scripts into a [CommandChain] of
//     [Pipeline] and [ParsedCommand] nodes. Recursively walks compound shell
//     constructs—including pipelines, logical chains (&&, ||, ;), subshells ((...)),
//     command blocks ({ ...; }), control-flow clauses (if, for, while, until, case,
//     function declarations), command substitutions ($(cmd) and `cmd`), and wrapper
//     commands (sudo, npx, bash -c, sh -c).
//
//  2. Virtual Working Directory Tracking & Value-Copied Subshell Isolation:
//     Tracks directory transitions (cd) across sequential commands in a virtual CWD
//     state machine using the configured HomeDir for tilde (~) expansion without
//     runtime OS syscalls. When entering a subshell (ParsedCommand.Subshell),
//     command substitution, or multi-command shell wrapper, the classifier passes
//     a value copy of the tracker state so directory changes inside (cd /tmp && ls)
//     never leak to subsequent commands outside the subshell.
//
//  3. Declarative Classification & Target Extraction ([Policy], [CommandRule]):
//     Evaluates each command against the Guard's [Policy] to assign an [ActionType]
//     ([ActionRead], [ActionUnknown], [ActionWrite], [ActionExec], [ActionDelete])
//     and a [SafetyLevel] ([SafetySafe], [SafetyUnknown], [SafetyUnsafe]).
//     Each [CommandRule] declaratively specifies its [TargetMode] ([TargetsAll],
//     [TargetsNone], [TargetsSkipFirst], [TargetsLast], [TargetsAfterDashDash],
//     [TargetsPredicateStop]), ValueFlags, PathFlags, and SubcommandModes so a single
//     generic extractor accurately identifies filesystem targets without false
//     positives on echo strings, grep/sed patterns, or git commit -m messages.
//     Commands escaping WorkspaceDir or modifying ProtectedDirs (.git, .bare, .agents)
//     are flagged as [SafetyUnsafe].
//
//  4. Compound Operation Collapsing ([Guard.Analyze] vs [Guard.AnalyzeDetailed]):
//     To prevent approval prompt bloat on multi-step scripts, [Guard.Analyze]
//     collapses command chains exceeding MaxOperations (default [DefaultMaxOperations])
//     or containing control-flow clauses / inline interpreter scripts into a single
//     summary [Operation] marked with IsCompound: true, taking the highest-severity
//     [ActionType] and worst [SafetyLevel] across all constituent operations.
package shellguard
