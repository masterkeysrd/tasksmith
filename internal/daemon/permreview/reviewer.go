package permreview

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
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
// human flow; they alias [metrics.ReviewOutcome*] directly for telemetry.
const (
	ReasonApproved        = metrics.ReviewOutcomeApproved
	ReasonDeferred        = metrics.ReviewOutcomeDeferred
	ReasonBelowThreshold  = metrics.ReviewOutcomeBelowThreshold
	ReasonNoModel         = metrics.ReviewOutcomeNoModel
	ReasonOversize        = metrics.ReviewOutcomeOversize
	ReasonInvalidManifest = metrics.ReviewOutcomeInvalidManifest
	ReasonTimeout         = metrics.ReviewOutcomeTimeout
	ReasonCanceled        = metrics.ReviewOutcomeCanceled
	ReasonError           = metrics.ReviewOutcomeError
)

// DecisionReviewSource is the receipt source stamp for decision-model reviews.
const DecisionReviewSource = "decision_model"

const (
	actionQuestionName    = "action"
	safetyQuestionName    = "safety"
	requestedQuestionName = "requested"

	// systemDecisionQualifiedName identifies the built-in system Decision used
	// when a higher-tier override omits an explicit threshold.
	systemDecisionQualifiedName = "system/Decision/" + workspace.PermissionReviewerDecisionName
	// maxUserIntentBytes bounds the outbound user prompt text; conversational
	// preamble beyond the budget is truncated with an explicit marker.
	maxUserIntentBytes = 1200
	// maxCardEvidenceBytes bounds summary+diff+target+script_preview. Oversize
	// cards defer locally with zero model calls — approving on truncated
	// evidence is not an option, so evidence is never cut to fit.
	maxCardEvidenceBytes = 16384
	// rubricDigestLen is the hex length of the rubric fingerprint.
	rubricDigestLen = 12
)

type requiredQuestionSpec struct {
	name    string
	options []string
}

var (
	// defaultModelRefs mirrors the built-in manifest's spec.models preference
	// order: a dedicated reviewer alias, the workspace decision-role default,
	// then the fast completion tier (adapted through the gateway).
	defaultModelRefs      = []string{"@permission-reviewer", "@default", "@fast"}
	actionOptionValues    = []string{"read", "write", "delete", "exec", "net"}
	safetyOptionValues    = []string{"safe", "outside", "unsafe"}
	requestedOptionValues = []string{"yes", "no"}
	requiredQuestions     = []requiredQuestionSpec{
		{name: actionQuestionName, options: actionOptionValues},
		{name: safetyQuestionName, options: safetyOptionValues},
		{name: requestedQuestionName, options: requestedOptionValues},
	}
)

