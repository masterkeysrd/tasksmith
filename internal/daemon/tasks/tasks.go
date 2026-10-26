package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTaskNotFound = errors.New("tasks: task not found")
	ErrNotRunning   = errors.New("tasks: task is not running")
)

const (
	MaxLogDiskBytes = 16 * 1024 * 1024 // 16 MB cap on task log file
	MaxLogMemBytes  = 64 * 1024        // 64 KB rolling window for in-memory buffer
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Func is the execution workload of a Task.
// ctx is the task's execution context; output receives combined stdout/stderr; stdin supplies input.
type Func func(ctx context.Context, output io.Writer, stdin io.Reader) (exitCode int, err error)

type Snapshot struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Command      string    `json:"command,omitempty"`
	Status       Status    `json:"status"`
	ExitCode     int       `json:"exit_code"`
	Error        string    `json:"error,omitempty"`
	LogPath      string    `json:"log_path,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitzero"`
	DurationMs   int64     `json:"duration_ms"`
	Backgrounded bool      `json:"backgrounded"`
}

type Task struct {
	ID      string
	Kind    string
	Title   string
	Command string
	Wait    time.Duration
	Fn      Func

	mu           sync.Mutex
	status       Status
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
}

func (t *Task) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *Task) snapshotLocked() Snapshot {
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
		status = StatusPending
	}
	return Snapshot{
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

func (t *Task) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status == "" {
		return StatusPending
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

func (t *Task) WriteInput(data string) error {
	t.mu.Lock()
	if t.status != StatusRunning || t.stdinW == nil {
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

type taskWriter struct {
	task *Task
}

func (w *taskWriter) Write(p []byte) (n int, err error) {
	w.task.mu.Lock()
	defer w.task.mu.Unlock()

	if len(p) == 0 {
		return 0, nil
	}

	// 1. In-memory rolling buffer capped at MaxLogMemBytes (64 KB)
	if len(p) >= MaxLogMemBytes {
		w.task.buf.Reset()
		w.task.buf.Write(p[len(p)-MaxLogMemBytes:])
	} else {
		w.task.buf.Write(p)
		if w.task.buf.Len() > MaxLogMemBytes {
			overflow := w.task.buf.Len() - MaxLogMemBytes
			b := w.task.buf.Bytes()[overflow:]
			trimmed := make([]byte, len(b))
			copy(trimmed, b)
			w.task.buf.Reset()
			w.task.buf.Write(trimmed)
		}
	}

	// 2. Disk logging capped at MaxLogDiskBytes (16 MB)
	if w.task.logFile != nil {
		if w.task.writtenBytes < MaxLogDiskBytes {
			toWrite := p
			rem := MaxLogDiskBytes - w.task.writtenBytes
			if int64(len(toWrite)) > rem {
				toWrite = toWrite[:rem]
			}
			if len(toWrite) > 0 {
				nw, _ := w.task.logFile.Write(toWrite)
				w.task.writtenBytes += int64(nw)
			}
			if w.task.writtenBytes >= MaxLogDiskBytes {
				_, _ = w.task.logFile.WriteString("\n[output truncated: exceeded 16MB cap]\n")
			}
		}
	}

	chunk := string(p)
	for ch := range w.task.subscribers {
		select {
		case ch <- chunk:
		default:
		}
	}

	return len(p), nil
}

type Config struct {
	Context    context.Context
	OutputsDir string
	OnUpdate   func(Snapshot)
}

type Manager interface {
	Run(ctx context.Context, task *Task) error
	Get(id string) (*Task, bool)
	List() []Snapshot
	Kill(id string) error
	WriteInput(id string, data string) error
	Close() error
}

type manager struct {
	mu         sync.Mutex
	bgCtx      context.Context
	outputsDir string
	onUpdate   func(Snapshot)
	tasks      map[string]*Task
	closed     bool
}

func New(cfg Config) Manager {
	bgCtx := cfg.Context
	if bgCtx == nil {
		bgCtx = context.Background()
	}
	return &manager{
		bgCtx:      bgCtx,
		outputsDir: strings.TrimSpace(cfg.OutputsDir),
		onUpdate:   cfg.OnUpdate,
		tasks:      make(map[string]*Task),
	}
}

func (m *manager) Run(ctx context.Context, task *Task) error {
	if task == nil || task.Fn == nil {
		return fmt.Errorf("tasks: task and task.Fn must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("tasks: manager is closed")
	}
	m.mu.Unlock()

	if task.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("tasks: generate task id: %w", err)
		}
		task.ID = id.String()
	}

	if m.outputsDir != "" {
		if err := os.MkdirAll(m.outputsDir, 0o755); err != nil {
			return fmt.Errorf("tasks: create outputs dir: %w", err)
		}
		logPath := filepath.Join(m.outputsDir, task.ID+".log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("tasks: open log file: %w", err)
		}
		task.logPath = logPath
		task.logFile = f
	}

	stdinR, stdinW, pipeErr := os.Pipe()
	if pipeErr != nil {
		return fmt.Errorf("tasks: create stdin pipe: %w", pipeErr)
	}
	taskCtx, taskCancel := context.WithCancel(m.bgCtx)

	task.mu.Lock()
	if task.done == nil {
		task.done = make(chan struct{})
	}
	task.stdinR = stdinR
	task.stdinW = stdinW
	task.cancel = taskCancel
	task.status = StatusRunning
	task.startedAt = time.Now().UTC()
	task.mu.Unlock()

	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()

	writer := &taskWriter{task: task}

	go func() {
		exitCode, fnErr := task.Fn(taskCtx, writer, stdinR)

		task.mu.Lock()
		if task.stdinW != nil {
			_ = task.stdinW.Close()
			task.stdinW = nil
		}
		if task.stdinR != nil {
			_ = task.stdinR.Close()
			task.stdinR = nil
		}
		if task.logFile != nil {
			_ = task.logFile.Sync()
			_ = task.logFile.Close()
			task.logFile = nil
		}
		for ch := range task.subscribers {
			close(ch)
		}
		task.subscribers = make(map[chan string]struct{})

		task.finishedAt = time.Now().UTC()
		task.exitCode = exitCode
		task.err = fnErr

		if task.canceled || (taskCtx.Err() != nil && errors.Is(fnErr, context.Canceled)) {
			task.status = StatusCanceled
			if task.err == nil {
				task.err = context.Canceled
			}
		} else if fnErr != nil || exitCode != 0 {
			task.status = StatusFailed
		} else {
			task.status = StatusCompleted
		}

		close(task.done)
		bg := task.backgrounded
		snap := task.snapshotLocked()
		task.mu.Unlock()

		if bg && m.onUpdate != nil {
			m.onUpdate(snap)
		}
	}()

	if task.Wait <= 0 {
		task.mu.Lock()
		task.backgrounded = true
		snap := task.snapshotLocked()
		task.mu.Unlock()

		if m.onUpdate != nil {
			m.onUpdate(snap)
		}
		return nil
	}

	timer := time.NewTimer(task.Wait)
	defer timer.Stop()

	select {
	case <-task.Done():
		return task.Err()
	case <-ctx.Done():
		task.Cancel()
		<-task.Done()
		return ctx.Err()
	case <-timer.C:
		var snap Snapshot
		shouldUpdate := false

		task.mu.Lock()
		select {
		case <-task.done:
			// Task completed before/when timer fired
		default:
			task.backgrounded = true
			snap = task.snapshotLocked()
			shouldUpdate = true
		}
		task.mu.Unlock()

		if shouldUpdate && m.onUpdate != nil {
			m.onUpdate(snap)
			return nil
		}
		return task.Err()
	}
}

func (m *manager) Get(id string) (*Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	return t, ok
}

func (m *manager) List() []Snapshot {
	m.mu.Lock()
	tasks := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	m.mu.Unlock()

	var result []Snapshot
	for _, t := range tasks {
		if t.Backgrounded() {
			result = append(result, t.Snapshot())
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})

	return result
}

func (m *manager) Kill(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrTaskNotFound
	}
	if st := t.Status(); st != StatusRunning && st != StatusPending {
		return ErrNotRunning
	}
	t.Cancel()
	return nil
}

func (m *manager) WriteInput(id string, data string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrTaskNotFound
	}
	return t.WriteInput(data)
}

func (m *manager) Close() error {
	m.mu.Lock()
	m.closed = true
	tasks := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	m.mu.Unlock()

	for _, t := range tasks {
		t.Cancel()
	}
	return nil
}
