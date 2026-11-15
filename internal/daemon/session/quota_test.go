package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/warp"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// quotaProfiles answers the catalog's quota capability check for the "pool"
// provider so built models come out subscription-billed.
type quotaProfiles struct{}

func (quotaProfiles) GetProfile(string) (profile.Profile, bool) { return profile.Profile{}, false }
func (quotaProfiles) QuotaCapable(m models.Model) bool          { return m.ID.Provider() == "pool" }

func quotaTestCatalog(t *testing.T, ws *workspace.Workspace) func() *models.Catalog {
	t.Helper()
	reg := warp.NewRegistry(nil)
	p := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "pool"},
		},
		Spec: warp.ModelProviderSpec{Type: "fake", Models: []warp.ProviderModel{{ID: "free"}}},
	}
	reg.Set(p.QualifiedName(), p)
	cat := workspace.NewCatalog(reg, ws.RootDir(), ws.CWD())
	catalog, err := models.Build(cat, quotaProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	return func() *models.Catalog { return catalog }
}

// setupQuotaEnv wires a session coordinator whose catalog has one quota-billed
// model (pool/free) and whose gateway answers quota reads with resp.
func setupQuotaEnv(t *testing.T, prov *fakeProvider, resp *api.ModelQuota) (*fakeGateway, *testPublisher, *testCoordinator) {
	t.Helper()
	db, ws, gw, pub, _ := setupTestEnv(t, prov)
	gw.setQuota(resp, nil)
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := newTestCoordinator(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
		Catalog:     quotaTestCatalog(t, ws),
	})
	if err != nil {
		t.Fatal(err)
	}
	return gw, pub, mgr
}

