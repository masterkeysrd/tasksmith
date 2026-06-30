package errors

import (
	"errors"
	"testing"
)

func TestErrors_E(t *testing.T) {
	// Simple builder with custom description using M helper
	err := E(Op("test.Op"), Code(ErrDatabase), M("sqlite disk full"))
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	appErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}

	if appErr.Op != "test.Op" {
		t.Errorf("expected Op test.Op, got %s", appErr.Op)
	}
	if appErr.Code != ErrDatabase {
		t.Errorf("expected Code ErrDatabase, got %s", appErr.Code)
	}
	if appErr.GetTitle() != "Database Failure" {
		t.Errorf("expected translated title, got %s", appErr.GetTitle())
	}
	if appErr.GetDescription() != "sqlite disk full" {
		t.Errorf("expected custom description, got %s", appErr.GetDescription())
	}
}

func TestErrors_Wrap(t *testing.T) {
	cause := errors.New("underlying socket error")
	// Wrapping using T and M helpers
	err := E(Op("api.Call"), Code(ErrLspStartFailed), T("LSP Failure"), M("Failed connection"), cause)

	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected errors.As to succeed")
	}

	if appErr.GetTitle() != "LSP Failure" {
		t.Errorf("expected title LSP Failure, got %s", appErr.GetTitle())
	}
	if appErr.GetDescription() != "Failed connection" {
		t.Errorf("expected description Failed connection, got %s", appErr.GetDescription())
	}
	if !errors.Is(appErr, cause) {
		t.Errorf("expected errors.Is(appErr, cause) to be true")
	}
}

func TestErrors_ChainOp(t *testing.T) {
	inner := E(Op("db.Insert"), Code(ErrDatabase), M("disk full"))
	outer := E(Op("api.CreateSession"), inner)

	expectedStr := "api.CreateSession: db.Insert: [ERR_DATABASE] disk full"
	if outer.Error() != expectedStr {
		t.Errorf("expected chain formatting %q, got %q", expectedStr, outer.Error())
	}
}
