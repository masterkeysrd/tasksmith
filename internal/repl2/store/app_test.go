package store

import (
	"testing"
)

func TestStoreApp(t *testing.T) {
	ResetApp()
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}

	SetCtrlCPending(true)
	if !GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be true after SetCtrlCPending(true)")
	}

	SetCtrlCPending(false)
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after SetCtrlCPending(false)")
	}

	SetCtrlCPending(true)
	ResetApp()
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}
}
