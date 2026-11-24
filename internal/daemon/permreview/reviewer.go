package permreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// Verdict is the categorical outcome of one permission review.
type Verdict string

const (
	// VerdictApproveOnce authorizes exactly one execution of the reviewed
	// request; it never grants standing permission.
	VerdictApproveOnce Verdict = "approve_once"
	// VerdictDefer hands the request to the existing human permission flow.
	VerdictDefer Verdict = "defer_to_user"
)

// Review reason codes. Every value other than ReasonApproved defers to the
// human flow; the codes exist for bounded telemetry and receipts.
const (
	ReasonApproved        = "approved"
	ReasonDeferred        = "deferred"
	ReasonBelowThreshold  = "below_threshold"
	ReasonNoModel         = "no_model"
	ReasonOversize        = "oversize"
	ReasonInvalidManifest = "invalid_manifest"
	ReasonTimeout         = "timeout"
	ReasonCanceled        = "canceled"
	ReasonError           = "error"
)

// DecisionReviewSource is the receipt source stamp for decision-model reviews.
const DecisionReviewSource = "decision_model"

const (
	// verdictQuestionName is the fixed name of the reviewer's choice question.
	verdictQuestionName = "verdict"
	// defaultModelRefs mirrors the built-in manifest's spec.models preference
	// order: a dedicated reviewer alias, the workspace decision-role default,
	// then the fast completion tier (adapted through the gateway).
	defaultModelRefs = "@permission-reviewer,@default,@fast"
	// maxUserIntentBytes bounds the outbound user prompt text; conversational
	// preamble beyond the budget is truncated with an explicit marker.
	maxUserIntentBytes = 4096
	// maxCardEvidenceBytes bounds summary+diff+target. Oversize cards defer
	// locally with zero model calls — approving on a truncated diff is not an
	// option, so the evidence is never cut to fit.
	maxCardEvidenceBytes = 16384
	// rubricDigestLen is the hex length of the rubric fingerprint.
	rubricDigestLen = 12
)

// Daemon floors on P(approve_once). A resolved Decision may tighten the
// threshold (e.g. 0.98) but can never lower these values, and manifests
// cannot mint new verdict powers.
const (
	// FloorNative applies to native decision.Provider models (typesafe,
	// openai, ollama).
	FloorNative = 0.90
	// FloorLLMAdapter applies to completion models adapted via
	// decision.NewFromLLM, whose structured-output calibration is weaker.
	FloorLLMAdapter = 0.95
)

// ReviewRequest carries the pending permission card and the plain text of the
// user message that initiated the active turn.
type ReviewRequest struct {
	Request api.PermissionRequest
	// UserIntent is trimmed plain text from the turn's initiating user
	// message. System reminders, !cmd outputs, assistant messages, and tool
	// results are excluded by the caller.
	UserIntent string
}

// Result is the outcome of one review.
type Result struct {
	Verdict Verdict
	// Receipt is non-nil iff Verdict == VerdictApproveOnce.
	Receipt *api.PermissionReview
	// ReasonCode is one of the Reason* constants.
	ReasonCode string
	// WarnOnce is populated when no eligible model is configured, the
	// manifest is invalid, or the reviewer errored. The Service deduplicates
	// warnings per session so it should be surfaced at most once per code.
	WarnOnce string
}

// Reviewer evaluates one-shot permission cards in auto mode. Implementations
// must never block longer than the caller's context allows and must fail
// closed to the human flow.
type Reviewer interface {
	Review(ctx context.Context, req ReviewRequest) Result
}

// Deps wires the reviewer to the live session dependencies it queries per
// review, so /workspace reload, /model, .agents/decisions edits, and alias
// changes apply immediately.
type Deps struct {
	// Permissions supplies the live permission mode gate.
	Permissions permissions.SessionManager
	Workspace   *workspace.Workspace
	// Catalog resolves models through workspace role/alias rules.
	Catalog func() *models.Catalog
	Gateway func() gateway.Gateway
	// Metrics receives permission_review outcome records. Nil disables them.
	Metrics metrics.Recorder
	Logger  log.Interface
}

// Service is the daemon-owned [Reviewer]. It is safe for concurrent use.
type Service struct {
	deps Deps

	warnMu sync.Mutex
	warned map[string]bool
}

var _ Reviewer = (*Service)(nil)

