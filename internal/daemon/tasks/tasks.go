package tasks

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
)

var (
	ErrTaskNotFound = errors.New("tasks: task not found")
	ErrNotRunning   = errors.New("tasks: task is not running")
)

const (
	MaxLogDiskBytes = 16 * 1024 * 1024 // 16 MB cap on task log file
	MaxLogMemBytes  = 64 * 1024        // 64 KB rolling window for in-memory buffer
)

// Func is the execution workload of a Task.
// ctx is the task's execution context; output receives combined stdout/stderr; stdin supplies input.
type Func func(ctx context.Context, output io.Writer, stdin io.Reader) (exitCode int, err error)

type Task struct {
	ID      string
	Kind    string
	Title   string
	Command string
	Wait    time.Duration
	Fn      Func

	mu           sync.Mutex
	status       api.TaskStatus
	exitCode     int
	err          error
	startedAt    time.Time
	finishedAt   time.Time
	logPath      string
	logFile      *os.File
	writtenBytes int64
	buf          bytes.Buffer
	stdinR       *os.File
	stdinW       *os.File
	cancel       context.CancelFunc
	done         chan struct{}
	backgrounded bool
	canceled     bool
	subscribers  map[chan string]struct{}

	bgCtx      context.Context
	outputsDir string
	onUpdate   func(api.Task)
}

func (t *Task) Snapshot() api.Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *Task) snapshotLocked() api.Task {
	var durMs int64
	if !t.startedAt.IsZero() {
		if !t.finishedAt.IsZero() {
			durMs = t.finishedAt.Sub(t.startedAt).Milliseconds()
		} else {
			durMs = time.Since(t.startedAt).Milliseconds()
		}
	}
	var errStr string
	if t.err != nil {
		errStr = t.err.Error()
	}
	status := t.status
	if status == "" {
		status = api.TaskStatusPending
	}
	return api.Task{
		ID:           t.ID,
		Kind:         t.Kind,
		Title:        t.Title,
		Command:      t.Command,
		Status:       status,
		ExitCode:     t.exitCode,
		Error:        errStr,
		LogPath:      t.logPath,
		StartedAt:    t.startedAt,
		FinishedAt:   t.finishedAt,
		DurationMs:   durMs,
		Backgrounded: t.backgrounded,
	}
}

func (t *Task) Status() api.TaskStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status == "" {
		return api.TaskStatusPending
	}
	return t.status
}

func (t *Task) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode
}

func (t *Task) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *Task) Duration() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.startedAt.IsZero() {
		return 0
	}
	if !t.finishedAt.IsZero() {
		return t.finishedAt.Sub(t.startedAt)
	}
	return time.Since(t.startedAt)
}

func (t *Task) LogPath() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.logPath
}

func (t *Task) Backgrounded() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.backgrounded
}

func (t *Task) Done() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done == nil {
		t.done = make(chan struct{})
	}
	return t.done
}

