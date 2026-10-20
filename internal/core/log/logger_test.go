package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func parseJSONLines(t *testing.T, data string) []map[string]any {
	t.Helper()
	rawLines := strings.Split(strings.TrimSpace(data), "\n")
	var records []map[string]any
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("failed to unmarshal JSONL line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

func TestDefaultSilentLogger(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	// Reset default logger after test.
	orig := Default()
	defer SetDefault(orig)

	SetDefault(nil)
	if Default() == nil {
		t.Fatal("expected Default() to never be nil")
	}

	// Ensure package-level functions do not panic and produce no stdout/stderr side effects.
	Debug("silent debug", String("k", "v"))
	Info("silent info", Int("n", 1))
	Warn("silent warn", Bool("ok", true))
	Error("silent error", Err(errors.New("boom")))
	comp := ForComponent("tui")
	comp.Info("component info")
	scoped := With(String("req", "123"))
	scoped.Info("scoped info")

	// Install a buffer-backed default logger and verify package-level helpers delegate to it.
	var buf bytes.Buffer
	SetDefault(New(&buf, LevelDebug))

	Debug("d-msg", String("step", "1"))
	Info("i-msg", String("step", "2"))
	Warn("w-msg", String("step", "3"))
	Error("e-msg", String("step", "4"))
	ForComponent("ipc").Info("comp-msg")
	With(String("session", "abc")).Info("with-msg")

	records := parseJSONLines(t, buf.String())
	if len(records) != 6 {
		t.Fatalf("expected 6 log records, got %d", len(records))
	}
	if records[0]["level"] != "DEBUG" || records[0]["msg"] != "d-msg" {
		t.Errorf("unexpected first record: %+v", records[0])
	}
	if records[4]["component"] != "ipc" || records[4]["msg"] != "comp-msg" {
		t.Errorf("unexpected component record: %+v", records[4])
	}
	if records[5]["session"] != "abc" || records[5]["msg"] != "with-msg" {
		t.Errorf("unexpected with record: %+v", records[5])
	}
}

func TestParseLevelAndFiltering(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	cases := []struct {
		input string
		want  Level
		str   string
	}{
		{"debug", LevelDebug, "DEBUG"},
		{"  DBG ", LevelDebug, "DEBUG"},
		{"info", LevelInfo, "INFO"},
		{"INFO", LevelInfo, "INFO"},
		{"warn", LevelWarn, "WARN"},
		{"WARNING", LevelWarn, "WARN"},
		{"error", LevelError, "ERROR"},
		{"ERR", LevelError, "ERROR"},
		{"unknown", LevelInfo, "INFO"},
		{"", LevelInfo, "INFO"},
	}

	for _, tc := range cases {
		got := ParseLevel(tc.input)
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
		if got.String() != tc.str {
			t.Errorf("Level(%v).String() = %q, want %q", got, got.String(), tc.str)
		}
	}

	// Verify level filtering at LevelWarn only emits WARN and ERROR.
	var buf bytes.Buffer
	logger := New(&buf, LevelWarn)
	logger.Debug("ignored debug")
	logger.Info("ignored info")
	logger.Warn("emitted warn")
	logger.Error("emitted error")

	records := parseJSONLines(t, buf.String())
	if len(records) != 2 {
		t.Fatalf("expected 2 records at LevelWarn, got %d", len(records))
	}
	if records[0]["level"] != "WARN" || records[1]["level"] != "ERROR" {
		t.Errorf("unexpected filtered levels: %+v", records)
	}
}

func TestAllAttrConstructorsAndScoping(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	var buf bytes.Buffer
	logger := New(&buf, LevelDebug)

	ts := time.Date(2026, 10, 20, 12, 30, 0, 0, time.UTC)
	dur := 250 * time.Millisecond
	testErr := errors.New("connection refused")

	scoped := logger.ForComponent("daemon").With(
		String("str_k", "hello"),
		Int("int_k", -42),
		Int64("int64_k", int64(9000000000)),
		Uint64("uint64_k", uint64(18000000000)),
		Float64("float64_k", 3.1415),
		Bool("bool_k", true),
	)

	scoped.Info("full attr test",
		Duration("dur_k", dur),
		Time("time_k", ts),
		Err(testErr),
		Any("map_k", map[string]string{"nested": "val"}),
		Any("nil_err", error(nil)),
		Attr{Key: "raw_attr", Value: "direct_literal"},
	)

	// Also test Err(nil) and empty With()
	scoped.With().Debug("nil err test", Err(nil))

	records := parseJSONLines(t, buf.String())
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	r := records[0]
	if r["component"] != "daemon" {
		t.Errorf("expected component=daemon, got %v", r["component"])
	}
	if r["str_k"] != "hello" {
		t.Errorf("expected str_k=hello, got %v", r["str_k"])
	}
	if r["int_k"] != float64(-42) {
		t.Errorf("expected int_k=-42, got %v", r["int_k"])
	}
	if r["int64_k"] != float64(9000000000) {
		t.Errorf("expected int64_k=9000000000, got %v", r["int64_k"])
	}
	if r["uint64_k"] != float64(18000000000) {
		t.Errorf("expected uint64_k=18000000000, got %v", r["uint64_k"])
	}
	if r["float64_k"] != 3.1415 {
		t.Errorf("expected float64_k=3.1415, got %v", r["float64_k"])
	}
	if r["bool_k"] != true {
		t.Errorf("expected bool_k=true, got %v", r["bool_k"])
	}
	if r["dur_k"] != float64(dur.Nanoseconds()) {
		t.Errorf("expected dur_k=%v, got %v", dur.Nanoseconds(), r["dur_k"])
	}
	if r["time_k"] != ts.Format(time.RFC3339Nano) {
		t.Errorf("expected time_k=%v, got %v", ts.Format(time.RFC3339Nano), r["time_k"])
	}
	if r["error"] != "connection refused" {
		t.Errorf("expected error='connection refused', got %v", r["error"])
	}
	if r["raw_attr"] != "direct_literal" {
		t.Errorf("expected raw_attr='direct_literal', got %v", r["raw_attr"])
	}
	nested, ok := r["map_k"].(map[string]any)
	if !ok || nested["nested"] != "val" {
		t.Errorf("expected nested map_k, got %v", r["map_k"])
	}

	if records[1]["error"] != nil {
		t.Errorf("expected nil error in second record, got %v", records[1]["error"])
	}
}

func TestRotatingWriterRotationAndMaxBackups(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	// When Dir is empty, RotatingWriter resolves xdg.LogsDir().
	logger, rw, err := NewRotating(RotatingWriterConfig{
		Stream:       "daemon",
		MaxSizeBytes: 180,
		MaxBackups:   3,
		MaxAge:       24 * time.Hour,
	}, LevelInfo)
	if err != nil {
		t.Fatalf("NewRotating failed: %v", err)
	}
	defer rw.Close()

	logsDir, err := xdg.LogsDir()
	if err != nil {
		t.Fatalf("xdg.LogsDir failed: %v", err)
	}
	expectedActive := filepath.Join(logsDir, "daemon.jsonl")
	if rw.Path() != expectedActive {
		t.Fatalf("rw.Path() = %q, want %q", rw.Path(), expectedActive)
	}

	// Write enough entries to trigger multiple rotations beyond MaxBackups (3).
	for i := 0; i < 12; i++ {
		logger.Info("rotating log entry", Int("iteration", i), String("padding", "0123456789abcdef"))
	}

	if err := rw.Sync(); err != nil {
		t.Fatalf("rw.Sync() failed: %v", err)
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) failed: %v", logsDir, err)
	}

	backupPattern := regexp.MustCompile(`^daemon-\d{8}-\d{6}\.\d{3}\.jsonl$`)
	var backups []string
	hasActive := false

	for _, e := range entries {
		name := e.Name()
		if name == "daemon.jsonl" {
			hasActive = true
			continue
		}
		if backupPattern.MatchString(name) {
			backups = append(backups, name)
		} else {
			t.Errorf("unexpected file in logs dir: %q", name)
		}
	}

	if !hasActive {
		t.Fatal("expected active file daemon.jsonl to exist")
	}
	if len(backups) != 3 {
		t.Fatalf("expected exactly MaxBackups=3 rotated backup files, got %d (%v)", len(backups), backups)
	}
}

func TestRotatingWriterMaxAgePruning(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Pre-create an expired backup file (10 days old) and a recent backup file (1 hour old),
	// plus an unrelated stream file that must not be touched.
	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour)
	recentTime := time.Now().UTC().Add(-1 * time.Hour)

	oldBackup := filepath.Join(logDir, fmt.Sprintf("client-%s.jsonl", oldTime.Format(backupTimeFormat)))
	recentBackup := filepath.Join(logDir, fmt.Sprintf("client-%s.jsonl", recentTime.Format(backupTimeFormat)))
	otherStream := filepath.Join(logDir, fmt.Sprintf("other-%s.jsonl", oldTime.Format(backupTimeFormat)))

	for _, p := range []string{oldBackup, recentBackup, otherStream} {
		if err := os.WriteFile(p, []byte("{\"msg\":\"backup\"}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) failed: %v", p, err)
		}
	}
	_ = os.Chtimes(oldBackup, oldTime, oldTime)
	_ = os.Chtimes(recentBackup, recentTime, recentTime)
	_ = os.Chtimes(otherStream, oldTime, oldTime)

	rw, err := NewRotatingWriter(RotatingWriterConfig{
		Dir:          logDir,
		Stream:       "client",
		MaxSizeBytes: 64,
		MaxBackups:   5,
		MaxAge:       7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer rw.Close()

	// First write populates active file (< 64 bytes); second write exceeds 64 bytes and triggers rotation + pruning.
	payload := []byte(strings.Repeat("x", 45) + "\n")
	if _, err := rw.Write(payload); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if _, err := rw.Write(payload); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Errorf("expected expired backup %q to be pruned, stat err = %v", oldBackup, err)
	}
	if _, err := os.Stat(recentBackup); err != nil {
		t.Errorf("expected recent backup %q to be retained, stat err = %v", recentBackup, err)
	}
	if _, err := os.Stat(otherStream); err != nil {
		t.Errorf("expected unrelated stream backup %q to be untouched, stat err = %v", otherStream, err)
	}
}

func TestRotatingWriterConcurrentWrites(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	logger, rw, err := NewRotating(RotatingWriterConfig{
		MaxSizeBytes: 512,
		MaxBackups:   4,
		MaxAge:       time.Hour,
	}, LevelDebug)
	if err != nil {
		t.Fatalf("NewRotating failed: %v", err)
	}

	const goroutines = 8
	const writesPerGoroutine = 25

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			compLogger := logger.ForComponent(fmt.Sprintf("worker-%d", id))
			for i := 0; i < writesPerGoroutine; i++ {
				compLogger.Info("concurrent log message", Int("worker", id), Int("seq", i))
			}
		}(g)
	}
	wg.Wait()

	if err := rw.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Writing after Close should return an error.
	if _, err := rw.Write([]byte("after close\n")); !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected os.ErrClosed when writing after Close, got %v", err)
	}
}