// New constructs a [Service]. A nil Logger falls back to the package default
// (silent) logger.
func New(deps Deps) *Service {
	if deps.Logger == nil {
		deps.Logger = log.Default()
	}
	return &Service{deps: deps, warned: make(map[string]bool)}
}

// reviewPayload is the complete outbound inference payload: an allowlisted
// projection of the card plus the user's initiating prompt text. Repository
// files, environment variables, prior tool outputs, and assistant messages
// are excluded by construction.
type reviewPayload struct {
	Card       cardView `json:"card"`
	UserIntent string   `json:"user_intent"`
}

type cardView struct {
	ToolName      string `json:"tool_name"`
	Action        string `json:"action"`
	Safety        string `json:"safety"`
	Target        string `json:"target"`
	Summary       string `json:"summary"`
	Diff          string `json:"diff"`
	Unsandboxed   bool   `json:"unsandboxed"`
	BypassSandbox bool   `json:"bypass_sandbox"`
}

// Review evaluates req once. It never denies: every non-approval path returns
// [VerdictDefer] so the caller falls through to the human permission flow.
func (s *Service) Review(ctx context.Context, req ReviewRequest) Result {
	start := time.Now()
	deferred := func(reason, warn string) Result {
		res := Result{Verdict: VerdictDefer, ReasonCode: reason}
		if warn != "" {
			res.WarnOnce = s.warnOnce(warn)
		}
		s.record(ctx, req.Request, res, time.Since(start))
		return res
	}

	if s.deps.Permissions.Mode() != api.ModeAuto {
		return deferred(ReasonDeferred, "")
	}

	dec, scopeName, warn := s.resolveDecision()
	if dec == nil {
		return deferred(ReasonInvalidManifest, warn)
	}
	question, threshold, ok := validateVerdictQuestion(dec)
	if !ok {
		return deferred(ReasonInvalidManifest,
			fmt.Sprintf("permission reviewer Decision %q is malformed: verdict question must offer exactly approve_once and defer_to_user; falling back to human prompts", workspace.PermissionReviewerDecisionName))
	}

	payload, oversize := buildPayload(req)
	if oversize {
		return deferred(ReasonOversize, "")
	}

	rendered, err := renderRubric(s.deps.Workspace, dec)
	if err != nil {
		return deferred(ReasonError, fmt.Sprintf("permission reviewer rubric failed to render: %v", err))
	}

	model, adapted, modelID, modelWarn := s.resolveModel(ctx, dec)
	if model == nil {
		return deferred(ReasonNoModel, modelWarn)
	}

	floor := FloorNative
	if adapted {
		floor = FloorLLMAdapter
	}
	effective := math.Max(threshold, floor)

	instructions := strings.TrimSpace(rendered)
	if qi := strings.TrimSpace(question.Instructions); qi != "" {
		instructions += "\n\n" + qi
	}
	digest := rubricDigest(instructions, question)
	q := decision.Choice(verdictQuestionName, instructions,
		decision.Opt(string(VerdictApproveOnce), optionDescription(question, string(VerdictApproveOnce))),
		decision.Opt(string(VerdictDefer), optionDescription(question, string(VerdictDefer))),
	)

	reviewCtx := scope.With(ctx, scope.Scope{Purpose: string(metrics.PurposePermissionReview)})
	resp, err := model.Evaluate(reviewCtx, decision.State(payload), q)

	latency := time.Since(start)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return deferred(ReasonCanceled, "")
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(reviewCtx.Err(), context.DeadlineExceeded):
		return deferred(ReasonTimeout, "")
	case err != nil:
		return deferred(ReasonError, fmt.Sprintf("permission reviewer model call failed: %v", err))
	}
	ans, found := resp.Answer(verdictQuestionName)
	if !found || ans.Refused {
		return deferred(ReasonDeferred, "")
	}

	probApprove := ans.Prob(string(VerdictApproveOnce))
	approved := ans.Choice == string(VerdictApproveOnce) &&
		!math.IsNaN(probApprove) && !math.IsInf(probApprove, 0) && probApprove >= effective &&
		(question.MinConfidence == nil || ans.Confidence >= *question.MinConfidence)
	if !approved {
		reason := ReasonDeferred
		if ans.Choice == string(VerdictApproveOnce) || probApprove >= threshold {
			reason = ReasonBelowThreshold
		}
		res := Result{Verdict: VerdictDefer, ReasonCode: reason}
		s.recordWith(ctx, req.Request, res, modelID, scopeName, digest, latency)
		return res
	}

	receipt := &api.PermissionReview{
		RequestID:     req.Request.ID,
		Source:        DecisionReviewSource,
		Verdict:       string(VerdictApproveOnce),
		Model:         modelID,
		DecisionScope: scopeName,
		RubricDigest:  digest,
		Probability:   probApprove,
		Confidence:    ans.Confidence,
		Threshold:     effective,
		LatencyMs:     latency.Milliseconds(),
	}
	res := Result{Verdict: VerdictApproveOnce, Receipt: receipt, ReasonCode: ReasonApproved}
	s.recordWith(ctx, req.Request, res, modelID, scopeName, digest, latency)
	return res
}