func (t *Task) Cancel() {
	t.mu.Lock()
	t.canceled = true
	if t.stdinW != nil {
		_ = t.stdinW.Close()
		t.stdinW = nil
	}
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (t *Task) Kill() error {
	if st := t.Status(); st != api.TaskStatusRunning && st != api.TaskStatusPending {
		return ErrNotRunning
	}
	t.Cancel()
	return nil
}

func (t *Task) WriteInput(data string) error {
	t.mu.Lock()
	if t.status != api.TaskStatusRunning || t.stdinW == nil {
		t.mu.Unlock()
		return ErrNotRunning
	}
	w := t.stdinW
	t.mu.Unlock()

	_, err := io.WriteString(w, data)
	return err
}

func (t *Task) Tail(maxLines int) (string, error) {
	if maxLines <= 0 {
		maxLines = 100
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var data []byte
	if t.logPath != "" {
		if t.logFile != nil {
			_ = t.logFile.Sync()
		}
		if b, err := os.ReadFile(t.logPath); err == nil {
			data = b
		} else {
			data = t.buf.Bytes()
		}
	} else {
		data = t.buf.Bytes()
	}

	return tailString(string(data), maxLines), nil
}

func tailString(s string, maxLines int) string {
	if s == "" || maxLines <= 0 {
		return s
	}
	hasTrailingNewline := strings.HasSuffix(s, "\n")
	trimmed := strings.TrimSuffix(s, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return s
	}
	selected := lines[len(lines)-maxLines:]
	res := strings.Join(selected, "\n")
	if hasTrailingNewline {
		res += "\n"
	}
	return res
}

func (t *Task) SubscribeOutput() (<-chan string, string, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.subscribers == nil {
		t.subscribers = make(map[chan string]struct{})
	}

	var initial string
	if t.logPath != "" {
		if t.logFile != nil {
			_ = t.logFile.Sync()
		}
		if b, err := os.ReadFile(t.logPath); err == nil {
			initial = string(b)
		} else {
			initial = t.buf.String()
		}
	} else {
		initial = t.buf.String()
	}

	ch := make(chan string, 100)
	if t.done != nil {
		select {
		case <-t.done:
			close(ch)
			return ch, initial, func() {}
		default:
		}
	}

	t.subscribers[ch] = struct{}{}

	var unsubOnce sync.Once
	unsubscribe := func() {
		unsubOnce.Do(func() {
			t.mu.Lock()
			delete(t.subscribers, ch)
			t.mu.Unlock()
		})
	}

	return ch, initial, unsubscribe
}

func (t *Task) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(p) == 0 {
		return 0, nil
	}

	// 1. In-memory rolling buffer capped at MaxLogMemBytes (64 KB)
	if len(p) >= MaxLogMemBytes {
		t.buf.Reset()
		t.buf.Write(p[len(p)-MaxLogMemBytes:])
	} else {
		t.buf.Write(p)
		if t.buf.Len() > MaxLogMemBytes {
			overflow := t.buf.Len() - MaxLogMemBytes
			b := t.buf.Bytes()[overflow:]
			trimmed := make([]byte, len(b))
			copy(trimmed, b)
			t.buf.Reset()
			t.buf.Write(trimmed)
		}
	}

	// 2. Disk logging capped at MaxLogDiskBytes (16 MB)
	if t.logFile != nil {
		if t.writtenBytes < MaxLogDiskBytes {
			toWrite := p
			rem := MaxLogDiskBytes - t.writtenBytes
			if int64(len(toWrite)) > rem {
				toWrite = toWrite[:rem]
			}
			if len(toWrite) > 0 {
				nw, _ := t.logFile.Write(toWrite)
				t.writtenBytes += int64(nw)
			}
			if t.writtenBytes >= MaxLogDiskBytes {
				_, _ = t.logFile.WriteString("\n[output truncated: exceeded 16MB cap]\n")
			}
		}
	}

	chunk := string(p)
	for ch := range t.subscribers {
		select {
		case ch <- chunk:
		default:
		}
	}

	return len(p), nil
}

func (t *Task) Run(ctx context.Context) error {
	if t == nil || t.Fn == nil {
		return fmt.Errorf("tasks: task and task.Fn must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	t.mu.Lock()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.startedAt.IsZero() {
		t.startedAt = time.Now().UTC()
	}
	if t.done == nil {
		t.done = make(chan struct{})
	}
	taskID := t.ID
	outputsDir := t.outputsDir
	bgCtx := t.bgCtx
	t.mu.Unlock()

	var logFile *os.File
	var logPath string
	if outputsDir != "" {
		if err := os.MkdirAll(outputsDir, 0o755); err != nil {
			return fmt.Errorf("tasks: create outputs dir: %w", err)
		}
		logPath = filepath.Join(outputsDir, taskID+".log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("tasks: open log file: %w", err)
		}
		logFile = f
	}

	stdinR, stdinW, pipeErr := os.Pipe()
	if pipeErr != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return fmt.Errorf("tasks: create stdin pipe: %w", pipeErr)
	}

	if bgCtx == nil {
		bgCtx = context.Background()
	}
	taskCtx, taskCancel := context.WithCancel(bgCtx)

	t.mu.Lock()
	t.logPath = logPath
	t.logFile = logFile
	t.stdinR = stdinR
	t.stdinW = stdinW
	t.cancel = taskCancel
	t.status = api.TaskStatusRunning
	t.mu.Unlock()

	go func() {
		exitCode, fnErr := t.Fn(taskCtx, t, stdinR)

		t.mu.Lock()
		if t.stdinW != nil {
			_ = t.stdinW.Close()
			t.stdinW = nil
		}
		if t.stdinR != nil {
			_ = t.stdinR.Close()
			t.stdinR = nil
		}
		if t.logFile != nil {
			_ = t.logFile.Sync()
			_ = t.logFile.Close()
			t.logFile = nil
		}
		for ch := range t.subscribers {
			close(ch)
		}
		t.subscribers = make(map[chan string]struct{})

		t.finishedAt = time.Now().UTC()
		t.exitCode = exitCode
		t.err = fnErr

		if t.canceled || (taskCtx.Err() != nil && errors.Is(fnErr, context.Canceled)) {
			t.status = api.TaskStatusCanceled
			if t.err == nil {
				t.err = context.Canceled
			}
		} else if fnErr != nil || exitCode != 0 {
			t.status = api.TaskStatusFailed
		} else {
			t.status = api.TaskStatusCompleted
		}

		close(t.done)
		bg := t.backgrounded
		snap := t.snapshotLocked()
		onUpdate := t.onUpdate
		t.mu.Unlock()

		if bg && onUpdate != nil {
			onUpdate(snap)
		}
	}()

	if t.Wait <= 0 {
		t.mu.Lock()
		t.backgrounded = true
		snap := t.snapshotLocked()
		onUpdate := t.onUpdate
		t.mu.Unlock()

		if onUpdate != nil {
			onUpdate(snap)
		}
		return nil
	}

	timer := time.NewTimer(t.Wait)
	defer timer.Stop()

	select {
	case <-t.Done():
		return t.Err()
	case <-ctx.Done():
		t.Cancel()
		<-t.Done()
		return ctx.Err()
	case <-timer.C:
		var snap api.Task
		shouldUpdate := false

		t.mu.Lock()
		select {
		case <-t.done:
			// Task completed before/when timer fired
		default:
			t.backgrounded = true
			snap = t.snapshotLocked()
			shouldUpdate = true
		}
		onUpdate := t.onUpdate
		t.mu.Unlock()

		if shouldUpdate && onUpdate != nil {
			onUpdate(snap)
			return nil
		}
		return t.Err()
	}
}

type Config struct {
	Context    context.Context
	OutputsDir string
	OnUpdate   func(api.Task)
}

// Store manages the background task registry for a session.
type Store interface {
	Add(task *Task) error
	Get(id string) (*Task, bool)
	List() []api.Task
	Close() error
}

type store struct {
	db         *memdb.DB[*Task]
	bgCtx      context.Context
	outputsDir string
	onUpdate   func(api.Task)
	mu         sync.Mutex
	closed     bool
}

var _ Store = (*store)(nil)

func New(cfg Config) Store {
	bgCtx := cfg.Context
	if bgCtx == nil {
		bgCtx = context.Background()
	}
	db := memdb.New(func(t *Task) string { return t.ID })
	return &store{
		db:         db,
		bgCtx:      bgCtx,
		outputsDir: strings.TrimSpace(cfg.OutputsDir),
		onUpdate:   cfg.OnUpdate,
	}
}

func (s *store) Add(task *Task) error {
	if task == nil || task.Fn == nil {
		return errors.New("tasks: task and task.Fn must not be nil")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("tasks: store is closed")
	}
	s.mu.Unlock()

	task.mu.Lock()
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.startedAt.IsZero() {
		task.startedAt = time.Now().UTC()
	}
	if task.done == nil {
		task.done = make(chan struct{})
	}
	task.bgCtx = s.bgCtx
	task.outputsDir = s.outputsDir
	task.onUpdate = s.onUpdate
	task.mu.Unlock()

	s.db.Save(task)
	return nil
}

func (s *store) Get(id string) (*Task, bool) {
	if s == nil || s.db == nil {
		return nil, false
	}
	return s.db.Snapshot().Get(id)
}

func (s *store) List() []api.Task {
	if s == nil || s.db == nil {
		return nil
	}
	all := s.db.Snapshot().All()
	var result []api.Task
	for _, t := range all {
		if t.Backgrounded() {
			result = append(result, t.Snapshot())
		}
	}
	hasNonUUID := false
	for _, t := range result {
		if !uuid.Validate(t.ID) {
			hasNonUUID = true
			break
		}
	}
	if hasNonUUID {
		slices.SortFunc(result, func(a, b api.Task) int {
			if a.StartedAt.Before(b.StartedAt) {
				return -1
			}
			if a.StartedAt.After(b.StartedAt) {
				return 1
			}
			return cmp.Compare(a.ID, b.ID)
		})
	}
	return result
}

func (s *store) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	if s.db != nil {
		for _, t := range s.db.Snapshot().All() {
			t.Cancel()
		}
	}
	return nil
}
