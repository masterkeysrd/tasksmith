// Package permreview hosts the daemon-owned Permission Review Service: a
// calibrated, one-shot decision-model reviewer that evaluates risky
// permission cards in auto mode before the human is interrupted.
//
// # Authority boundary
//
// The reviewer is strictly advisory and asymmetric: deterministic permission
// decisions (workspace/agent policies, deny rules, untrusted-workspace
// posture) always win with zero model calls, and only a non-refused
// approve_once verdict meeting the calibrated probability threshold advances
// execution. Every other outcome (defer_to_user, below threshold, oversize
// input, missing model, error, timeout, cancellation) falls back cleanly to
// the existing human permission flow with zero false auto-denials.
//
// Model approval applies exactly once to the exact pending request. The
// reviewer never writes permissions.json, session Allow rules, or
// sandbox.json, and never evaluates in non-auto modes.
//
// # Rubric resolution
//
// The rubric and question spec come from the WARP Decision resource
// "permission-reviewer", resolved through the standard 4-tier hierarchy
// (local > workspace > user > system) so teams can version-control
// spec.models, tighter thresholds, and repo-specific rubric rules in
// .agents/decisions/permission-reviewer.decision.md. Three layers keep that
// override power safe: repository trust fingerprinting of
// .agents/decisions/** (repo Decisions never evaluate in untrusted or
// restricted workspaces), kernel write-denial of .agents/, and immutable Go
// floors on the approval probability that a manifest may tighten but never
// lower.
//
// # Injection defense
//
// The outbound payload is an allowlisted projection of the permission card
// plus the initiating user prompt text only. Repository files, .env,
// environment variables, prior tool outputs, and assistant messages are
// strictly excluded, and the rendered rubric frames card and user_intent as
// untrusted data.
package permreview
