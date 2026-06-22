package process

import (
	"os/exec"
	"testing"
	"time"
)

func TestFindPorts(t *testing.T) {
	// Start netcat listener in the background on port 64399
	cmd := exec.Command("nc", "-l", "64399")
	Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip("Skipping test: nc command not available or failed to start")
		return
	}
	defer Kill(cmd)

	// Wait for netcat to bind to the socket
	time.Sleep(100 * time.Millisecond)

	ports, err := FindPorts(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("FindPorts failed: %v", err)
	}

	found := false
	for _, p := range ports {
		if p == 64399 {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to find port 64399 in ports list %v", ports)
	}
}
