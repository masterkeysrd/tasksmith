package permreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// fakeDecisionProvider is a scripted decision.Provider capturing every
// outbound request so tests can assert the exact wire payload.
type fakeDecisionProvider struct {
	mu       sync.Mutex
	calls    []*decision.Request
	resp     *decision.Response
	err      error
	errAfter int  // fail on call index errAfter (0 = never)
	waitCtx  bool // block until the caller's context ends, then return its error
}

func (p *fakeDecisionProvider) Name() string { return "fakedec" }

func (p *fakeDecisionProvider) Evaluate(ctx context.Context, req *decision.Request) (*decision.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.calls = append(p.calls, req)
	wait := p.waitCtx
	p.mu.Unlock()
	if wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.errAfter > 0 && len(p.calls) == p.errAfter {
		return nil, p.err
	}
	if p.err != nil && p.errAfter == 0 {
		return nil, p.err
	}
	if p.resp != nil {
		return p.resp, nil
	}
	return cannedResponse(0.97, false), nil
}

func (p *fakeDecisionProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func cannedResponse(probApprove float64, refused bool) *decision.Response {
	return cannedClassification("exec", probApprove, "safe", probApprove, "yes", probApprove, refused)
}

func cannedClassification(
	action string,
	probAction float64,
	safety string,
	probSafety float64,
	requested string,
	probRequested float64,
	refused bool,
) *decision.Response {
	otherAction := "read"
	if action == otherAction {
		otherAction = "exec"
	}
	otherSafety := "unsafe"
	if safety == otherSafety {
		otherSafety = "safe"
	}
	otherReq := "no"
	if requested == otherReq {
		otherReq = "yes"
	}
	return &decision.Response{
		Model: "fakedec-model",
		Answers: map[string]decision.Answer{
			actionQuestionName: {
				Name:    actionQuestionName,
				Type:    decision.QuestionChoice,
				Refused: refused,
				Choice:  action,
				ChoiceProbabilities: []decision.OptionProbability{
					{Value: action, Probability: probAction},
					{Value: otherAction, Probability: 1 - probAction},
				},
				Confidence: 2*probAction - 1,
			},
			safetyQuestionName: {
				Name:    safetyQuestionName,
				Type:    decision.QuestionChoice,
				Refused: refused,
				Choice:  safety,
				ChoiceProbabilities: []decision.OptionProbability{
					{Value: safety, Probability: probSafety},
					{Value: otherSafety, Probability: 1 - probSafety},
				},
				Confidence: 2*probSafety - 1,
			},
			requestedQuestionName: {
				Name:    requestedQuestionName,
				Type:    decision.QuestionChoice,
				Refused: refused,
				Choice:  requested,
				ChoiceProbabilities: []decision.OptionProbability{
					{Value: requested, Probability: probRequested},
					{Value: otherReq, Probability: 1 - probRequested},
				},
				Confidence: 2*probRequested - 1,
			},
		},
	}
}

func testClassifierManifest(thresholdLine string) string {
	th := ""
	if thresholdLine != "" {
		th = "      " + thresholdLine
	}
	return fmt.Sprintf(`---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: permission-reviewer
spec:
  models: ["fake/decider"]
  questions:
    - name: action
      type: choice
%s      options:
        - value: read
          description: read
        - value: write
          description: write
        - value: delete
          description: delete
        - value: exec
          description: exec
        - value: net
          description: net
    - name: safety
      type: choice
%s      options:
        - value: safe
          description: safe
        - value: outside
          description: outside
        - value: unsafe
          description: unsafe
    - name: requested
      type: choice
%s      options:
        - value: "yes"
          description: yes
        - value: "no"
          description: no
---
rubric
`, th, th, th)
}

// fakeLLMProvider answers structured-output calls with a canned verdict JSON,
// driving the real decision.LLMAdapter path.
type fakeLLMProvider struct {
	raw   string
	err   error
	calls int
}

func (p *fakeLLMProvider) Name() string { return "fakellm" }

func (p *fakeLLMProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	chunks := []message.AssistantChunk{
		{Content: []message.Block{&message.TextBlock{Text: p.raw}}, Done: true},
	}
	return func(yield func(message.AssistantChunk, error) bool) {
		for _, ch := range chunks {
			if !yield(ch, nil) {
				return
			}
		}
	}, nil
}

func (p *fakeLLMProvider) ListProfiles() []profile.Profile { return nil }

func (p *fakeLLMProvider) GetProfile(id string) (profile.Profile, bool) {
	return profile.Profile{Name: id, Capabilities: profile.Capabilities{Completion: true, Decision: true}}, true
}

func (p *fakeLLMProvider) SearchProfiles(string) []profile.Profile { return nil }

func (p *fakeLLMProvider) OverrideProfile(string, profile.Profile) {}

// fakeGateway implements gateway.Gateway, serving decision models backed by
// the scripted providers.
type fakeGateway struct {
	native *fakeDecisionProvider
	llm    *fakeLLMProvider
}

func (g *fakeGateway) LLM(ctx context.Context, m models.Model, cfg llm.ModelConfig) (*llm.Model, error) {
	if g.llm == nil {
		return nil, errors.New("no llm configured")
	}
	return llm.NewModel(g.llm, string(m.ID), nil)
}

func (g *fakeGateway) Decision(ctx context.Context, m models.Model, cfg llm.ModelConfig) (*decision.Model, error) {
	if m.Capabilities.SupportsDecision() && g.native != nil {
		return decision.NewModel(g.native, string(m.ID), nil)
	}
	if g.llm == nil {
		return nil, errors.New("no llm configured")
	}
	lm, err := llm.NewModel(g.llm, string(m.ID), nil)
	if err != nil {
		return nil, err
	}
	return decision.NewFromLLM(lm, nil)
}

func (g *fakeGateway) Embedding(ctx context.Context, m models.Model) (*embedding.Model, error) {
	return nil, errors.New("unsupported")
}

func (g *fakeGateway) ListProviders() []gateway.ProviderInfo { return nil }

func (g *fakeGateway) ListModels(ctx context.Context, provider string) ([]profile.Profile, bool, error) {
	return nil, false, nil
}

func (g *fakeGateway) GetProfile(id string) (profile.Profile, bool) { return profile.Profile{}, false }

func (g *fakeGateway) Quota(ctx context.Context, m models.Model) (*api.ModelQuota, error) {
	return nil, nil
}

func (g *fakeGateway) QuotaSnapshots(ctx context.Context) (*api.QuotaSnapshotResponse, error) {
	return nil, nil
}

func (g *fakeGateway) QuotaCapable(m models.Model) bool { return false }

func (g *fakeGateway) Close() error { return nil }

// metricsRecorder captures permission_review outcome records.
type metricsRecorder struct {
	mu      sync.Mutex
	records []metrics.Record
}

func (r *metricsRecorder) Record(ctx context.Context, rec metrics.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *metricsRecorder) outcomeRecords() []metrics.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []metrics.Record
	for _, rec := range r.records {
		if rec.Kind == metrics.KindPermissionReview {
			out = append(out, rec)
		}
	}
	return out
}

