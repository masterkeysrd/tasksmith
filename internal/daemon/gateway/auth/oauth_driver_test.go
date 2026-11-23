package auth

import (
	"errors"
	"testing"
)

type browserOAuthTestCloser struct {
	BrowserOAuthDriver
	closeFunc func() error
}

func (d *browserOAuthTestCloser) Close() error { return d.closeFunc() }

func TestCloseBrowserOAuthDrivers(t *testing.T) {
	oauthDriversMu.Lock()
	original := browserOAuthDrivers
	browserOAuthDrivers = make(map[string]BrowserOAuthDriver)
	oauthDriversMu.Unlock()
	t.Cleanup(func() {
		oauthDriversMu.Lock()
		browserOAuthDrivers = original
		oauthDriversMu.Unlock()
	})

	firstErr := errors.New("first driver close failed")
	secondErr := errors.New("second driver close failed")
	calls := make(map[error]int)
	newCloser := func(err error) *browserOAuthTestCloser {
		return &browserOAuthTestCloser{closeFunc: func() error {
			calls[err]++
			if oauthDriversMu.TryLock() {
				oauthDriversMu.Unlock()
			} else {
				t.Error("driver Close called while the registry was locked")
			}
			return err
		}}
	}
	drivers := map[string]BrowserOAuthDriver{
		"first":  newCloser(firstErr),
		"second": newCloser(secondErr),
		"plain":  struct{ BrowserOAuthDriver }{},
	}
	for name, driver := range drivers {
		RegisterBrowserOAuthDriver(name, driver)
	}

	err := CloseBrowserOAuthDrivers()
	for _, want := range []error{firstErr, secondErr} {
		if calls[want] != 1 || !errors.Is(err, want) {
			t.Errorf("driver error %v: got %d closes and joined error %v", want, calls[want], err)
		}
	}
	for name, want := range drivers {
		if got, ok := GetBrowserOAuthDriver(name); !ok || got != want {
			t.Errorf("registration %q changed after shutdown", name)
		}
	}
}
