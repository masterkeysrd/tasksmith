package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	loomauth "github.com/masterkeysrd/loom/auth"
)

func TestOAuthCallbackTemplateRender(t *testing.T) {
	var buf bytes.Buffer
	err := oauthCallbackTemplate.Execute(&buf, loomauth.CallbackData{
		Success: true,
	})
	if err != nil {
		t.Fatalf("execute success template failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Authentication Successful") {
		t.Errorf("expected 'Authentication Successful', got %s", out)
	}
	if !strings.Contains(out, "TaskSmith") {
		t.Errorf("expected 'TaskSmith', got %s", out)
	}
	if !strings.Contains(out, "You can safely close this browser window and return to your terminal.") {
		t.Errorf("expected safely close text, got %s", out)
	}

	buf.Reset()
	err = oauthCallbackTemplate.Execute(&buf, loomauth.CallbackData{
		Success:          false,
		Error:            "access_denied",
		ErrorDescription: "The user denied access",
	})
	if err != nil {
		t.Fatalf("execute failure template failed: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "Authentication Failed") {
		t.Errorf("expected 'Authentication Failed', got %s", out)
	}
	if !strings.Contains(out, "access_denied") {
		t.Errorf("expected error code 'access_denied', got %s", out)
	}
	if !strings.Contains(out, "The user denied access") {
		t.Errorf("expected error description, got %s", out)
	}
}

func TestParseCallbackInput(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "full callback url", raw: "http://localhost:51121/callback?code=abc&state=xyz", wantErr: false},
		{name: "bare query", raw: "code=abc&state=xyz", wantErr: false},
		{name: "fragment params", raw: "http://localhost:51121/callback#code=abc&state=xyz", wantErr: false},
		{name: "bare code", raw: "abc", wantErr: false},
		{name: "empty", raw: "", wantErr: true},
		{name: "url without code", raw: "http://localhost:51121/callback", wantErr: true},
		{name: "query without code", raw: "state=xyz", wantErr: true},
		{name: "provider error", raw: "error=access_denied&state=xyz", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, err := ParseCallbackInput(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidCallback) {
					t.Fatalf("ParseCallbackInput(%q) = (%q, %v), want ErrInvalidCallback", tt.raw, code, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCallbackInput(%q): %v", tt.raw, err)
			}
			if tt.name == "bare code" && code != "abc" {
				t.Fatalf("bare code = %q, want abc", code)
			}
		})
	}
}
