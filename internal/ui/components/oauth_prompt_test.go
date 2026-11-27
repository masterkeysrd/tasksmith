package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/terminal"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

type oauthClipboard struct {
	terminal.Clipboard
	writes []string
}

func (c *oauthClipboard) WriteText(text string) *promise.Promise[struct{}] {
	c.writes = append(c.writes, text)
	return promise.Resolved(struct{}{})
}

func TestOAuthPromptCopySubmitAndClose(t *testing.T) {
	env := testutils.NewEnv(t, 100, 30)
	clip := &oauthClipboard{}
	env.Document().SetClipboardProvider(clip)
	var copies, closes int
	var submissions []string
	props := OAuthPromptProps{
		Title: "Sign in", Status: "Waiting", AuthURL: "https://example.com/authorize",
		OnCopy:   func() { copies++ },
		OnSubmit: func(url string) { submissions = append(submissions, url) },
		OnClose:  func() { closes++ },
	}
	out := env.Render(OAuthPrompt(props))
	for _, want := range []string{props.Title, props.Status, props.AuthURL, "ctrl+y", "Copy URL", "Back"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	out = env.Press(key.Key{Code: 'y', Mod: key.ModCtrl})
	if copies != 1 || !slices.Equal(clip.writes, []string{props.AuthURL}) || !strings.Contains(out, "Copied authorization URL") {
		t.Fatalf("copy hook=%d writes=%v:\n%s", copies, clip.writes, out)
	}
	props.Status = "Retry the callback"
	out = env.Render(OAuthPrompt(props))
	if strings.Contains(out, "Copied authorization URL") || !strings.Contains(out, props.Status) {
		t.Fatalf("status did not refresh:\n%s", out)
	}
	const callback = "http://localhost/callback?code=abc"
	env.Type("  " + callback + " ")
	env.Press(key.Key{Code: key.KeyEnter})
	env.Press(key.Key{Code: key.KeyEnter})
	if !slices.Equal(submissions, []string{callback}) {
		t.Fatalf("submissions = %q", submissions)
	}
	env.Press(key.Key{Code: key.KeyEscape})
	if closes != 1 {
		t.Fatalf("close calls = %d", closes)
	}
}

func TestOAuthPromptBrowserOnly(t *testing.T) {
	env := testutils.NewEnv(t, 100, 30)
	clip := &oauthClipboard{}
	env.Document().SetClipboardProvider(clip)
	closed := false
	props := OAuthPromptProps{
		BrowserOnly: true, AuthURL: "https://example.com/authorize", EscLabel: "Close",
		Status:   "Continue in your browser",
		OnSubmit: func(string) { t.Error("unexpected callback") },
		OnClose:  func() { closed = true },
	}
	out := env.Render(OAuthPrompt(props))
	for _, hidden := range []string{props.AuthURL, "Copy URL", "Submit pasted URL", "URL >"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("browser-only screen shows %q:\n%s", hidden, out)
		}
	}
	if !strings.Contains(out, props.Status) || !strings.Contains(out, "Close") {
		t.Fatal(out)
	}
	env.Press(key.Key{Code: 'y', Mod: key.ModCtrl})
	env.Type("abc")
	env.Press(key.Key{Code: key.KeyEnter})
	env.Press(key.Key{Code: key.KeyEscape})
	if len(clip.writes) != 0 || !closed {
		t.Fatalf("writes=%v closed=%t", clip.writes, closed)
	}
}
