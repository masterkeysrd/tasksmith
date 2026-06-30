package toast

import (
	"errors"
	"testing"
	"time"
)

func TestToastStore_Basic(t *testing.T) {
	ClearAll()

	// Initial store should be empty
	toasts := GetToasts()
	if len(toasts) != 0 {
		t.Fatalf("expected empty store, got %d toasts", len(toasts))
	}

	// Add Success toast
	id1 := AddSuccess("Build OK", "Project compiled successfully")
	if id1 == "" {
		t.Fatal("expected non-empty toast ID")
	}

	toasts = GetToasts()
	if len(toasts) != 1 {
		t.Fatalf("expected 1 toast, got %d", len(toasts))
	}
	if toasts[0].Title != "Build OK" || toasts[0].Severity != Success {
		t.Errorf("incorrect toast details: %+v", toasts[0])
	}

	// Add Error toast
	id2 := AddError(errors.New("connection failed"))
	if id2 == "" {
		t.Fatal("expected non-empty toast ID for error")
	}

	toasts = GetToasts()
	if len(toasts) != 2 {
		t.Fatalf("expected 2 toasts, got %d", len(toasts))
	}

	// Dismiss first toast
	Dismiss(id1)
	toasts = GetToasts()
	if len(toasts) != 1 {
		t.Fatalf("expected 1 toast after dismissal, got %d", len(toasts))
	}
	if toasts[0].ID != id2 {
		t.Errorf("expected remaining toast to be ID %s, got %s", id2, toasts[0].ID)
	}

	// Clear all
	ClearAll()
	toasts = GetToasts()
	if len(toasts) != 0 {
		t.Fatalf("expected 0 toasts after ClearAll, got %d", len(toasts))
	}
}

func TestToastStore_MaxLimit(t *testing.T) {
	ClearAll()

	// Add 12 toasts (limit is 10)
	for i := 1; i <= 12; i++ {
		AddInfo("Title", "Message")
	}

	toasts := GetToasts()
	if len(toasts) != 10 {
		t.Errorf("expected store to cap at 10 toasts, got %d", len(toasts))
	}
}

func TestToastStore_HelperMethods(t *testing.T) {
	ClearAll()

	AddInfo("Info Title", "Info Msg")
	AddWarning("Warn Title", "Warn Msg")
	AddErrorMessage("Error Title", "Error Msg")

	toasts := GetToasts()
	if len(toasts) != 3 {
		t.Fatalf("expected 3 toasts, got %d", len(toasts))
	}

	if toasts[0].Severity != Info || toasts[0].Title != "Info Title" {
		t.Errorf("first toast incorrect: %+v", toasts[0])
	}
	if toasts[1].Severity != Warning || toasts[1].Title != "Warn Title" {
		t.Errorf("second toast incorrect: %+v", toasts[1])
	}
	if toasts[2].Severity != Error || toasts[2].Title != "Error Title" {
		t.Errorf("third toast incorrect: %+v", toasts[2])
	}

	// Add nil error (should be a no-op)
	id := AddError(nil)
	if id != "" {
		t.Errorf("expected AddError(nil) to return empty string, got %s", id)
	}
}

func TestToastStore_AutoDismissDuration(t *testing.T) {
	ClearAll()

	id := Add(Info, "Title", "Message", -1) // negative duration
	toasts := GetToasts()
	if len(toasts) != 1 {
		t.Fatalf("expected 1 toast, got %d", len(toasts))
	}
	if toasts[0].Duration != 5*time.Second {
		t.Errorf("expected default 5s duration for negative input, got %s", toasts[0].Duration)
	}

	Dismiss(id)
}
