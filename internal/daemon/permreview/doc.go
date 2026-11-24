// Package permreview hosts the daemon-owned Permission Review Service: a
// calibrated, one-shot decision-model reviewer that evaluates risky
// permission cards in auto mode before the human is interrupted.
//
// # Authority boundary
//
// The reviewer is strictly advisory and asymmetric: deterministic permission
// decisions (workspace/agent policies, deny rules, untrusted-workspace
// posture) always win with zero model calls, and only a non-refused
// approve_once verdict meeting the Decision manifest's probability threshold
// advances execution. Every other outcome (defer_to_user, below threshold,
// oversize input, missing model, error, timeout, cancellation) falls back
// cleanly to the existing human permission flow with zero false auto-denials.
//
// Model approval applies exactly once to the exact pending request. The
// reviewer never writes permissions.json, session Allow rules, or
// sandbox.json, and never evaluates in non-auto modes.
//
// # Rubric & calibration resolution
//
// The rubric, question spec, and approval probability threshold come from the
// WARP Decision resource "permission-reviewer", resolved as a single source of
// truth through the standard 4-tier hierarchy (local > workspace > user >
// system) so teams can version-control spec.models, threshold calibration, and
// repo-specific rubric rules in .agents/decisions/permission-reviewer.decision.md.
// Repository trust fingerprinting of .agents/decisions/** (repo Decisions never
// evaluate in untrusted or restricted workspaces), kernel write-denial of
// .agents/, and strict Go validation of the verdict option set keep overrides
// confined.
//
// # Injection defense
//
// The outbound payload is an allowlisted projection of the permission card
// (including active sandbox enforcement posture and a bounded, non-secret
// preview when bash invokes a workspace-confined script) plus the initiating
// human prompt text only. Secrets, .env files, environment variables, prior
// tool outputs, and assistant messages are strictly excluded, and the rendered
// rubric frames card and user_intent as untrusted data.
package permreview