// ReviewRequest carries the pending permission card, pre-computed path/script
// posture from tools.Plan, and the plain text of the user's recent prompts.
type ReviewRequest struct {
	Request api.PermissionRequest
	// InsideWorkspace reports whether a file-targeted plan resolves inside the
	// workspace root (nil when not a single file path).
	InsideWorkspace *bool
	// ProtectedPath reports whether the plan targets or references a protected
	// control-plane, secret, XDG policy, or sensitive host path.
	ProtectedPath bool
	// ScriptPaths holds local script paths referenced by a shell plan for
	// bounded workspace preview inspection.
	ScriptPaths []string
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
	// Permissions supplies the live permission mode gate and runtime sandbox posture.
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
// projection of the card plus the user's initiating prompt text. Secrets,
// environment variables, prior tool outputs, and assistant messages are
// excluded by construction.
type reviewPayload struct {
	Card       cardView `json:"card"`
	UserIntent string   `json:"user_intent"`
}

type cardView struct {
	ToolName        string `json:"tool_name"`
	Target          string `json:"target"`
	Summary         string `json:"summary,omitempty"`
	Diff            string `json:"diff,omitempty"`
	ScriptPreview   string `json:"script_preview,omitempty"`
	InsideWorkspace *bool  `json:"inside_workspace,omitempty"`
	ProtectedPath   bool   `json:"protected_path,omitempty"`
	SandboxEnforced bool   `json:"sandbox_enforced"`
	Unsandboxed     bool   `json:"unsandboxed"`
	BypassSandbox   bool   `json:"bypass_sandbox"`
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
		s.recordWith(ctx, req.Request, res, "", "", "", time.Since(start))
		return res
	}

	if s.deps.Permissions.Mode() != api.ModeAuto {
		return deferred(ReasonDeferred, "")
	}

	dec, scopeName, warn := s.resolveDecision()
	if dec == nil {
		return deferred(ReasonInvalidManifest, warn)
	}

	spec, ok := validateClassifierQuestions(dec, s.systemDefaultThresholds())
	if !ok {
		msg := fmt.Sprintf(
			"permission reviewer Decision %q is malformed: must define choice questions "+
				"action, safety, and requested with thresholds in (0, 1]; falling back to human prompts",
			workspace.PermissionReviewerDecisionName,
		)
		return deferred(ReasonInvalidManifest, msg)
	}

	payload, oversize := s.buildPayload(req)
	if oversize {
		return deferred(ReasonOversize, "")
	}

	cat := s.deps.Workspace.Catalog()
	rendered, err := dec.Render(&warp.DecisionRenderOptions{
		Workspace: cat.WorkspaceSpec(),
		Project:   cat.Project(),
	})
	if err != nil {
		return deferred(ReasonError, fmt.Sprintf("permission reviewer rubric failed to render: %v", err))
	}

	model, modelID, modelWarn := s.resolveModel(ctx, dec)
	if model == nil {
		return deferred(ReasonNoModel, modelWarn)
	}

	rubric := strings.TrimSpace(rendered)
	digest := rubricDigest(rubric, spec)

	questions, err := gateway.DecisionQuestions(dec, rubric, safetyQuestionName)
	if err != nil {
		return deferred(ReasonInvalidManifest, fmt.Sprintf("permission reviewer decision questions invalid: %v", err))
	}

	reviewCtx := scope.With(ctx, scope.Scope{Purpose: string(metrics.PurposePermissionReview)})
	resp, err := model.Evaluate(reviewCtx, decision.State(payload), questions...)

	latency := time.Since(start)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return deferred(ReasonCanceled, "")
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(reviewCtx.Err(), context.DeadlineExceeded):
		return deferred(ReasonTimeout, "")
	case err != nil:
		return deferred(ReasonError, fmt.Sprintf("permission reviewer model call failed: %v", err))
	}

	decisionOut := evaluateClassification(spec, payload.Card, resp)
	if !decisionOut.approved {
		res := Result{
			Verdict:    VerdictDefer,
			ReasonCode: decisionOut.reason,
		}
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
		Probability:   decisionOut.probability,
		Confidence:    decisionOut.confidence,
		Threshold:     decisionOut.threshold,
		LatencyMs:     latency.Milliseconds(),
	}
	res := Result{
		Verdict:    VerdictApproveOnce,
		Receipt:    receipt,
		ReasonCode: ReasonApproved,
	}
	s.recordWith(ctx, req.Request, res, modelID, scopeName, digest, latency)
	return res
}

type validatedQuestion struct {
	question  warp.DecisionQuestion
	threshold float64
}

type classifierSpec struct {
	action    validatedQuestion
	safety    validatedQuestion
	requested validatedQuestion
}

type classificationOutcome struct {
	approved    bool
	reason      string
	probability float64
	confidence  float64
	threshold   float64
}

