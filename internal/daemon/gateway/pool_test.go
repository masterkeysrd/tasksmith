package gateway

import (
	"errors"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
)

type poolTestGateway struct {
	Gateway
	closeErr error
	closes   int
}

func (g *poolTestGateway) Close() error {
	g.closes++
	return g.closeErr
}

func TestPoolCloseAggregatesGatewayErrors(t *testing.T) {
	first := &poolTestGateway{closeErr: errors.New("first gateway close failed")}
	second := &poolTestGateway{closeErr: errors.New("second gateway close failed")}
	pool := &poolImpl{gateways: map[string]Gateway{"first": first, "second": second}}

	err := pool.Close()
	for _, gw := range []*poolTestGateway{first, second} {
		if !errors.Is(err, gw.closeErr) {
			t.Errorf("pool Close error %v does not contain %v", err, gw.closeErr)
		}
	}
	if len(pool.gateways) != 0 {
		t.Errorf("pool retained %d gateways after Close", len(pool.gateways))
	}
	if err := pool.Close(); err != nil {
		t.Errorf("closing the pool again: %v", err)
	}
	if first.closes != 1 || second.closes != 1 {
		t.Errorf("first and second gateway close counts: got %d and %d, want 1 and 1", first.closes, second.closes)
	}
}

type poolTestOAuthDriver struct {
	auth.BrowserOAuthDriver
	closes int
}

func (d *poolTestOAuthDriver) Close() error {
	d.closes++
	return nil
}

func TestPoolCloseLeavesOtherPoolsAndOAuthDriversOpen(t *testing.T) {
	const provider = providers.ClaudeCodeName
	original, ok := auth.GetBrowserOAuthDriver(provider)
	if !ok {
		t.Fatalf("browser OAuth driver %q not registered", provider)
	}
	driver := &poolTestOAuthDriver{BrowserOAuthDriver: original}
	auth.RegisterBrowserOAuthDriver(provider, driver)
	t.Cleanup(func() { auth.RegisterBrowserOAuthDriver(provider, original) })

	own, peer := &poolTestGateway{}, &poolTestGateway{}
	pool := &poolImpl{gateways: map[string]Gateway{"same-key": own}}
	other := &poolImpl{gateways: map[string]Gateway{"same-key": peer}}

	if err := pool.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if own.closes != 1 || peer.closes != 0 || other.closed {
		t.Errorf("gateway close counts: own = %d, peer = %d, other pool closed = %t; want 1, 0, false", own.closes, peer.closes, other.closed)
	}
	if driver.closes != 0 {
		t.Errorf("driver close count = %d, want 0", driver.closes)
	}
}
