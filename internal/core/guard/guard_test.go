package guard

import "testing"

func TestActionTypeAndSafetyLevel(t *testing.T) {
	actions := []struct {
		action ActionType
		str    string
		sev    int
	}{
		{ActionRead, "read", 0},
		{ActionUnknown, "unknown", 1},
		{ActionNet, "net", 2},
		{ActionWrite, "write", 3},
		{ActionExec, "exec", 4},
		{ActionDelete, "delete", 5},
		{ActionType("invalid"), "unknown", 1},
	}

	for _, tc := range actions {
		if got := tc.action.String(); got != tc.str {
			t.Errorf("ActionType(%q).String() = %q, want %q", tc.action, got, tc.str)
		}
		if got := tc.action.Severity(); got != tc.sev {
			t.Errorf("ActionType(%q).Severity() = %d, want %d", tc.action, got, tc.sev)
		}
	}

	safetys := []struct {
		safety SafetyLevel
		str    string
		sev    int
	}{
		{SafetySafe, "safe", 0},
		{SafetyOutside, "outside", 1},
		{SafetyUnknown, "unknown", 2},
		{SafetyUnsafe, "unsafe", 3},
		{SafetyLevel("invalid"), "unknown", 2},
	}

	for _, tc := range safetys {
		if got := tc.safety.String(); got != tc.str {
			t.Errorf("SafetyLevel(%q).String() = %q, want %q", tc.safety, got, tc.str)
		}
		if got := tc.safety.Severity(); got != tc.sev {
			t.Errorf("SafetyLevel(%q).Severity() = %d, want %d", tc.safety, got, tc.sev)
		}
	}
}

func TestIsProtectedPath(t *testing.T) {
	ws := "/tmp/workspace"
	cases := []struct {
		path string
		want bool
	}{
		{".git", true},
		{".git/config", true},
		{"sub/.git/HEAD", true},
		{".bare", true},
		{".bare/HEAD", true},
		{".agents", true},
		{".agents/rules.md", true},
		{"/tmp/workspace/.git/config", true},
		{"/tmp/workspace/.agents/skills/a.md", true},
		{".github/workflows/ci.yml", false},
		{".gitignore", false},
		{"internal/main.go", false},
		{"/tmp/workspace/internal/main.go", false},
	}

	for _, tc := range cases {
		if got := IsProtectedPath(tc.path, ws); got != tc.want {
			t.Errorf("IsProtectedPath(%q, %q) = %v, want %v", tc.path, ws, got, tc.want)
		}
	}

	if !IsProtectedPath(".vault/key", ws, ".vault") {
		t.Error("expected .vault/key to be protected with custom protectedDirs")
	}
	if IsProtectedPath(".git/config", ws, ".vault") {
		t.Error("expected .git/config not to be protected when overridden by custom protectedDirs")
	}
}

func TestIsSensitivePathAndEvaluatePath(t *testing.T) {
	ws := "/tmp/workspace"
	home := "/Users/tester"

	sensitiveCases := []struct {
		path string
		want bool
	}{
		{"/etc/hosts", true},
		{"/etc/passwd", true},
		{"/private/etc/hosts", true},
		{"/proc/self/environ", true},
		{"/dev/sda", true},
		{"~/.ssh/id_rsa", true},
		{"/Users/tester/.ssh/id_rsa", true},
		{"/Users/tester/.aws/credentials", true},
		{"/tmp/somefile.txt", false},
		{"/tmp/workspace/file.txt", false},
	}

	for _, tc := range sensitiveCases {
		if got := IsSensitivePath(tc.path, home); got != tc.want {
			t.Errorf("IsSensitivePath(%q, %q) = %v, want %v", tc.path, home, got, tc.want)
		}
	}

	evalCases := []struct {
		target     string
		action     ActionType
		wantPath   string
		wantSafety SafetyLevel
	}{
		{"file.txt", ActionRead, "/tmp/workspace/file.txt", SafetySafe},
		{"file.txt", ActionWrite, "/tmp/workspace/file.txt", SafetySafe},
		{".git/config", ActionRead, "/tmp/workspace/.git/config", SafetySafe},
		{".git/config", ActionWrite, "/tmp/workspace/.git/config", SafetyUnsafe},
		{"../somefile.txt", ActionRead, "/tmp/somefile.txt", SafetyOutside},
		{"../somefile.txt", ActionWrite, "/tmp/somefile.txt", SafetyUnsafe},
		{"/etc/hosts", ActionRead, "/etc/hosts", SafetyUnsafe},
		{"~/.ssh/id_rsa", ActionRead, "/Users/tester/.ssh/id_rsa", SafetyUnsafe},
		{"~", ActionRead, "/Users/tester", SafetyOutside},
	}

	for _, tc := range evalCases {
		gotPath, gotSafety := EvaluatePath(tc.target, ws, home, tc.action)
		if gotPath != tc.wantPath || gotSafety != tc.wantSafety {
			t.Errorf("EvaluatePath(%q, %s) = (%q, %s), want (%q, %s)",
				tc.target, tc.action, gotPath, gotSafety, tc.wantPath, tc.wantSafety)
		}
	}
}
