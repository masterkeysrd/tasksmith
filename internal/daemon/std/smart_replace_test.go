package std

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type editFixture struct {
	Name         string `json:"name"`
	Before       string `json:"before"`
	Old          string `json:"old"`
	New          string `json:"new"`
	After        string `json:"after"`
	ReplaceAll   bool   `json:"replace_all"`
	Replacements int    `json:"replacements"`
	Error        string `json:"error"`
}

type editScore struct {
	correct, missed, unsafe, refused, wrongRefusal int
}

func (s *editScore) record(edit editFixture, wantErr error, output string, count int, err error) {
	switch {
	case err == nil && wantErr == nil && output == edit.After && count == edit.Replacements:
		s.correct++
	case err == nil || output != edit.Before:
		s.unsafe++
	case wantErr == nil:
		s.missed++
	case errors.Is(err, wantErr):
		s.refused++
	default:
		s.wrongRefusal++
	}
}

func exactEditBaseline(edit editFixture) (string, int, error) {
	count := strings.Count(edit.Before, edit.Old)
	if count == 0 {
		return edit.Before, 0, ErrTargetNotFound
	}
	if count > 1 && !edit.ReplaceAll {
		return edit.Before, 0, ErrAmbiguousEdit
	}
	return strings.Replace(edit.Before, edit.Old, edit.New, count), count, nil
}

func TestSmartReplaceCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/smart_replace.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []editFixture
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var baseline, cascade editScore
	for _, edit := range cases {
		t.Run(edit.Name, func(t *testing.T) {
			var wantErr error
			want := edit.After
			switch edit.Error {
			case "not_found":
				wantErr = ErrTargetNotFound
			case "ambiguous":
				wantErr = ErrAmbiguousEdit
			case "":
				if edit.Replacements < 1 {
					t.Fatal("successful fixture must specify its replacement count")
				}
			default:
				t.Fatalf("unknown fixture error %q", edit.Error)
			}
			if wantErr != nil {
				want = edit.Before
			}
			bText, bCount, bErr := exactEditBaseline(edit)
			baseline.record(edit, wantErr, bText, bCount, bErr)

			s, tracker, dir := newTestStd(t, false)
			ctx := t.Context()
			const name = "fixture.txt"
			file := filepath.Join(dir, name)
			if err := os.WriteFile(file, []byte(edit.Before), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := s.View(ctx, ViewRequest{Path: name}); err != nil {
				t.Fatal(err)
			}
			originalHash, _ := tracker.LastHash(name)
			req := EditRequest{Path: name, OldText: edit.Old, NewText: edit.New, ReplaceAll: edit.ReplaceAll, DryRun: true}
			preview, previewErr := s.Edit(ctx, req)
			if !errors.Is(previewErr, wantErr) {
				t.Errorf("preview error = %v, want %v", previewErr, wantErr)
			}
			if currentHash, _ := tracker.LastHash(name); currentHash != originalHash || readSmartEditFile(t, file) != edit.Before {
				t.Fatal("preview modified the file or tracker")
			}

			req.DryRun = false
			result, err := s.Edit(ctx, req)
			got := readSmartEditFile(t, file)
			count := 0
			if result != nil {
				count = result.Replacements
			}
			cascade.record(edit, wantErr, got, count, err)
			if !errors.Is(err, wantErr) || got != want || count != edit.Replacements {
				t.Fatalf("got (%q, %d, %v), want (%q, %d, %v)", got, count, err, want, edit.Replacements, wantErr)
			}
			if err == nil && (preview == nil || !preview.DryRun || result.DryRun || preview.Diff != result.Diff ||
				preview.AfterHash != result.AfterHash || preview.BeforeHash != originalHash ||
				result.BeforeHash != originalHash || preview.Replacements != count) {
				t.Fatalf("preview and execution disagree: preview=%+v execution=%+v", preview, result)
			}
		})
	}
	for _, row := range []struct {
		name  string
		score editScore
	}{{"Exact-only", baseline}, {"SmartReplace", cascade}} {
		s := row.score
		t.Logf("%s (%d cases): %d correct edits, %d misses, %d unsafe edits, %d correct refusals, %d wrong refusal reasons",
			row.name, len(cases), s.correct, s.missed, s.unsafe, s.refused, s.wrongRefusal)
	}
}

func TestEditFallbackCAS(t *testing.T) {
	s, tracker, dir := newTestStd(t, false)
	ctx := t.Context()
	const name = "fixture.txt"
	file := filepath.Join(dir, name)
	before := "start\r\n  path: old\t\r\nend\r\n"
	if err := os.WriteFile(file, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	req := EditRequest{Path: name, OldText: "  path: old\n", NewText: "  path: new\n", DryRun: true}
	if _, err := s.Edit(ctx, req); !errors.Is(err, ErrFileNotRead) {
		t.Fatalf("unread fallback preview error = %v, want ErrFileNotRead", err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: name}); err != nil {
		t.Fatal(err)
	}
	preview, err := s.Edit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(before, "\r\n", "\n")
	if err := os.WriteFile(file, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	req.DryRun = false
	if _, err := s.Edit(ctx, req); !errors.Is(err, ErrFileModified) {
		t.Fatalf("fallback after line-ending mutation error = %v, want ErrFileModified", err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: name}); err != nil {
		t.Fatal(err)
	}
	req.ExpectedHash = preview.BeforeHash
	if _, err := s.Edit(ctx, req); !errors.Is(err, ErrFileModified) {
		t.Fatalf("fallback with stale expected hash error = %v, want ErrFileModified", err)
	}
	if got := readSmartEditFile(t, file); got != changed {
		t.Fatalf("CAS failure modified the file: %q", got)
	}
	req.ExpectedHash, _ = tracker.LastHash(name)
	if _, err := s.Edit(ctx, req); err != nil {
		t.Fatal(err)
	}
	if got, want := readSmartEditFile(t, file), "start\n  path: new\nend\n"; got != want {
		t.Fatalf("after re-read got %q, want %q", got, want)
	}
}

func readSmartEditFile(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