func quotaBilledSession(t *testing.T, mgr *testCoordinator) *Session {
	t.Helper()
	sess, _, err := mgr.Create(context.Background(), api.CreateSessionRequest{
		Title:  "quota",
		Config: api.SessionConfig{Model: "pool/free"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sess.quotaBilledModel(); !ok {
		t.Fatal("pool/free should resolve as quota-billed")
	}
	return sess
}

func quotaEvents(t *testing.T, pub *testPublisher, sess *Session) []api.QuotaUpdatedEvent {
	t.Helper()
	topic := api.SessionStreamTopic(sess.ID)
	var out []api.QuotaUpdatedEvent
	for _, rec := range pub.recordedRecords() {
		if rec.topic != topic {
			continue
		}
		if ev, ok := rec.event.(api.QuotaUpdatedEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

// waitForQuotaEvents polls until want events landed (or fails on timeout).
// settle > 0 adds quiet time to catch extra late publishes.
func waitForQuotaEvents(t *testing.T, pub *testPublisher, sess *Session, want int, settle time.Duration) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := quotaEvents(t, pub, sess)
		if len(got) >= want {
			if settle > 0 {
				time.Sleep(settle)
				if extra := len(quotaEvents(t, pub, sess)) - want; extra > 0 {
					t.Fatalf("got %d extra quota_updated events", extra)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota_updated events = %d, want %d", len(got), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPublishQuotaAfterFastCompletedTurn(t *testing.T) {
	gw, pub, mgr := setupQuotaEnv(t, nil, &api.ModelQuota{Model: "pool/free", Provider: "pool", RemainingFraction: 0.92})
	t.Cleanup(func() { _ = mgr.Close() })
	sess := quotaBilledSession(t, mgr)

	// A sub-5s completed turn skips the timeline summary but must still
	// refresh the quota window.
	sess.beginTurn("m1", time.Now().Add(-time.Second))
	sess.finishTurn(api.TurnStatusDone)

	if got := len(turnEntries(t, sess)); got != 0 {
		t.Fatalf("fast turn wrote %d timeline entries", got)
	}
	waitForQuotaEvents(t, pub, sess, 1, 50*time.Millisecond)
	ev := quotaEvents(t, pub, sess)[0]
	if ev.SessionID != sess.ID || ev.Quota.Model != "pool/free" || ev.Quota.RemainingFraction != 0.92 {
		t.Fatalf("event = %+v", ev)
	}
	if n := gw.quotaCallCount(); n != 1 {
		t.Fatalf("quota reads = %d, want 1", n)
	}
}

func TestPublishQuotaAfterLongCompletedTurn(t *testing.T) {
	gw, pub, mgr := setupQuotaEnv(t, nil, &api.ModelQuota{Model: "pool/free", RemainingFraction: 0.5})
	t.Cleanup(func() { _ = mgr.Close() })
	sess := quotaBilledSession(t, mgr)

	sess.beginTurn("m1", time.Now().Add(-8*time.Second))
	sess.finishTurn(api.TurnStatusDone)

	if got := len(turnEntries(t, sess)); got != 1 {
		t.Fatalf("long turn wrote %d timeline entries, want 1", got)
	}
	waitForQuotaEvents(t, pub, sess, 1, 50*time.Millisecond)
	if n := gw.quotaCallCount(); n != 1 {
		t.Fatalf("quota reads = %d, want 1", n)
	}
}

func TestRateLimitedTurnPublishesQuotaOnce(t *testing.T) {
	prov := &fakeProvider{
		handler: func(context.Context, *llm.Request) ([]string, string, error) {
			return nil, "", errors.New("status 429: rate limit exceeded")
		},
	}
	gw, pub, mgr := setupQuotaEnv(t, prov, &api.ModelQuota{Model: "pool/free", RemainingFraction: 0.03})
	t.Cleanup(func() { _ = mgr.Close() })
	gw.mu.Lock()
	gw.quotaDelay = 80 * time.Millisecond
	gw.mu.Unlock()
	sess := quotaBilledSession(t, mgr)

	if err := sess.Send(context.Background(), api.SendRequest{Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(context.Background()); err == nil {
		t.Fatal("expected rate-limit error from Wait")
	}

	// handleAsyncError starts the refresh and the deferred finishTurn would
	// start a second; exactly one read and one event must survive.
	waitForQuotaEvents(t, pub, sess, 1, 200*time.Millisecond)
	if n := gw.quotaCallCount(); n != 1 {
		t.Fatalf("quota reads = %d, want 1", n)
	}
}

func TestPublishQuotaSkipsNonQuotaModel(t *testing.T) {
	gw, _, mgr := setupQuotaEnv(t, nil, &api.ModelQuota{Model: "pool/free"})
	t.Cleanup(func() { _ = mgr.Close() })
	sess, _, err := mgr.Create(context.Background(), api.CreateSessionRequest{
		Title:  "priced",
		Config: api.SessionConfig{Model: "fake/fake-default-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess.beginTurn("m1", time.Now().Add(-8*time.Second))
	sess.finishTurn(api.TurnStatusDone)
	time.Sleep(50 * time.Millisecond)
	if n := gw.quotaCallCount(); n != 0 {
		t.Fatalf("quota reads = %d, want 0 for a per-token model", n)
	}
}

func TestPublishQuotaStopsWithSessionContext(t *testing.T) {
	gw, pub, mgr := setupQuotaEnv(t, nil, &api.ModelQuota{Model: "pool/free"})
	t.Cleanup(func() { _ = mgr.Close() })
	gw.mu.Lock()
	gw.quotaDelay = 300 * time.Millisecond
	gw.mu.Unlock()
	sess := quotaBilledSession(t, mgr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sess.bgCtx = ctx

	sess.publishQuota()
	time.Sleep(150 * time.Millisecond)
	if got := quotaEvents(t, pub, sess); len(got) != 0 {
		t.Fatalf("published %+v after session context closed", got)
	}
}

func TestTurnCompletedRecordQuotaBilled(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })
	sess, _, err := mgr.Create(context.Background(), api.CreateSessionRequest{Title: "rec"})
	if err != nil {
		t.Fatal(err)
	}

	sess.metrics = &fakeTurnMetrics{usage: api.UsageGroup{Calls: 2, QuotaCalls: 2, Tokens: api.UsageTokens{Input: 10, Output: 5}}}
	sess.beginTurn("q1", time.Now().Add(-8*time.Second))
	sess.finishTurn(api.TurnStatusDone)

	sess.metrics = &fakeTurnMetrics{usage: api.UsageGroup{Calls: 2, QuotaCalls: 1}}
	sess.beginTurn("m2", time.Now().Add(-8*time.Second))
	sess.finishTurn(api.TurnStatusDone)

	entries := turnEntries(t, sess)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if !entries[0].TurnCompleted.QuotaBilled {
		t.Fatalf("all-quota turn not flagged: %+v", entries[0].TurnCompleted)
	}
	if entries[1].TurnCompleted.QuotaBilled {
		t.Fatalf("mixed turn flagged as quota-billed: %+v", entries[1].TurnCompleted)
	}
}