func evaluateClassification(
	spec classifierSpec,
	card cardView,
	resp *decision.Response,
) classificationOutcome {
	actionAns, hasAction := resp.Answer(actionQuestionName)
	safetyAns, hasSafety := resp.Answer(safetyQuestionName)
	if !hasAction || !hasSafety || actionAns.Refused || safetyAns.Refused {
		return classificationOutcome{reason: ReasonDeferred}
	}
	if !slices.Contains(actionOptionValues, actionAns.Choice) ||
		!slices.Contains(safetyOptionValues, safetyAns.Choice) ||
		safetyAns.Choice == string(api.SafetyUnsafe) {
		return classificationOutcome{reason: ReasonDeferred}
	}

	probAction := actionAns.Prob(actionAns.Choice)
	probSafety := safetyAns.Prob(safetyAns.Choice)
	if !passesQuestion(actionAns, probAction, spec.action) ||
		!passesQuestion(safetyAns, probSafety, spec.safety) {
		return classificationOutcome{reason: ReasonBelowThreshold}
	}

	approve := func(extraAns *decision.Answer, extraProb float64, extraQ *validatedQuestion) classificationOutcome {
		prob := min(probAction, probSafety)
		conf := min(actionAns.Confidence, safetyAns.Confidence)
		th := max(spec.action.threshold, spec.safety.threshold)
		if extraAns != nil && extraQ != nil {
			prob = min(prob, extraProb)
			conf = min(conf, extraAns.Confidence)
			th = max(th, extraQ.threshold)
		}
		return classificationOutcome{
			approved:    true,
			reason:      ReasonApproved,
			probability: prob,
			confidence:  conf,
			threshold:   th,
		}
	}

	switch safetyAns.Choice {
	case string(api.SafetySafe):
		if card.ProtectedPath {
			return classificationOutcome{reason: ReasonDeferred}
		}
		if actionAns.Choice != string(api.ActionRead) &&
			card.InsideWorkspace != nil && !*card.InsideWorkspace {
			return classificationOutcome{reason: ReasonDeferred}
		}
		if actionAns.Choice == string(api.ActionDelete) &&
			!card.SandboxEnforced &&
			(card.InsideWorkspace == nil || !*card.InsideWorkspace) {
			return classificationOutcome{reason: ReasonDeferred}
		}
		return approve(nil, 0, nil)

	case string(api.SafetyOutside):
		if actionAns.Choice == string(api.ActionRead) {
			if card.ProtectedPath {
				return classificationOutcome{reason: ReasonDeferred}
			}
			return approve(nil, 0, nil)
		}

		reqAns, hasReq := resp.Answer(requestedQuestionName)
		if !hasReq || reqAns.Refused || reqAns.Choice != "yes" {
			return classificationOutcome{reason: ReasonDeferred}
		}
		probReq := reqAns.Prob("yes")
		if !passesQuestion(reqAns, probReq, spec.requested) {
			return classificationOutcome{reason: ReasonBelowThreshold}
		}
		return approve(&reqAns, probReq, &spec.requested)

	default:
		return classificationOutcome{reason: ReasonDeferred}
	}
}

