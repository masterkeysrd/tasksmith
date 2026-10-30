package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

func TestRuntimeSharesGatewayAcrossModelOverrides(t *testing.T) {
	ctx := context.Background()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-catalog-pool-test")
	t.Cleanup(xdg.ClearCache)
	pool := gateway.NewPool()
	defer pool.Close()
	mgr, err := New(Config{GatewayPool: pool, Publisher: reloadPublisher{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	var runtimes []*workspaceRuntime
	for _, limit := range []int{8000, 16000} {
		root := t.TempDir()
		dir := filepath.Join(root, ".agents")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := fmt.Sprintf("apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: local\nspec:\n  type: ollama\n  models:\n    - id: sample\n      limits:\n        context: %d\n", limit)
		if err := os.WriteFile(filepath.Join(dir, "local.provider.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		rt, err := mgr.Get(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		runtimes = append(runtimes, rt.(*workspaceRuntime))
		model, ok := rt.Catalog().Get("local/sample")
		if !ok || model.Limits.Context != limit {
			t.Fatalf("catalog limit: %+v", model)
		}
	}
	if runtimes[0].gw.(*switchingGateway).snapshot() != runtimes[1].gw.(*switchingGateway).snapshot() {
		t.Fatal("identical providers did not share a gateway")
	}
}

func TestRuntimeBuildsAndSwapsCatalog(t *testing.T) {
	ctx := context.Background()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-catalog-test")
	t.Cleanup(xdg.ClearCache)
	root := t.TempDir()
	workspaceManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: catalog-test\nspec:\n  projects: [\".\"]\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(workspaceManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "local.provider.yaml")
	writeProvider := func(context int) {
		t.Helper()
		content := fmt.Sprintf("apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: local\nspec:\n  type: ollama\n  defaultModel: sample\n  models:\n    - id: sample\n      limits:\n        context: %d\n        output: 1000\n", context)
		if err := os.WriteFile(provider, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeProvider(8000)

	pool := gateway.NewPool()
	defer pool.Close()
	mgr, err := New(Config{GatewayPool: pool, Publisher: reloadPublisher{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	rt, err := mgr.Get(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	// The catalog is built on create and exposes the manifest-listed model.
	cat := rt.Catalog()
	if cat == nil {
		t.Fatal("catalog is nil")
	}
	m, ok := cat.Get(models.ID("local/sample"))
	if !ok {
		t.Fatal("model not found in catalog")
	}
	if m.Limits.Context != 8000 || m.Limits.Output != 1000 {
		t.Fatalf("unexpected limits: %+v", m.Limits)
	}
	if !m.Source.Manifest {
		t.Fatalf("source should record manifest: %+v", m.Source)
	}

	// Resolve uses warp's role/priority rules.
	if m, ok := cat.Resolve(models.RoleCompletion); !ok || m.ID != models.ID("local/sample") {
		t.Fatalf("resolve: %+v ok=%v", m, ok)
	}

	// Reload rebuilds and swaps the catalog; the new limits are visible.
	writeProvider(16000)
	if err := rt.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if m, ok := rt.Catalog().Get(models.ID("local/sample")); !ok || m.Limits.Context != 16000 {
		t.Fatalf("reloaded catalog: %+v ok=%v", m, ok)
	}
	if m, ok := cat.Get(models.ID("local/sample")); !ok || m.Limits.Context != 8000 {
		t.Fatalf("old catalog changed after reload: %+v ok=%v", m, ok)
	}
}

type failingReloadPool struct {
	gateway.Pool
	fail bool
}

func (p *failingReloadPool) Acquire(ctx context.Context, cfg gateway.Config) (gateway.Gateway, error) {
	if p.fail {
		return nil, fmt.Errorf("provider unavailable")
	}
	return p.Pool.Acquire(ctx, cfg)
}

type reloadPublisher struct{}

func (reloadPublisher) Publish(string, broker.Event) {}
func (reloadPublisher) DeleteTopic(string)           {}

func TestReloadRefreshesModelDiagnostics(t *testing.T) {
	ctx := context.Background()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-runtime-test")
	t.Cleanup(xdg.ClearCache)
	root := t.TempDir()
	workspaceManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: reload-test\nspec:\n  projects: [\".\", \"sub\"]\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(workspaceManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "local.provider.yaml")
	writeProvider := func(supported bool) {
		t.Helper()
		content := fmt.Sprintf("apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: local\nspec:\n  type: ollama\n  defaultModel: sample\n  models:\n    - id: sample\n      capabilities:\n        completion: true\n        temperature: %t\n", supported)
		if err := os.WriteFile(provider, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent := filepath.Join(dir, "custom.agent.md")
	writeAgent := func(temperature bool) {
		t.Helper()
		spec := "  temperature: 0.3\n"
		if !temperature {
			spec = "  description: no temperature\n"
		}
		content := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: custom\nspec:\n" + spec + "---\nHelpful.\n"
		if err := os.WriteFile(agent, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeAgent(true)
	writeProvider(false)
	pool := &failingReloadPool{Pool: gateway.NewPool()}
	defer pool.Close()
	mgr, err := New(Config{GatewayPool: pool, Publisher: reloadPublisher{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	rt, err := mgr.Get(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := mgr.Get(ctx, subdir)
	if err != nil {
		t.Fatal(err)
	}
	if sibling.SessionRuntime() != rt || rt.SessionRuntime() != rt {
		t.Fatal("shared sessions must use the owning runtime")
	}
	sess, _, err := rt.Sessions().Create(ctx, session.CreateParams{Title: "test", Config: session.SessionConfig{Agent: "custom", Model: "local/sample"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Diagnostics) != 1 || sess.Diagnostics[0].Code != "temperature_unsupported" {
		t.Fatalf("initial diagnostics: %+v", sess.Diagnostics)
	}
	writeAgent(false)
	writeProvider(true)
	pool.fail = true
	if err := sibling.Reload(ctx); err == nil {
		t.Fatal("reload unexpectedly succeeded")
	}
	oldAgent, ok := rt.Workspace().GetAgent("custom")
	oldModel, found := rt.Catalog().Get("local/sample")
	if !ok || oldAgent.Temperature == nil || !found || oldModel.Capabilities.Temperature == nil || *oldModel.Capabilities.Temperature || len(sess.Diagnostics) != 1 {
		t.Fatalf("failed reload changed active configuration: %+v, %+v, %+v", oldAgent, oldModel, sess.Diagnostics)
	}
	pool.fail = false
	writeProvider(false)
	if err := sibling.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sess.Diagnostics) != 0 {
		t.Fatalf("diagnostics after agent reload: %+v", sess.Diagnostics)
	}
	writeAgent(true)
	writeProvider(true)
	if err := sibling.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sess.Diagnostics) != 0 {
		t.Fatalf("diagnostics after reload: %+v", sess.Diagnostics)
	}
	resolved, found := rt.Catalog().Get("local/sample")
	if !found || resolved.Capabilities.Temperature == nil || !*resolved.Capabilities.Temperature {
		t.Fatalf("reloaded model: %+v", resolved)
	}

	results := make(chan error, 1)
	go func() {
		for range 20 {
			created, _, err := rt.Sessions().Create(ctx, session.CreateParams{Title: "concurrent", Config: session.SessionConfig{Agent: "custom", Model: "local/sample"}})
			if err != nil {
				results <- err
				return
			}
			if diagnostics := created.Clone().Diagnostics; len(diagnostics) != 0 {
				results <- fmt.Errorf("mixed reload configuration: %+v", diagnostics)
				return
			}
		}
		results <- nil
	}()
	for i := range 8 {
		supported := i%2 == 0
		writeAgent(supported)
		writeProvider(supported)
		if err := sibling.Reload(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
}