func testCard() api.PermissionRequest {
	return api.PermissionRequest{
		ID:       "req-1",
		ToolName: "bash",
		Action:   api.ActionExec,
		Safety:   api.SafetyUnsafe,
		Target:   "rm -rf build/",
		Summary:  "delete build directory",
	}
}

// newTestService builds a Service wired to an isolated XDG home, a temp-dir
// workspace, a scripted gateway, and a real permissions.SessionManager.
// wsDir may carry .agents overrides; providerManifest selects the workspace
// provider model list.
func newTestService(t *testing.T, mode api.PermissionMode, wsDir string, decisionCaps bool) (*Service, *fakeGateway, permissions.SessionManager, *metricsRecorder) {
	t.Helper()
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-permreview-test")
	t.Cleanup(xdg.ClearCache)

	if wsDir == "" {
		wsDir = t.TempDir()
	}
	provDir := wsDir + "/.agents/providers"
	if err := mkdirAll(provDir); err != nil {
		t.Fatal(err)
	}
	decisionCap := "false"
	if decisionCaps {
		decisionCap = "true"
	}
	prov := fmt.Sprintf("apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: fake\nspec:\n  type: ollama\n  models:\n    - id: decider\n      capabilities:\n        completion: true\n        decision: %s\n        structuredOutput: true\n---\n", decisionCap)
	if err := writeFile(provDir+"/fake.provider.yaml", prov); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ws, err := workspace.Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	gw := &fakeGateway{
		native: &fakeDecisionProvider{},
		llm: &fakeLLMProvider{
			raw: `{"answers":{` +
				`"action":{"choice":"exec","probabilities":{"exec":0.97,"read":0.03}},` +
				`"safety":{"choice":"safe","probabilities":{"safe":0.97,"unsafe":0.03}},` +
				`"requested":{"choice":"yes","probabilities":{"yes":0.97,"no":0.03}}}}`,
		},
	}
	cat, err := models.Build(ws.Catalog(), gw)
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}
	rec := &metricsRecorder{}
	sess := permissions.NewSession(mode, api.Permissions{}, nil)
	svc := New(Deps{
		Permissions: sess,
		Workspace:   ws,
		Catalog:     func() *models.Catalog { return cat },
		Gateway:     func() gateway.Gateway { return gw },
		Metrics:     rec,
	})
	return svc, gw, sess, rec
}