// resolveDecision resolves the permission-reviewer Decision strictly through
// the workspace catalog across the 4-tier WARP hierarchy. Restricted
// workspaces already have repo-scoped Decisions stripped at load time, so an
// untrusted repo can never run a custom rubric here.
func (s *Service) resolveDecision() (*warp.Decision, string, string) {
	snap, ok := s.deps.Workspace.Catalog().GetDecision(workspace.PermissionReviewerDecisionName)
	if !ok || snap.Def == nil {
		return nil, "", fmt.Sprintf("permission reviewer Decision %q is unavailable in workspace catalog", workspace.PermissionReviewerDecisionName)
	}
	scopeName := snap.Scope
	if scopeName == "" {
		scopeName = string(workspace.NamespaceSystem)
	}
	return snap.Def, scopeName, ""
}

// validateVerdictQuestion enforces the immutable verdict contract: exactly one
// choice question named "verdict" exposing exactly approve_once and
// defer_to_user. Malformed manifests defer with a validation warning.
func validateVerdictQuestion(dec *warp.Decision) (warp.DecisionQuestion, float64, bool) {
	var found *warp.DecisionQuestion
	for i := range dec.Spec.Questions {
		if dec.Spec.Questions[i].Name == verdictQuestionName {
			found = &dec.Spec.Questions[i]
			break
		}
	}
	if found == nil || len(dec.Spec.Questions) != 1 || found.Type != warp.DecisionQuestionChoice {
		return warp.DecisionQuestion{}, 0, false
	}
	seen := map[string]bool{}
	for _, opt := range found.Options {
		seen[opt.Value] = true
	}
	if len(found.Options) != 2 || !seen[string(VerdictApproveOnce)] || !seen[string(VerdictDefer)] {
		return warp.DecisionQuestion{}, 0, false
	}
	threshold := 0.0
	if found.Threshold != nil {
		threshold = *found.Threshold
	}
	return *found, threshold, true
}

func optionDescription(q warp.DecisionQuestion, value string) string {
	for _, opt := range q.Options {
		if opt.Value == value {
			return opt.Description
		}
	}
	return ""
}

func renderRubric(ws *workspace.Workspace, dec *warp.Decision) (string, error) {
	cat := ws.Catalog()
	return dec.Render(&warp.DecisionRenderOptions{
		Workspace: cat.WorkspaceSpec(),
		Project:   cat.Project(),
	})
}

func rubricDigest(instructions string, q warp.DecisionQuestion) string {
	spec, err := json.Marshal(q)
	if err != nil {
		spec = []byte(q.Name)
	}
	sum := sha256.Sum256(append([]byte(instructions), spec...))
	return hex.EncodeToString(sum[:])[:rubricDigestLen]
}

// buildPayload projects the card onto the allowlisted outbound fields and
// enforces the input byte budgets. Oversize card evidence is never truncated
// to fit: it defers locally with zero model calls.
func buildPayload(req ReviewRequest) (reviewPayload, bool) {
	card := cardView{
		ToolName:      req.Request.ToolName,
		Action:        string(req.Request.Action),
		Safety:        string(req.Request.Safety),
		Target:        req.Request.Target,
		Summary:       req.Request.Summary,
		Diff:          req.Request.Diff,
		Unsandboxed:   req.Request.Unsandboxed,
		BypassSandbox: req.Request.BypassSandbox,
	}
	if len(card.Summary)+len(card.Diff)+len(card.Target) > maxCardEvidenceBytes {
		return reviewPayload{}, true
	}
	intent, _ := strutils.TruncateLine(strings.TrimSpace(req.UserIntent), maxUserIntentBytes, "\n…[user prompt truncated]")
	return reviewPayload{Card: card, UserIntent: intent}, false
}

