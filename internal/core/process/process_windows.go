//go:build windows

package process

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Prepare configures command execution properties for Windows.
func Prepare(cmd *exec.Cmd) {
	// Standard Windows process preparation
}

// Kill terminates the process on Windows.
func Kill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// FindPorts returns all active listening TCP ports owned by targetPid or its child processes on Windows.
func FindPorts(targetPid int) ([]int, error) {
	if targetPid <= 0 {
		return nil, nil
	}

	// Recursively collect descendant PIDs
	pidsToMatch := make(map[int]bool)
	pidsToMatch[targetPid] = true

	pidsToQuery := []int{targetPid}
	for len(pidsToQuery) > 0 {
		nextPid := pidsToQuery[0]
		pidsToQuery = pidsToQuery[1:]

		cmd := exec.Command("wmic", "process", "where", fmt.Sprintf("ParentProcessId=%d", nextPid), "get", "ProcessId")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			scanner := bufio.NewScanner(&out)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.EqualFold(line, "ProcessId") {
					continue
				}
				if childPid, err := strconv.Atoi(line); err == nil {
					if !pidsToMatch[childPid] {
						pidsToMatch[childPid] = true
						pidsToQuery = append(pidsToQuery, childPid)
					}
				}
			}
		}
	}

	// Run netstat to retrieve active listening connections
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, nil
	}

	var ports []int
	seenPorts := make(map[int]bool)

	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, "LISTENING") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		localAddr := fields[1]
		pidStr := fields[len(fields)-1]

		pidVal, err := strconv.Atoi(pidStr)
		if err != nil || !pidsToMatch[pidVal] {
			continue
		}

		lastColon := strings.LastIndex(localAddr, ":")
		if lastColon != -1 && lastColon < len(localAddr)-1 {
			portStr := localAddr[lastColon+1:]
			if port, err := strconv.Atoi(portStr); err == nil {
				if !seenPorts[port] {
					seenPorts[port] = true
					ports = append(ports, port)
				}
			}
		}
	}

	return ports, nil
}
