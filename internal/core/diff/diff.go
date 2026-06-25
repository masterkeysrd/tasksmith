package diff

import (
	"fmt"
	"strings"
)

// Op represents the type of edit operation.
type Op int

const (
	OpEqual Op = iota
	OpDelete
	OpInsert
)

// Edit represents a single line edit operation.
type Edit struct {
	Op   Op
	Line string
}

// hunk represents a contiguous group of edit operations.
type hunk struct {
	aStart, aLen int
	bStart, bLen int
	edits        []Edit
}

// FormatUnified compares contentA and contentB and returns a unified diff string.
// If the contents are identical, it returns an empty string.
func FormatUnified(nameA, nameB string, contentA, contentB string) string {
	linesA := splitLines(contentA)
	linesB := splitLines(contentB)

	// If both are empty
	if len(linesA) == 0 && len(linesB) == 0 {
		return ""
	}

	edits := MyersDiff(linesA, linesB)
	if len(edits) == 0 {
		return ""
	}

	// Check if all edits are equal (identical contents)
	allEqual := true
	for _, e := range edits {
		if e.Op != OpEqual {
			allEqual = false
			break
		}
	}
	if allEqual {
		return ""
	}

	hunks := groupEdits(edits, 3)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", nameA)
	fmt.Fprintf(&sb, "+++ %s\n", nameB)

	for _, h := range hunks {
		var aLenStr, bLenStr string
		if h.aLen == 1 {
			aLenStr = fmt.Sprintf("%d", h.aStart)
		} else {
			aLenStr = fmt.Sprintf("%d,%d", h.aStart, h.aLen)
		}
		if h.bLen == 1 {
			bLenStr = fmt.Sprintf("%d", h.bStart)
		} else {
			bLenStr = fmt.Sprintf("%d,%d", h.bStart, h.bLen)
		}

		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", aLenStr, bLenStr)
		for _, e := range h.edits {
			switch e.Op {
			case OpEqual:
				sb.WriteString(" ")
				sb.WriteString(e.Line)
			case OpDelete:
				sb.WriteString("-")
				sb.WriteString(e.Line)
			case OpInsert:
				sb.WriteString("+")
				sb.WriteString(e.Line)
			}
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// MyersDiff implements the Myers diff algorithm to find the shortest edit script.
func MyersDiff(a, b []string) []Edit {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}

	max := n + m
	v := make([]int, 2*max+1)
	vOffset := max

	trace := make([][]int, 0, max+1)
	found := false
	var d int

	for d = 0; d <= max; d++ {
		vCopy := make([]int, len(v))
		copy(vCopy, v)
		trace = append(trace, vCopy)

		for k := -d; k <= d; k += 2 {
			var x int
			kIdx := k + vOffset

			if k == -d || (k != d && v[kIdx-1] < v[kIdx+1]) {
				x = v[kIdx+1]
			} else {
				x = v[kIdx-1] + 1
			}

			y := x - k

			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[kIdx] = x

			if x >= n && y >= m {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil
	}

	// Backtrack to find the path
	var path []struct{ x, y int }
	x, y := n, m
	for d >= 0 {
		k := x - y
		kIdx := k + vOffset
		vCurrent := trace[d]

		var prevK int
		if k == -d || (k != d && vCurrent[kIdx-1] < vCurrent[kIdx+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := vCurrent[prevK+vOffset]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			path = append(path, struct{ x, y int }{x, y})
			x--
			y--
		}

		if d > 0 {
			path = append(path, struct{ x, y int }{x, y})
		}
		x, y = prevX, prevY
		d--
	}

	// Reverse path
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	var edits []Edit
	currX, currY := 0, 0
	for _, pt := range path {
		if pt.x == currX+1 && pt.y == currY {
			edits = append(edits, Edit{Op: OpDelete, Line: a[currX]})
			currX++
		} else if pt.x == currX && pt.y == currY+1 {
			edits = append(edits, Edit{Op: OpInsert, Line: b[currY]})
			currY++
		} else if pt.x == currX+1 && pt.y == currY+1 {
			edits = append(edits, Edit{Op: OpEqual, Line: a[currX]})
			currX++
			currY++
		}
	}

	for currX < n && currY < m {
		edits = append(edits, Edit{Op: OpEqual, Line: a[currX]})
		currX++
		currY++
	}

	return edits
}

// groupEdits clusters edits into hunks with context lines.
func groupEdits(edits []Edit, context int) []hunk {
	var hunks []hunk
	n := len(edits)
	if n == 0 {
		return nil
	}

	lineA, lineB := 1, 1
	i := 0

	for i < n {
		for i < n && edits[i].Op == OpEqual {
			lineA++
			lineB++
			i++
		}
		if i == n {
			break
		}

		hunkStart := i
		preContextStart := hunkStart - context
		if preContextStart < 0 {
			preContextStart = 0
		}
		hunkAStart := lineA - (hunkStart - preContextStart)
		hunkBStart := lineB - (hunkStart - preContextStart)

		lastEditIdx := i
		for i < n {
			if edits[i].Op != OpEqual {
				lastEditIdx = i
			}
			if i-lastEditIdx > 2*context {
				break
			}
			i++
		}

		hunkEnd := lastEditIdx + 1 + context
		if hunkEnd > i {
			hunkEnd = i
		}
		if hunkEnd > n {
			hunkEnd = n
		}

		hunkEdits := edits[preContextStart:hunkEnd]

		var hunkALen, hunkBLen int
		for _, e := range hunkEdits {
			switch e.Op {
			case OpEqual:
				hunkALen++
				hunkBLen++
			case OpDelete:
				hunkALen++
			case OpInsert:
				hunkBLen++
			}
		}

		hunks = append(hunks, hunk{
			aStart: hunkAStart,
			aLen:   hunkALen,
			bStart: hunkBStart,
			bLen:   hunkBLen,
			edits:  hunkEdits,
		})

		for j := hunkStart; j < hunkEnd; j++ {
			switch edits[j].Op {
			case OpEqual:
				lineA++
				lineB++
			case OpDelete:
				lineA++
			case OpInsert:
				lineB++
			}
		}
		i = hunkEnd
	}

	return hunks
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Merge3 performs a line-based three-way merge.
// It merges the changes from B -> A (left) and B -> C (right) using B as the ancestor.
// Returns the merged lines and a boolean indicating if there was a conflict.
func Merge3(ancestor, left, right []string) ([]string, bool) {
	matchA := buildMatches(ancestor, left)
	matchC := buildMatches(ancestor, right)

	var result []string
	hasConflict := false

	prevA := -1
	prevC := -1

	i := 0
	n := len(ancestor)

	for i < n {
		if matchA[i] != -1 && matchC[i] != -1 {
			// Handle insertions before line i
			currA := matchA[i]
			currC := matchC[i]

			insA := left[prevA+1 : currA]
			insC := right[prevC+1 : currC]

			if len(insA) > 0 && len(insC) > 0 {
				if equalSlices(insA, insC) {
					result = append(result, insA...)
				} else {
					hasConflict = true
				}
			} else if len(insA) > 0 {
				result = append(result, insA...)
			} else if len(insC) > 0 {
				result = append(result, insC...)
			}

			// Keep the matched line
			result = append(result, ancestor[i])

			prevA = currA
			prevC = currC
			i++
		} else {
			// Find the end of the contiguous mismatch segment
			start := i
			for i < n && (matchA[i] == -1 || matchC[i] == -1) {
				i++
			}
			end := i

			// The next matched indices (or the end of the files)
			nextA := len(left)
			if end < n && matchA[end] != -1 {
				nextA = matchA[end]
			}
			nextC := len(right)
			if end < n && matchC[end] != -1 {
				nextC = matchC[end]
			}

			hunkAncestor := ancestor[start:end]
			hunkLeft := left[prevA+1 : nextA]
			hunkRight := right[prevC+1 : nextC]

			leftChanged := !equalSlices(hunkAncestor, hunkLeft)
			rightChanged := !equalSlices(hunkAncestor, hunkRight)

			if leftChanged && rightChanged {
				if equalSlices(hunkLeft, hunkRight) {
					result = append(result, hunkLeft...)
				} else {
					hasConflict = true
				}
			} else if leftChanged {
				result = append(result, hunkLeft...)
			} else if rightChanged {
				result = append(result, hunkRight...)
			} else {
				result = append(result, hunkAncestor...)
			}

			prevA = nextA - 1
			prevC = nextC - 1
		}
	}

	// Handle trailing insertions after the last matched line
	insA := left[prevA+1:]
	insC := right[prevC+1:]
	if len(insA) > 0 && len(insC) > 0 {
		if equalSlices(insA, insC) {
			result = append(result, insA...)
		} else {
			hasConflict = true
		}
	} else if len(insA) > 0 {
		result = append(result, insA...)
	} else if len(insC) > 0 {
		result = append(result, insC...)
	}

	return result, hasConflict
}

func buildMatches(src, dst []string) []int {
	edits := MyersDiff(src, dst)
	matches := make([]int, len(src))
	for i := range matches {
		matches[i] = -1
	}

	srcIdx := 0
	dstIdx := 0
	for _, e := range edits {
		switch e.Op {
		case OpEqual:
			matches[srcIdx] = dstIdx
			srcIdx++
			dstIdx++
		case OpDelete:
			srcIdx++
		case OpInsert:
			dstIdx++
		}
	}
	return matches
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
