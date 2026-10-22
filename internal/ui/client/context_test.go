package client

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	daemonclient "github.com/masterkeysrd/tasksmith/client"
)

func testClient(t *testing.T, cwd string) *daemonclient.Client {
	t.Helper()
	c, err := daemonclient.New("/daemon.sock", daemonclient.WithCWD(cwd))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestProviderScopesClientWithoutWind(t *testing.T) {
	outer := testClient(t, "/outer")
	inner := testClient(t, "/inner")
	clients := make(map[string]*daemonclient.Client)
	consumer := kitex.FC("ClientConsumer", func(name string) kitex.Node {
		clients[name] = UseClient()
		return kitex.Text(name)
	})
	inst, err := trail.New(
		Provider(Props{Client: outer},
			consumer("outer"),
			Provider(Props{Client: inner}, consumer("inner")),
			consumer("sibling"),
		),
		trail.WithBackend(mock.New(80, 24)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	if clients["outer"] != outer || clients["inner"] != inner || clients["sibling"] != outer {
		t.Errorf("incorrect provider scope: %v", clients)
	}
}

func TestProviderUpdatesClient(t *testing.T) {
	first := testClient(t, "/first")
	second := testClient(t, "/second")
	var current *daemonclient.Client
	consumer := kitex.FC("ClientConsumer", func(_ struct{}) kitex.Node {
		current = UseClient()
		return kitex.Text("client")
	})
	inst, err := trail.New(
		Provider(Props{Client: first}, consumer(struct{}{})),
		trail.WithBackend(mock.New(80, 24)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	if current != first {
		t.Fatal("consumer did not receive initial client")
	}
	inst.Rerender(Provider(Props{Client: second}, consumer(struct{}{})))
	if current != second {
		t.Fatal("consumer did not receive updated client")
	}
}

func assertPanic(t *testing.T, message string, fn func()) {
	t.Helper()
	defer func() {
		if value := recover(); value == nil || !strings.Contains(fmt.Sprint(value), message) {
			t.Errorf("panic = %v, want message containing %q", value, message)
		}
	}()
	fn()
}

func TestProviderRequiresClient(t *testing.T) {
	assertPanic(t, "Provider requires a daemon client", func() {
		Provider(Props{})
	})
}

func TestUseClientRequiresProvider(t *testing.T) {
	consumer := kitex.FC("MissingClient", func(_ struct{}) kitex.Node {
		assertPanic(t, "UseClient must be used within a client.Provider", func() {
			UseClient()
		})
		return kitex.Text("client")
	})
	inst, err := trail.New(consumer(struct{}{}), trail.WithBackend(mock.New(80, 24)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
}