func TestReviewDefersOutsideAutoMode(t *testing.T) {
	svc, gw, _, rec := newTestService(t, api.ModeDefault, "", true)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean the build"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonDeferred {
		t.Fatalf("verdict = %s/%s, want defer/deferred", res.Verdict, res.ReasonCode)
	}
	if gw.native.count() != 0 {
		t.Fatalf("expected 0 model calls outside auto, got %d", gw.native.count())
	}
	if len(rec.outcomeRecords()) != 1 {
		t.Fatalf("expected 1 outcome record, got %d", len(rec.outcomeRecords()))
	}
}

func TestReviewApproveOnceNative(t *testing.T) {
	svc, gw, _, rec := newTestService(t, api.ModeAuto, "", true)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean the build directory"})
	if res.Verdict != VerdictApproveOnce || res.ReasonCode != ReasonApproved {
		t.Fatalf("verdict = %s/%s, want approve_once/approved", res.Verdict, res.ReasonCode)
	}
	if res.Receipt == nil {
		t.Fatal("missing receipt on approval")
	}
	if res.Receipt.Source != DecisionReviewSource || res.Receipt.Verdict != string(VerdictApproveOnce) {
		t.Fatalf("receipt = %+v", res.Receipt)
	}
	if res.Receipt.Model != "fake/decider" || res.Receipt.DecisionScope != "system" || res.Receipt.RubricDigest == "" {
		t.Fatalf("receipt provenance = %+v", res.Receipt)
	}
	if res.Receipt.Threshold != 0.85 {
		t.Fatalf("threshold = %v, want built-in manifest threshold 0.85", res.Receipt.Threshold)
	}
	if gw.native.count() != 1 {
		t.Fatalf("expected 1 native call, got %d", gw.native.count())
	}
	outs := rec.outcomeRecords()
	if len(outs) != 1 || attrValue(outs[0], metrics.AttrOutcome) != metrics.ReviewOutcomeApproved {
		t.Fatalf("outcome records = %+v", outs)
	}
}

func TestReviewBelowThreshold(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	gw.native.resp = cannedResponse(0.80, false)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonBelowThreshold {
		t.Fatalf("verdict = %s/%s, want defer/below_threshold", res.Verdict, res.ReasonCode)
	}
	if res.Receipt != nil {
		t.Fatal("receipt must be nil on defer")
	}
}

func TestReviewRefusedDefers(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	gw.native.resp = cannedResponse(0.99, true)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonDeferred {
		t.Fatalf("verdict = %s/%s, want defer/deferred", res.Verdict, res.ReasonCode)
	}
}

