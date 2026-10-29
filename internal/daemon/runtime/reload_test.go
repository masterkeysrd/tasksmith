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
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

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
	oldProfile, err := rt.Gateway().ResolveConfig("local", "sample", gateway.ModelSettings{})
	if !ok || oldAgent.Temperature == nil || err != nil || oldProfile.Profile.Profile.Capabilities.Temperature || len(sess.Diagnostics) != 1 {
		t.Fatalf("failed reload changed active configuration: %+v, %+v, %v", oldAgent, sess.Diagnostics, err)
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
	resolved, err := rt.Gateway().ResolveConfig("local", "sample", gateway.ModelSettings{})
	if err != nil || !resolved.Profile.TemperatureKnown || !resolved.Profile.Profile.Capabilities.Temperature {
		t.Fatalf("reloaded profile: %+v, %v", resolved, err)
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
