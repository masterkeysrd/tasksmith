package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
)

// pathBreadth returns the number of path components in p, used to order rules
// from broadest (fewest components) to narrowest so that kernel profile
// resolution — last-match wins on Seatbelt, later-mount-overlays on
// Bubblewrap — lets a narrower rule override a broader one. The filesystem root
// is breadth 0 so it never ties with a top-level directory like /tmp.
func pathBreadth(p string) int {
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "/" {
		return 0
	}
	return strings.Count(p, "/")
}

// readRule is a single ordered file-read permission emitted by platform
// profile builders.
type readRule struct {
	Op   string // "allow" or "deny"
	Path string
}

// operationalPolicyRoots returns the sandbox's own operational roots — the
// work directory, the temp directory, and the extra write paths, each with its
// symlink-resolved variant — that a broader DeniedReadPaths mask must never
// blanket: a session that cannot read its own working directory or temp
// directory cannot run at all.
func operationalPolicyRoots(workDir, realWorkDir, tempDir, realTempDir string, extraWrite []string) []string {
	roots := []string{workDir, realWorkDir, tempDir, realTempDir}
	roots = append(roots, extraWrite...)
	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		r = filepath.Clean(strings.TrimSpace(r))
		if r == "" || r == "." {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// mergeReadRules merges denied and allowed read paths with implicit writable-
// root re-opens into a single list ordered from broadest to narrowest path so
// profile builders can emit rules in an order where the narrowest matching
// rule wins (Seatbelt last-match, later bwrap overlays). On equal path breadth,
// denies are emitted after allows so deny wins an exact-tie prefix.
func mergeReadRules(denied, allowed, reopens []string) []readRule {
	rules := make([]readRule, 0, len(denied)+len(allowed)+len(reopens))
	for _, p := range withCanonicalVariants(denied) {
		rules = append(rules, readRule{Op: "deny", Path: p})
	}
	for _, p := range withCanonicalVariants(append(slices.Clone(allowed), reopens...)) {
		rules = append(rules, readRule{Op: "allow", Path: p})
	}
	slices.SortStableFunc(rules, func(a, b readRule) int {
		if d := pathBreadth(a.Path) - pathBreadth(b.Path); d != 0 {
			return d
		}
		if a.Op != b.Op {
			if a.Op == "deny" {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return rules
}

// mountKind orders the operations emitted at one path in a Bubblewrap mount
// plan. Lower kinds are emitted first so a later operation overrides an
// earlier one at the same path: a read-only re-allow is overridden by a
// writable bind, a writable bind by an explicit read deny (tmpfs), and any of
// those by a protected-path read-only overlay.
type mountKind int

const (
	mountAllowRead mountKind = iota
	mountWritable
	mountDenyRead
	mountDenyWrite
)

// mountStep is one ordered Bubblewrap mount operation at a canonical path.
type mountStep struct {
	Kind mountKind
	Path string
}

// mountPlanInput carries the platform-neutral confinement inputs for
// [planMountSteps].
type mountPlanInput struct {
	WorkDir     string
	RealWorkDir string
	TempDir     string
	RealTempDir string
	ReadOnly    bool
	Filesystem  FilesystemPolicy
}

// planMountSteps assembles the full ordered Bubblewrap mount plan: a single
// breadth-sorted pass (broadest first) over read masks, read re-opens,
// implicit writable-root re-opens, writable binds, and protected-path
// read-only overlays, so the narrowest matching operation always lands last
// (bwrap overlays later mounts at the same or nested path) while writable
// roots (work dir, temp dir, extra write paths) are re-bound writable after
// any broader tmpfs mask and protected DeniedWritePaths overlays land after
// every writable bind that covers them. An explicit read deny at exactly a
// writable root wins over the implicit re-open.
func planMountSteps(params mountPlanInput) []mountStep {
	type op struct {
		kind mountKind
		path string
	}
	var ops []op

	add := func(kind mountKind, paths ...string) {
		for _, p := range paths {
			p = filepath.Clean(strings.TrimSpace(p))
			if p == "" || p == "." {
				continue
			}
			ops = append(ops, op{kind: kind, path: p})
		}
	}

	add(mountDenyRead, params.Filesystem.DeniedReadPaths...)
	add(mountAllowRead, params.Filesystem.AllowReadPaths...)

	roots := operationalPolicyRoots(params.WorkDir, params.RealWorkDir, params.TempDir, params.RealTempDir, params.Filesystem.ExtraWritePaths)
	if !params.ReadOnly {
		// Writable binds re-expose their contents for reading too, so they
		// double as the implicit read re-open for the sandbox's own roots.
		add(mountWritable, params.WorkDir, params.RealWorkDir, params.TempDir, params.RealTempDir)
		add(mountWritable, params.Filesystem.ExtraWritePaths...)
		add(mountDenyWrite, params.Filesystem.DeniedWritePaths...)
	} else {
		// A read-only session still has to read its own roots when a broad
		// denyRead masks them, so the work dir and extra write paths are
		// re-opened read-only (the temp dir stays writable in both modes).
		add(mountWritable, params.TempDir, params.RealTempDir)
		add(mountAllowRead, params.WorkDir, params.RealWorkDir)
		add(mountAllowRead, params.Filesystem.ExtraWritePaths...)
	}

	// Canonicalize every entry to both its logical and resolved variant so a
	// symlinked root or protected entry is overlaid at both paths.
	expanded := make([]op, 0, len(ops))
	seen := make(map[string]struct{}, len(ops)*2)
	for _, o := range ops {
		for _, variant := range []string{o.path, filepath.Clean(fsutils.ResolveDeepestExisting(o.path))} {
			if variant == "" {
				continue
			}
			key := fmt.Sprintf("%d\x00%s", o.kind, variant)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			expanded = append(expanded, op{kind: o.kind, path: variant})
		}
	}

	// bwrap can only mount over existing targets; the sandbox's own roots are
	// created by the caller and always bound even if a stat transiently fails.
	forced := map[string]struct{}{
		filepath.Clean(strings.TrimSpace(params.WorkDir)):     {},
		filepath.Clean(strings.TrimSpace(params.RealWorkDir)): {},
		filepath.Clean(strings.TrimSpace(params.TempDir)):     {},
		filepath.Clean(strings.TrimSpace(params.RealTempDir)): {},
	}
	candidates := make([]op, 0, len(expanded))
	for _, o := range expanded {
		if _, ok := forced[o.path]; ok {
			candidates = append(candidates, o)
			continue
		}
		if _, err := os.Stat(o.path); err != nil {
			continue
		}
		candidates = append(candidates, o)
	}

	slices.SortStableFunc(candidates, func(a, b op) int {
		if d := pathBreadth(a.path) - pathBreadth(b.path); d != 0 {
			return d
		}
		return int(a.kind) - int(b.kind)
	})

	// A mount re-exposes real host contents under any tmpfs mask, so writable
	// binds and protected read-only overlays must not land strictly inside a
	// read mask unless an operational root strictly between the mask and the
	// step re-opened that subtree. An explicit deny exactly at one of the
	// sandbox's own roots suppresses that root's implicit bind too — the user
	// denied that root outright.
	var masks []string
	for _, o := range candidates {
		if o.kind == mountDenyRead {
			masks = append(masks, o.path)
		}
	}
	steps := make([]mountStep, 0, len(candidates))
	for _, o := range candidates {
		if (o.kind == mountWritable || o.kind == mountDenyWrite) && insideAnyMask(o.path, masks, roots) {
			continue
		}
		steps = append(steps, mountStep{Kind: o.kind, Path: o.path})
	}
	return steps
}

// insideAnyMask reports whether p is strictly inside an active read mask that
// no operational root re-opened.
func insideAnyMask(p string, masks, roots []string) bool {
	clean := filepath.Clean(p)
	for _, m := range masks {
		mClean := filepath.Clean(m)
		if !fsutils.IsInsideDir(clean, mClean) {
			continue
		}
		if isInsideOperationalRoot(clean, mClean, roots) {
			continue
		}
		return true
	}
	return false
}

// isInsideOperationalRoot reports whether some path strictly between the mask
// and p was re-opened because it belongs to an operational root: the bind of
// that root already exposes the subtree, so a mount below it leaks nothing new
// (e.g. workdir re-bound under a "~" mask keeps workdir/.git/hooks overlays
// meaningful). A mask exactly at a root is an explicit user deny of that root
// itself and grants no exemption.
func isInsideOperationalRoot(clean, mask string, roots []string) bool {
	for _, r := range roots {
		rClean := filepath.Clean(r)
		if rClean == mask {
			continue
		}
		if fsutils.IsInsideDir(rClean, mask) && fsutils.IsInsideDir(clean, rClean) {
			return true
		}
	}
	return false
}

// withCanonicalVariants returns every path plus its deepest-existing-symlink
// resolved variant when different, so kernel rules anchor on both the logical
// path (e.g. /tmp/work/.git/hooks, which protects the symlink entry itself from
// being unlinked) and the canonical vnode path (e.g. the symlink target the
// kernel actually resolves on open).
func withCanonicalVariants(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths)*2)
	for _, p := range paths {
		p = filepath.Clean(p)
		if p == "." || p == "" {
			continue
		}
		for _, variant := range []string{p, filepath.Clean(fsutils.ResolveDeepestExisting(p))} {
			if variant == "" {
				continue
			}
			if _, dup := seen[variant]; dup {
				continue
			}
			seen[variant] = struct{}{}
			out = append(out, variant)
		}
	}
	return out
}
