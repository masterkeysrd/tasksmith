package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathBreadthRootIsNarrowerTiedWithTopLevelDirs(t *testing.T) {
	if pathBreadth("/") != 0 {
		t.Fatalf("root must have breadth 0, got %d", pathBreadth("/"))
	}
	if pathBreadth("/tmp") != 1 {
		t.Fatalf("/tmp must have breadth 1, got %d", pathBreadth("/tmp"))
	}
	if pathBreadth("/") >= pathBreadth("/tmp") {
		t.Fatalf("root must be strictly broader than a top-level directory")
	}
}

func TestMergeReadRulesRootDenyDoesNotDefeatTmpAllow(t *testing.T) {
	rules := mergeReadRules([]string{"/"}, []string{"/tmp"}, nil)
	if len(rules) < 2 {
		t.Fatalf("expected at least 2 rules, got %+v", rules)
	}
	if rules[0].Op != "deny" || rules[0].Path != "/" {
		t.Fatalf("root deny must sort first, got %+v", rules[0])
	}
	for _, r := range rules[1:] {
		if r.Op != "allow" {
			t.Fatalf("no deny may follow the root deny here, got %+v", r)
		}
	}
	if rules[1].Path != "/tmp" {
		t.Fatalf("narrower /tmp allow must sort immediately after the root deny, got %+v", rules[1])
	}
}

func writePlannerTree(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func stepIndex(steps []mountStep, kind mountKind, path string) int {
	for i, s := range steps {
		if s.Kind == kind && s.Path == filepath.Clean(path) {
			return i
		}
	}
	return -1
}

func TestPlanMountStepsReopensWritableRootUnderBroadDeny(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "project")
	protected := filepath.Join(work, ".git", "hooks")
	leaky := filepath.Join(home, "other", "nested")
	writePlannerTree(t, work, protected, filepath.Join(home, "other", "nested"))

	steps := planMountSteps(mountPlanInput{
		WorkDir: work, RealWorkDir: work,
		TempDir: filepath.Join(home, "tmp"), RealTempDir: filepath.Join(home, "tmp"),
		Filesystem: FilesystemPolicy{
			DeniedReadPaths:  []string{home},
			DeniedWritePaths: []string{protected, leaky},
		},
	})

	maskIdx := stepIndex(steps, mountDenyRead, home)
	workIdx := stepIndex(steps, mountWritable, work)
	protectedIdx := stepIndex(steps, mountDenyWrite, protected)
	if maskIdx < 0 || workIdx < 0 || protectedIdx < 0 {
		t.Fatalf("expected mask, writable work re-open, and protected overlay, got %+v", steps)
	}
	if workIdx <= maskIdx {
		t.Fatalf("work dir must be re-bound after the tmpfs mask covering it: %+v", steps)
	}
	if protectedIdx <= workIdx {
		t.Fatalf("protected overlay must land after the writable work bind: %+v", steps)
	}
	if leakIdx := stepIndex(steps, mountDenyWrite, leaky); leakIdx >= 0 {
		t.Fatalf("overlay inside a masked subtree with no re-opened root must be dropped: %+v", steps)
	}
}

func TestPlanMountStepsReadOnlyReopensWorkRead(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "project")
	temp := filepath.Join(home, "tmp")
	writePlannerTree(t, work, temp)

	steps := planMountSteps(mountPlanInput{
		WorkDir: work, RealWorkDir: work,
		TempDir: temp, RealTempDir: temp,
		ReadOnly:   true,
		Filesystem: FilesystemPolicy{DeniedReadPaths: []string{home}},
	})

	maskIdx := stepIndex(steps, mountDenyRead, home)
	readIdx := stepIndex(steps, mountAllowRead, work)
	tempIdx := stepIndex(steps, mountWritable, temp)
	if maskIdx < 0 || readIdx < 0 || tempIdx < 0 {
		t.Fatalf("expected mask, read-only work re-open, and writable temp bind, got %+v", steps)
	}
	if readIdx <= maskIdx || tempIdx <= maskIdx {
		t.Fatalf("read-only session must re-open work and temp after the mask: %+v", steps)
	}
	if writableIdx := stepIndex(steps, mountWritable, work); writableIdx >= 0 {
		t.Fatalf("read-only session must not bind the work dir writable: %+v", steps)
	}
}

func TestPlanMountStepsExplicitRootDenyWins(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "project")
	writePlannerTree(t, work)

	steps := planMountSteps(mountPlanInput{
		WorkDir: work, RealWorkDir: work,
		TempDir: filepath.Join(home, "tmp"), RealTempDir: filepath.Join(home, "tmp"),
		Filesystem: FilesystemPolicy{DeniedReadPaths: []string{work}},
	})

	if workIdx := stepIndex(steps, mountWritable, work); workIdx >= 0 {
		t.Fatalf("explicit denyRead at exactly the work dir must suppress the implicit writable bind: %+v", steps)
	}
	if maskIdx := stepIndex(steps, mountDenyRead, work); maskIdx < 0 {
		t.Fatalf("explicit work dir deny must remain in the plan: %+v", steps)
	}
}
