//go:build !windows

package process

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Prepare configures the command to run in its own process group on Unix.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill terminates the entire process group associated with the command on Unix.
func Kill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Use negative PID to target the process group
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// FindPorts returns all active listening TCP ports owned by any process in the process group of targetPid.
func FindPorts(targetPid int) ([]int, error) {
	if targetPid <= 0 {
		return nil, nil
	}

	targetPgid, err := syscall.Getpgid(targetPid)
	if err != nil {
		return nil, fmt.Errorf("failed to get pgid for pid %d: %w", targetPid, err)
	}

	// lsof -iTCP -sTCP:LISTEN -P -n -F pPn list details in machine parseable format:
	// p[pid]
	// f[fd]
	// PTCP
	// n[name] -> e.g. *:8080 or 127.0.0.1:9000
	cmd := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-P", "-n", "-F", "pPn")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// If lsof exits with non-zero (e.g. no open files/listening ports matching query), return empty list
		return nil, nil
	}

	var ports []int
	seenPorts := make(map[int]bool)

	scanner := bufio.NewScanner(&stdout)
	var currentPid int
	var inTargetGroup bool

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}
		prefix := line[0]
		val := line[1:]

		switch prefix {
		case 'p':
			pidVal, err := strconv.Atoi(val)
			if err != nil {
				inTargetGroup = false
				continue
			}
			currentPid = pidVal
			pgid, err := syscall.Getpgid(currentPid)
			inTargetGroup = (err == nil && pgid == targetPgid)

		case 'n':
			if !inTargetGroup {
				continue
			}
			// Extract port from name field (last colon-delimited value)
			lastColon := strings.LastIndex(val, ":")
			if lastColon != -1 && lastColon < len(val)-1 {
				portStr := val[lastColon+1:]
				if port, err := strconv.Atoi(portStr); err == nil {
					if !seenPorts[port] {
						seenPorts[port] = true
						ports = append(ports, port)
					}
				}
			}
		}
	}

	return ports, nil
}