func TestReviewManifestThresholdIsSourceOfTruth(t *testing.T) {
	for _, tc := range []struct {
		manifestThreshold string
		prob              float64
		wantApproved      bool
		wantThreshold     float64
	}{
		{"threshold: 0.75\n", 0.78, true, 0.75},
		{"threshold: 0.75\n", 0.72, false, 0},
		{"threshold: 0.98\n", 0.92, false, 0},
		{"threshold: 0.98\n", 0.99, true, 0.98},
		{"", 0.88, true, 0.85},
		{"", 0.80, false, 0},
	} {
		t.Run(fmt.Sprintf("spec=%q_p=%.2f", strings.TrimSpace(tc.manifestThreshold), tc.prob), func(t *testing.T) {
			wsDir := t.TempDir()
			decDir := wsDir + "/.agents/decisions"
			if err := mkdirAll(decDir); err != nil {
				t.Fatal(err)
			}
			manifest := testClassifierManifest(tc.manifestThreshold)
			if err := writeFile(decDir+"/permission-reviewer.decision.md", manifest); err != nil {
				t.Fatal(err)
			}
			svc, gw, _, _ := newTestService(t, api.ModeAuto, wsDir, true)
			gw.native.resp = cannedResponse(tc.prob, false)
			res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
			if tc.wantApproved {
				if res.Verdict != VerdictApproveOnce {
					t.Fatalf("verdict = %s (%s), want approve_once", res.Verdict, res.ReasonCode)
				}
				if res.Receipt.Threshold != tc.wantThreshold {
					t.Fatalf("effective threshold = %v, want %v", res.Receipt.Threshold, tc.wantThreshold)
				}
			} else if res.Verdict != VerdictDefer {
				t.Fatalf("verdict = %s, want defer", res.Verdict)
			}
		})
	}
}

func TestReviewAdaptedUsesSameManifestThreshold(t *testing.T) {
	wsDir := t.TempDir()
	decDir := wsDir + "/.agents/decisions"
	if err := mkdirAll(decDir); err != nil {
		t.Fatal(err)
	}
	manifest := testClassifierManifest("")
	if err := writeFile(decDir+"/permission-reviewer.decision.md", manifest); err != nil {
		t.Fatal(err)
	}
	svc, gw, _, _ := newTestService(t, api.ModeAuto, wsDir, false)
	gw.llm.raw = `{"answers":{` +
		`"action":{"choice":"exec","probabilities":{"exec":0.95,"read":0.05}},` +
		`"safety":{"choice":"safe","probabilities":{"safe":0.80,"unsafe":0.20}},` +
		`"requested":{"choice":"yes","probabilities":{"yes":0.95,"no":0.05}}}}`
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer {
		t.Fatalf("verdict = %s, want defer below 0.85 manifest threshold", res.Verdict)
	}
	if gw.llm.calls != 1 {
		t.Fatalf("expected 1 adapted llm call, got %d", gw.llm.calls)
	}
	gw.llm.raw = `{"answers":{` +
		`"action":{"choice":"exec","probabilities":{"exec":0.95,"read":0.05}},` +
		`"safety":{"choice":"safe","probabilities":{"safe":0.88,"unsafe":0.12}},` +
		`"requested":{"choice":"yes","probabilities":{"yes":0.95,"no":0.05}}}}`
	res = svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictApproveOnce {
		t.Fatalf("verdict = %s (%s), want approve_once at 0.88 using manifest threshold", res.Verdict, res.ReasonCode)
	}
	if res.Receipt.Threshold != 0.85 {
		t.Fatalf("threshold = %v, want manifest threshold 0.85", res.Receipt.Threshold)
	}
}

