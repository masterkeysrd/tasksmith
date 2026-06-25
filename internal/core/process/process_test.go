package process

import (
	"os"
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

func TestKillSafety(t *testing.T) {
	// 1. Nil cmd or nil Process
	if err := Kill(nil); err != nil {
		t.Errorf("Kill(nil) returned error: %v", err)
	}
	var emptyCmd exec.Cmd
	if err := Kill(&emptyCmd); err != nil {
		t.Errorf("Kill(emptyCmd) returned error: %v", err)
	}

	// 2. Cmd with Pid <= 0
	cmdWithZeroPid := &exec.Cmd{
		Process: &os.Process{Pid: 0},
	}
	err := Kill(cmdWithZeroPid)
	if err == nil {
		t.Errorf("expected error when killing cmd with Pid=0, got nil")
	}

	cmdWithNegPid := &exec.Cmd{
		Process: &os.Process{Pid: -1},
	}
	err = Kill(cmdWithNegPid)
	if err == nil {
		t.Errorf("expected error when killing cmd with Pid=-1, got nil")
	}

	// 3. Cmd with self PID (or sharing the same PGID)
	cmdWithSelfPid := &exec.Cmd{
		Process: &os.Process{Pid: os.Getpid()},
	}
	err = Kill(cmdWithSelfPid)
	if err == nil {
		t.Errorf("expected error when killing cmd with self PID, got nil")
	}
}
