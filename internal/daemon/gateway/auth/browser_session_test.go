package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"golang.org/x/oauth2"
)

type browserTestTokenSource func() (*oauth2.Token, error)

func (f browserTestTokenSource) Token() (*oauth2.Token, error) { return f() }

func TestBrowserSessionSharesRefreshAndRevokesOldSources(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-browser-session-test")
	store := NewBrowserSessionStore(
		"test-provider",
		func(account string) string { return account },
		func(_ context.Context, account *string, save func(string) error) (oauth2.TokenSource, error) {
			return browserTestTokenSource(func() (*oauth2.Token, error) {
				refreshed := *account + "-refreshed"
				if err := save(refreshed); err != nil {
					return nil, err
				}
				return &oauth2.Token{AccessToken: refreshed}, nil
			}), nil
		},
	)
	if err := store.Save("initial"); err != nil {
		t.Fatal(err)
	}
	revision := store.CredentialRevision()
	if revision == "" {
		t.Fatal("saved registration has no revision")
	}
	first, err := store.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if token, err := first.Token(); err != nil || token.AccessToken != "initial-refreshed" {
		t.Fatalf("refresh returned %+v, %v", token, err)
	}
	savedAccount, err := store.LoadAccount()
	if err != nil || savedAccount != "initial-refreshed" || store.CredentialRevision() != revision {
		t.Fatalf("refresh persisted account %q, revision %q, error %v", savedAccount, store.CredentialRevision(), err)
	}
	if account, ok := store.Account(); !ok || account != savedAccount {
		t.Fatalf("account reported %q, %v", account, ok)
	}
	if second, err := store.TokenSource(); err != nil || second != first {
		t.Fatalf("refresh rebuilt the shared token source: %v", err)
	}

	if err := store.Save("replacement"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Token(); !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("old source after reauthorization: %v", err)
	}
	replacement, err := store.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.Logout(); err != nil || !deleted {
		t.Fatalf("logout returned %v, %v", deleted, err)
	}
	if _, err := replacement.Token(); !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("replacement source after sign-out: %v", err)
	}
}