func TestReviewInvalidThresholdDefers(t *testing.T) {
	for _, bad := range []string{"0", "-0.2", "1.5"} {
		wsDir := t.TempDir()
		decDir := wsDir + "/.agents/decisions"
		if err := mkdirAll(decDir); err != nil {
			t.Fatal(err)
		}
		manifest := testClassifierManifest(fmt.Sprintf("threshold: %s\n", bad))
		if err := writeFile(decDir+"/permission-reviewer.decision.md", manifest); err != nil {
			t.Fatal(err)
		}
		svc, gw, _, _ := newTestService(t, api.ModeAuto, wsDir, true)
		res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
		if res.Verdict != VerdictDefer || res.ReasonCode != ReasonInvalidManifest {
			t.Fatalf("threshold %s: verdict = %s/%s, want defer/invalid_manifest", bad, res.Verdict, res.ReasonCode)
		}
		if gw.native.count() != 0 {
			t.Fatalf("threshold %s: expected 0 model calls", bad)
		}
	}
}

func TestReviewMalformedManifestDefers(t *testing.T) {
	wsDir := t.TempDir()
	decDir := wsDir + "/.agents/decisions"
	if err := mkdirAll(decDir); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Decision\nmetadata:\n  name: permission-reviewer\nspec:\n  models: [\"fake/decider\"]\n  questions:\n    - name: verdict\n      type: choice\n      options:\n        - value: approve_forever\n          description: bad\n        - value: defer_to_user\n          description: ask human\n---\nrubric\n"
	if err := writeFile(decDir+"/permission-reviewer.decision.md", manifest); err != nil {
		t.Fatal(err)
	}
	svc, gw, _, rec := newTestService(t, api.ModeAuto, wsDir, true)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonInvalidManifest {
		t.Fatalf("verdict = %s/%s, want defer/invalid_manifest", res.Verdict, res.ReasonCode)
	}
	if res.WarnOnce == "" {
		t.Fatal("expected warning on first malformed manifest")
	}
	if gw.native.count() != 0 {
		t.Fatalf("malformed manifest must not call the model, got %d calls", gw.native.count())
	}
	res2 := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res2.WarnOnce != "" {
		t.Fatalf("warning must dedupe per session, got %q", res2.WarnOnce)
	}
	outs := rec.outcomeRecords()
	if len(outs) != 2 || attrValue(outs[0], metrics.AttrOutcome) != metrics.ReviewOutcomeInvalidManifest {
		t.Fatalf("outcome records = %+v", outs)
	}
}

func TestReviewOversizeDefersWithoutCalls(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	req := testCard()
	req.Diff = strings.Repeat("x", maxCardEvidenceBytes+1)
	res := svc.Review(context.Background(), ReviewRequest{Request: req, UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonOversize {
		t.Fatalf("verdict = %s/%s, want defer/oversize", res.Verdict, res.ReasonCode)
	}
	if gw.native.count() != 0 {
		t.Fatalf("oversize card must not call the model, got %d", gw.native.count())
	}
}

func TestReviewPayloadAllowlist(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	intent := strings.Repeat("p", maxUserIntentBytes+500)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: intent})
	if res.Verdict != VerdictApproveOnce {
		t.Fatalf("verdict = %s, want approve_once", res.Verdict)
	}
	gw.native.mu.Lock()
	defer gw.native.mu.Unlock()
	req := gw.native.calls[0]
	if len(req.Questions) != 3 ||
		req.Questions[0].Name != actionQuestionName ||
		req.Questions[1].Name != safetyQuestionName ||
		req.Questions[2].Name != requestedQuestionName {
		t.Fatalf("questions = %+v", req.Questions)
	}
	qSafety := req.Questions[1]
	if !strings.Contains(qSafety.Instructions, "calibrated permission classifier") ||
		!strings.Contains(qSafety.Instructions, "Classify the real harm") {
		t.Fatalf("safety instructions missing rendered rubric or question instructions:\n%s", qSafety.Instructions)
	}
	if strings.Count(strings.ToLower(qSafety.Instructions), "untrusted data") < 1 {
		t.Fatalf("instructions missing untrusted data framing:\n%s", qSafety.Instructions)
	}
	raw, err := json.Marshal(req.Input.State)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("payload keys = %v, want exactly card+user_intent", decoded)
	}
	card, ok := decoded["card"].(map[string]any)
	if !ok {
		t.Fatalf("card missing or wrong type: %v", decoded["card"])
	}
	if _, leaked := card["action"]; leaked {
		t.Fatalf("static action pre-label must not be sent in card: %v", card)
	}
	if _, leaked := card["safety"]; leaked {
		t.Fatalf("static safety pre-label must not be sent in card: %v", card)
	}
	for k := range card {
		switch k {
		case "tool_name", "target", "summary", "diff", "script_preview",
			"inside_workspace", "protected_path", "sandbox_enforced", "unsandboxed", "bypass_sandbox":
		default:
			t.Fatalf("unexpected card field %q in outbound payload", k)
		}
	}
	ui, _ := decoded["user_intent"].(string)
	if !strings.HasSuffix(ui, "…[user prompt truncated]") || len(ui) > maxUserIntentBytes+40 {
		t.Fatalf("user_intent not truncated to budget: len=%d", len(ui))
	}
}

