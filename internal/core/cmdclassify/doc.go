// Package cmdclassify provides command safety classification and permission matching.
//
// It uses the parsed commands from cmdparse to evaluate command safety profiles
// (static signature checks and dynamic redirection checks) and matches them
// against user-defined permission grants (e.g. "git *" or "git commit").
//
// Safety Categories:
//   - ReadOnly: Commands that do not modify the filesystem (e.g. cat, grep, ls).
//   - SafeWrite: Commands that write strictly inside the workspace (e.g. mkdir, touch).
//   - UnsafeWrite: Commands that write or redirect outside the workspace (e.g. > /etc/hosts).
//   - Destructive: Commands that can cause data loss (e.g. rm -rf, git reset --hard).
//   - Unknown: Custom runtimes or scripts with unknown behavior (e.g. ./run.sh).
package cmdclassify