func passesQuestion(ans decision.Answer, prob float64, vq validatedQuestion) bool {
	if math.IsNaN(prob) || math.IsInf(prob, 0) || prob < vq.threshold {
		return false
	}
	if vq.question.MinConfidence != nil {
		if math.IsNaN(ans.Confidence) || math.IsInf(ans.Confidence, 0) ||
			ans.Confidence < *vq.question.MinConfidence {
			return false
		}
	}
	return true
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

// systemDefaultThresholds reads the thresholds declared on the built-in system
// permission-reviewer Decision so higher-tier overrides that omit a threshold
// inherit from permission-reviewer.decision.md as the single source of truth.
func (s *Service) systemDefaultThresholds() map[string]float64 {
	snap, ok := s.deps.Workspace.Catalog().GetDecision(systemDecisionQualifiedName)
	if !ok || snap.Def == nil {
		return nil
	}
	defaults := make(map[string]float64, len(snap.Def.Spec.Questions))
	for i := range snap.Def.Spec.Questions {
		q := &snap.Def.Spec.Questions[i]
		if q.Threshold != nil {
			defaults[q.Name] = *q.Threshold
		}
	}
	return defaults
}

// validateClassifierQuestions enforces the 3-question static-evaluator
// emulation contract: action, safety, and requested choice questions with
// exact option sets and valid probability thresholds in (0, 1].
func validateClassifierQuestions(
	dec *warp.Decision,
	fallbackThresholds map[string]float64,
) (classifierSpec, bool) {
	if len(dec.Spec.Questions) != len(requiredQuestions) {
		return classifierSpec{}, false
	}

	byName := make(map[string]warp.DecisionQuestion, len(dec.Spec.Questions))
	for _, q := range dec.Spec.Questions {
		byName[q.Name] = q
	}

	validated := make(map[string]validatedQuestion, len(requiredQuestions))
	for _, reqQ := range requiredQuestions {
		vq, ok := validateSingleChoice(byName, reqQ.name, reqQ.options, fallbackThresholds)
		if !ok {
			return classifierSpec{}, false
		}
		validated[reqQ.name] = vq
	}

	return classifierSpec{
		action:    validated[actionQuestionName],
		safety:    validated[safetyQuestionName],
		requested: validated[requestedQuestionName],
	}, true
}

func validateSingleChoice(
	byName map[string]warp.DecisionQuestion,
	name string,
	expectedOptions []string,
	fallbackThresholds map[string]float64,
) (validatedQuestion, bool) {
	q, ok := byName[name]
	if !ok || q.Type != warp.DecisionQuestionChoice || len(q.Options) != len(expectedOptions) {
		return validatedQuestion{}, false
	}

	seen := make(map[string]bool, len(q.Options))
	for _, opt := range q.Options {
		seen[opt.Value] = true
	}
	for _, want := range expectedOptions {
		if !seen[want] {
			return validatedQuestion{}, false
		}
	}

	threshold := fallbackThresholds[name]
	if q.Threshold != nil {
		threshold = *q.Threshold
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 || threshold > 1 {
		return validatedQuestion{}, false
	}
	if q.MinConfidence != nil {
		mc := *q.MinConfidence
		if math.IsNaN(mc) || math.IsInf(mc, 0) || mc < 0 || mc > 1 {
			return validatedQuestion{}, false
		}
	}

	return validatedQuestion{
		question:  q,
		threshold: threshold,
	}, true
}

func rubricDigest(instructions string, spec classifierSpec) string {
	ordered := []validatedQuestion{spec.action, spec.safety, spec.requested}
	normalized := make([]warp.DecisionQuestion, len(ordered))
	for i, vq := range ordered {
		q := vq.question
		th := vq.threshold
		q.Threshold = &th
		normalized[i] = q
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		raw = []byte(actionQuestionName + safetyQuestionName + requestedQuestionName)
	}
	sum := sha256.Sum256(append([]byte(instructions), raw...))
	return hex.EncodeToString(sum[:])[:rubricDigestLen]
}

// buildPayload projects the card onto factual allowlisted fields (omitting
// static action/safety pre-labels to avoid anchoring bias), attaches sandbox
// posture, path confinement/protection flags, and any bounded workspace script
// preview, and enforces input byte budgets.
func (s *Service) buildPayload(req ReviewRequest) (reviewPayload, bool) {
	sandboxEnforced := false
	if rt := s.deps.Permissions.RuntimePolicy(); rt != nil && rt.SandboxNative &&
		!req.Request.Unsandboxed && !req.Request.BypassSandbox &&
		(req.Request.Action != api.ActionNet || req.Request.ToolName != "net") {
		sandboxEnforced = true
	}

	scriptPreview, scriptOversize := extractWorkspaceScriptPreview(s.deps.Workspace, req.ScriptPaths)
	if scriptOversize {
		return reviewPayload{}, true
	}

	card := cardView{
		ToolName:        req.Request.ToolName,
		Target:          req.Request.Target,
		Summary:         req.Request.Summary,
		Diff:            req.Request.Diff,
		ScriptPreview:   scriptPreview,
		InsideWorkspace: req.InsideWorkspace,
		ProtectedPath:   req.ProtectedPath,
		SandboxEnforced: sandboxEnforced,
		Unsandboxed:     req.Request.Unsandboxed,
		BypassSandbox:   req.Request.BypassSandbox,
	}
	if len(card.Summary)+len(card.Diff)+len(card.Target)+len(card.ScriptPreview) > maxCardEvidenceBytes {
		return reviewPayload{}, true
	}

	intent, _ := strutils.TruncateLine(
		strings.TrimSpace(req.UserIntent),
		maxUserIntentBytes,
		"\n…[user prompt truncated]",
	)
	return reviewPayload{
		Card:       card,
		UserIntent: intent,
	}, false
}

// extractWorkspaceScriptPreview reads bounded UTF-8 text of workspace-confined
// script paths so the reviewer can judge local scripts from actual content
// rather than opaque filenames. Protected/secret files, symlink escapes, and
// binary files are excluded; scripts exceeding maxCardEvidenceBytes return
// oversize=true.
func extractWorkspaceScriptPreview(ws *workspace.Workspace, scriptPaths []string) (string, bool) {
	if ws == nil || len(scriptPaths) == 0 {
		return "", false
	}
	wsRoot := strings.TrimSpace(ws.RootDir())
	if wsRoot == "" {
		return "", false
	}

	wsClean := filepath.Clean(wsRoot)
	var sections []string
	totalBytes := 0
	for _, cand := range scriptPaths {
		resolved := fsutils.ExpandAndCleanPath(cand, wsClean, "")
		if !fsutils.IsConfinedInsideDir(resolved, wsClean) {
			continue
		}
		if guard.IsProtectedTarget(resolved, wsClean, "") {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > maxCardEvidenceBytes {
			return "", true
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			continue
		}
		rel, err := filepath.Rel(wsClean, resolved)
		if err != nil {
			rel = filepath.Base(resolved)
		}
		section := "# " + filepath.ToSlash(rel) + "\n" + string(data)
		totalBytes += len(section)
		if totalBytes > maxCardEvidenceBytes {
			return "", true
		}
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n"), false
}

// resolveModel picks the first initialized decision model from the resolved
// Decision's spec.models preferences (delegating role/alias resolution to
// models.Catalog). Resolution happens before inference, so trying the next
// candidate never duplicates outbound card data.
func (s *Service) resolveModel(ctx context.Context, dec *warp.Decision) (*decision.Model, string, string) {
	cat := s.deps.Catalog()
	gw := s.deps.Gateway()
	if cat == nil || gw == nil {
		return nil, "", "permission reviewer has no model catalog or gateway configured"
	}

	refs := dec.Spec.Models
	if len(refs) == 0 {
		refs = defaultModelRefs
	}

	var lastErr string
	for _, m := range cat.ResolveDecisionCandidates(ctx, refs...) {
		dm, err := gw.Decision(ctx, m, llm.ModelConfig{})
		if err != nil {
			lastErr = fmt.Sprintf("model %q failed to initialize: %v", m.ID, err)
			continue
		}
		return dm, string(m.ID), ""
	}

	warn := "no eligible model configured for permission review; risky auto-mode requests will keep prompting"
	if lastErr != "" {
		warn = fmt.Sprintf(
			"permission reviewer could not initialize a model (%s); risky auto-mode requests will keep prompting",
			lastErr,
		)
	}
	return nil, "", warn
}

// requestClass buckets a card into the bounded telemetry class.
func requestClass(req api.PermissionRequest) string {
	switch {
	case req.BypassSandbox:
		return "bypass_sandbox"
	case req.Action == api.ActionNet, req.Action == api.ActionDelete:
		return string(req.Action)
	case req.Safety == api.SafetyUnsafe, req.Safety == api.SafetyOutside:
		return string(req.Safety)
	default:
		return "other"
	}
}

func (s *Service) recordWith(
	ctx context.Context,
	req api.PermissionRequest,
	res Result,
	modelID, decisionScope, digest string,
	dur time.Duration,
) {
	outcome := cmp.Or(res.ReasonCode, metrics.ReviewOutcomeError)
	if res.Verdict == VerdictApproveOnce {
		outcome = metrics.ReviewOutcomeApproved
	}
	err := metrics.RecordPermissionReview(ctx, s.deps.Metrics, metrics.PermissionReview{
		Outcome:      outcome,
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