func TestReviewPayloadSandboxPostureAndScriptPreview(t *testing.T) {
	wsDir := t.TempDir()
	scriptsDir := wsDir + "/scripts"
	if err := mkdirAll(scriptsDir); err != nil {
		t.Fatal(err)
	}
	scriptBody := "#!/usr/bin/env sh\ngofmt -w .\n"
	if err := writeFile(scriptsDir+"/format.sh", scriptBody); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(wsDir+"/.env", "SECRET=1\n"); err != nil {
		t.Fatal(err)
	}

	svc, gw, sess, _ := newTestService(t, api.ModeAuto, wsDir, true)
	sess.SetRuntimePolicy(&permissions.RuntimePolicy{SandboxNative: true})

	card := api.PermissionRequest{
		ID:       "req-script",
		ToolName: "bash",
		Action:   api.ActionExec,
		Safety:   api.SafetyUnknown,
		Target:   "./scripts/format.sh",
		Summary:  "Format Go files",
	}
	res := svc.Review(context.Background(), ReviewRequest{
		Request:     card,
		ScriptPaths: []string{"./scripts/format.sh"},
		UserIntent:  "fix formatting",
	})
	if res.Verdict != VerdictApproveOnce {
		t.Fatalf("verdict = %s (%s), want approve_once", res.Verdict, res.ReasonCode)
	}
	gw.native.mu.Lock()
	raw, err := json.Marshal(gw.native.calls[len(gw.native.calls)-1].Input.State)
	gw.native.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Card struct {
			SandboxEnforced bool   `json:"sandbox_enforced"`
			ScriptPreview   string `json:"script_preview"`
		} `json:"card"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Card.SandboxEnforced {
		t.Fatal("expected sandbox_enforced=true when native sandbox is active and command is confined")
	}
	if !strings.Contains(payload.Card.ScriptPreview, "# scripts/format.sh\n"+scriptBody) {
		t.Fatalf("unexpected script_preview:\n%s", payload.Card.ScriptPreview)
	}

	// Secret .env files must never be attached to script_preview, and bypass_sandbox clears sandbox_enforced.
	envCard := api.PermissionRequest{
		ID:            "req-env",
		ToolName:      "bash",
		Action:        api.ActionExec,
		Safety:        api.SafetyUnsafe,
		Target:        "sh .env",
		Summary:       "Run .env",
		Unsandboxed:   true,
		BypassSandbox: true,
	}
	_ = svc.Review(context.Background(), ReviewRequest{
		Request:       envCard,
		ProtectedPath: true,
		ScriptPaths:   []string{".env"},
		UserIntent:    "run",
	})
	gw.native.mu.Lock()
	rawEnv, err := json.Marshal(gw.native.calls[len(gw.native.calls)-1].Input.State)
	gw.native.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	payload = struct {
		Card struct {
			SandboxEnforced bool   `json:"sandbox_enforced"`
			ScriptPreview   string `json:"script_preview"`
		} `json:"card"`
	}{}
	if err := json.Unmarshal(rawEnv, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Card.SandboxEnforced {
		t.Fatal("expected sandbox_enforced=false when bypass_sandbox=true")
	}
	if payload.Card.ScriptPreview != "" {
		t.Fatalf("secret .env leaked into script_preview: %q", payload.Card.ScriptPreview)
	}
}

func TestReviewPayloadUserIntentUTF8Truncation(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	// Place a 4-byte emoji ("🚀") straddling byte maxUserIntentBytes (4096):
	// 4094 ASCII bytes + "🚀" (bytes 4094..4097) + trailing text.
	intent := strings.Repeat("a", maxUserIntentBytes-2) + "🚀" + strings.Repeat("b", 200)
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: intent})
	if res.Verdict != VerdictApproveOnce {
		t.Fatalf("verdict = %s, want approve_once", res.Verdict)
	}
	gw.native.mu.Lock()
	req := gw.native.calls[0]
	gw.native.mu.Unlock()
	raw, err := json.Marshal(req.Input.State)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		UserIntent string `json:"user_intent"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(decoded.UserIntent) {
		t.Fatalf("truncated user_intent produced invalid UTF-8: %q", decoded.UserIntent)
	}
	if !strings.HasSuffix(decoded.UserIntent, "\n…[user prompt truncated]") {
		t.Fatalf("missing truncation suffix: %q", decoded.UserIntent)
	}
	prefix := strings.TrimSuffix(decoded.UserIntent, "\n…[user prompt truncated]")
	if len(prefix) != maxUserIntentBytes-2 || !utf8.ValidString(prefix) {
		t.Fatalf("prefix len = %d (valid=%v), want %d (rune boundary before emoji)", len(prefix), utf8.ValidString(prefix), maxUserIntentBytes-2)
	}
}