// resolveModel walks the resolved Decision's spec.models preferences in
// order. Non-@default references try RoleDecision first, then RoleCompletion
// (adapted via the gateway). @default resolves strictly against RoleDecision
// so it never accidentally grabs a slow chat model before @fast is tried.
// Once card data would be sent to a model, the service never fails over to a
// second model: resolution failures happen before any inference call, so
// trying the next preference stays safe.
func (s *Service) resolveModel(ctx context.Context, dec *warp.Decision) (*decision.Model, bool, string, string) {
	cat := s.deps.Catalog()
	gw := s.deps.Gateway()
	if cat == nil || gw == nil {
		return nil, false, "", "permission reviewer has no model catalog or gateway configured"
	}
	refs := dec.Spec.Models
	if len(refs) == 0 {
		refs = strings.Split(defaultModelRefs, ",")
	}
	var lastErr string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		var (
			m     models.Model
			found bool
		)
		if ref == "@default" {
			m, found = cat.ResolveContext(ctx, models.RoleDecision)
			if !found {
				continue
			}
		} else if dm, ok := cat.ResolveContext(ctx, models.RoleDecision, ref); ok {
			m = dm
		} else if dm, ok := cat.ResolveContext(ctx, models.RoleCompletion, ref); ok {
			m = dm
		} else {
			lastErr = fmt.Sprintf("model %q does not resolve", ref)
			continue
		}
		dm, err := gw.Decision(ctx, m, llm.ModelConfig{})
		if err != nil {
			lastErr = fmt.Sprintf("model %q failed to initialize: %v", m.ID, err)
			continue
		}
		_, adapted := dm.Provider().(*decision.LLMAdapter)
		return dm, adapted, string(m.ID), ""
	}
	warn := "no eligible model configured for permission review; risky auto-mode requests will keep prompting"
	if lastErr != "" {
		warn = fmt.Sprintf("permission reviewer could not initialize a model (%s); risky auto-mode requests will keep prompting", lastErr)
	}
	return nil, false, "", warn
}

// requestClass buckets a card into the bounded telemetry class.
func requestClass(req api.PermissionRequest) string {
	switch {
	case req.BypassSandbox:
		return "bypass_sandbox"
	case req.Action == api.ActionNet || req.ToolName == "net":
		return "net"
	case req.Safety == api.SafetyUnsafe:
		return "unsafe"
	case req.Safety == api.SafetyOutside:
		return "outside"
	case req.Action == api.ActionDelete:
		return "delete"
	default:
		return "other"
	}
}

func (s *Service) record(ctx context.Context, req api.PermissionRequest, res Result, dur time.Duration) {
	s.recordWith(ctx, req, res, "", "", "", dur)
}

func (s *Service) recordWith(ctx context.Context, req api.PermissionRequest, res Result, modelID, decisionScope, digest string, dur time.Duration) {
	err := metrics.RecordPermissionReview(ctx, s.deps.Metrics, metrics.PermissionReview{
		Outcome:      outcomeCode(res),
		RequestClass: requestClass(req),
		Model:        modelID,
		Decision:     decisionScope,
		RubricDigest: digest,
		Duration:     dur,
	})
	if err != nil {
		s.deps.Logger.Warn("failed to record permission review metrics", log.String("error", err.Error()))
	}
}

func outcomeCode(res Result) string {
	if res.Verdict == VerdictApproveOnce {
		return metrics.ReviewOutcomeApproved
	}
	switch res.ReasonCode {
	case ReasonDeferred:
		return metrics.ReviewOutcomeDeferred
	case ReasonBelowThreshold:
		return metrics.ReviewOutcomeBelowThreshold
	case ReasonNoModel:
		return metrics.ReviewOutcomeNoModel
	case ReasonOversize:
		return metrics.ReviewOutcomeOversize
	case ReasonInvalidManifest:
		return metrics.ReviewOutcomeInvalidManifest
	case ReasonTimeout:
		return metrics.ReviewOutcomeTimeout
	case ReasonCanceled:
		return metrics.ReviewOutcomeCanceled
	default:
		return metrics.ReviewOutcomeError
	}
}

// warnOnce returns msg the first time this service emits it and "" for every
// repeat, so per-session notices do not spam the transcript.
func (s *Service) warnOnce(msg string) string {
	key := sha256.Sum256([]byte(msg))
	code := hex.EncodeToString(key[:])
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	if s.warned[code] {
		return ""
	}
	s.warned[code] = true
	return msg
}
