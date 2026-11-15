package daemon

import (
	"bytes"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/auth"
)

func TestOAuthCallbackTemplateRender(t *testing.T) {
	// Success case
	var buf bytes.Buffer
	err := oauthCallbackTemplate.Execute(&buf, auth.CallbackData{
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

	// Failure case
	buf.Reset()
	err = oauthCallbackTemplate.Execute(&buf, auth.CallbackData{
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