func TestReviewNoModelWarns(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-permreview-test")
	t.Cleanup(xdg.ClearCache)
	wsDir := t.TempDir()
	ws, err := workspace.Open(context.Background(), wsDir)
	if err != nil {
		t.Fatal(err)
	}
	gw := &fakeGateway{}
	cat, err := models.Build(ws.Catalog(), gw)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{
		Permissions: permissions.NewSession(api.ModeAuto, api.Permissions{}, nil),
		Workspace:   ws,
		Catalog:     func() *models.Catalog { return cat },
		Gateway:     func() gateway.Gateway { return gw },
	})
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonNoModel {
		t.Fatalf("verdict = %s/%s, want defer/no_model", res.Verdict, res.ReasonCode)
	}
	if res.WarnOnce == "" {
		t.Fatal("expected one-time warning when no reviewer model is configured")
	}
}

func TestReviewModelFailureDefers(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	gw.native.err = errors.New("boom")
	res := svc.Review(context.Background(), ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonError {
		t.Fatalf("verdict = %s/%s, want defer/error", res.Verdict, res.ReasonCode)
	}
	if res.WarnOnce == "" {
		t.Fatal("expected warning on model failure")
	}
}

func TestReviewCanceledContext(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	ctx, cancel := context.WithCancel(context.Background())
	gw.native.waitCtx = true
	done := make(chan Result, 1)
	go func() {
		done <- svc.Review(ctx, ReviewRequest{Request: testCard(), UserIntent: "clean"})
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	res := <-done
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonCanceled {
		t.Fatalf("verdict = %s/%s, want defer/canceled", res.Verdict, res.ReasonCode)
	}
}

func TestReviewTimeoutReason(t *testing.T) {
	svc, gw, _, _ := newTestService(t, api.ModeAuto, "", true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	gw.native.errAfter = 1
	gw.native.err = context.DeadlineExceeded
	res := svc.Review(ctx, ReviewRequest{Request: testCard(), UserIntent: "clean"})
	if res.Verdict != VerdictDefer || res.ReasonCode != ReasonTimeout {
		t.Fatalf("verdict = %s/%s, want defer/timeout", res.Verdict, res.ReasonCode)
	}
}

func attrValue(rec metrics.Record, key string) string {
	for _, a := range rec.Attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o755) }

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
