//go:build !windows

package tools

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// getHardLinkCount returns the number of hard links for a file using syscall.Stat_t.
// Falls back to 1 on platforms where Sys() does not return *syscall.Stat_t.
func getHardLinkCount(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 1
}

// getFileOwnerGroup returns the owner username and group name for a file.
// Falls back to numeric UID/GID strings if name lookup fails.
func getFileOwnerGroup(info os.FileInfo) (string, string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "unknown", "unknown"
	}

	ownerName := strconv.Itoa(int(stat.Uid))
	if u, err := user.LookupId(ownerName); err == nil {
		ownerName = u.Username
	}

	groupName := strconv.Itoa(int(stat.Gid))
	if g, err := user.LookupGroupId(groupName); err == nil {
		groupName = g.Name
	}

	return ownerName, groupName
}
