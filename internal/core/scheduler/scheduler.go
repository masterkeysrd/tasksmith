package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TaskStatus represents the current state of a registered task.
type TaskStatus string

const (
	StatusIdle      TaskStatus = "idle"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

// Runner represents the function body executed by a scheduled task.
type Runner func(ctx context.Context) error

// Task defines the runtime parameters and current state of a task.
type Task struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"` // e.g. "5s", "10m"
	Recurring bool       `json:"recurring"`
	Status    TaskStatus `json:"status"`
	LastRun   time.Time  `json:"last_run"`
	NextRun   time.Time  `json:"next_run"`
	LastError string     `json:"last_error,omitempty"`

	cancel context.CancelFunc
}

// Scheduler manages the registry, clock check ticks, and thread-safe execution of tasks.
type Scheduler struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	runners  map[string]Runner
	ctx      context.Context
	cancel   context.CancelFunc
	onChange func()
	wakeup   chan struct{}

	nowFunc func() time.Time
}

// New creates and initializes a new Scheduler.
func New(ctx context.Context) *Scheduler {
	subCtx, cancel := context.WithCancel(ctx)
	return &Scheduler{
		tasks:   make(map[string]*Task),
		runners: make(map[string]Runner),
		ctx:     subCtx,
		cancel:  cancel,
		wakeup:  make(chan struct{}, 1),
		nowFunc: time.Now,
	}
}

// OnChange registers a callback that triggers whenever any task status or list changes.
func (s *Scheduler) OnChange(cb func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = cb
}

// Register adds a new task with its execution runner to the scheduler.
func (s *Scheduler) Register(id string, name string, schedule string, recurring bool, runner Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	interval, err := time.ParseDuration(schedule)
	if err != nil && recurring {
		return fmt.Errorf("invalid schedule duration %q: %w", schedule, err)
	}

	nextRun := time.Time{}
	if recurring {
		nextRun = s.now().Add(interval)
	} else if interval > 0 {
		nextRun = s.now().Add(interval)
	} else {
		nextRun = s.now()
	}

	s.tasks[id] = &Task{
		ID:        id,
		Name:      name,
		Schedule:  schedule,
		Recurring: recurring,
		Status:    StatusIdle,
		NextRun:   nextRun,
	}
	s.runners[id] = runner

	s.notify()
	return nil
}

// Unregister cancels and removes a task from the scheduler.
func (s *Scheduler) Unregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return
	}

	if task.cancel != nil {
		task.cancel()
	}

	delete(s.tasks, id)
	delete(s.runners, id)
	s.notify()
}

// Trigger starts a task execution immediately in a separate background goroutine.
func (s *Scheduler) Trigger(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	if task.Status == StatusRunning {
		return fmt.Errorf("task %q is already running", id)
	}

	runner, ok := s.runners[id]
	if !ok {
		return fmt.Errorf("runner for task %q not found", id)
	}

	task.Status = StatusRunning
	task.LastRun = s.now()

	taskCtx, cancel := context.WithCancel(s.ctx)
	task.cancel = cancel

	go s.executeTask(id, taskCtx, runner)
	s.notify()
	return nil
}

// Cancel interrupts a currently running task using its context.
func (s *Scheduler) Cancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return
	}

	if task.cancel != nil {
		task.cancel()
		task.cancel = nil
	}

	task.Status = StatusIdle
	s.notify()
}

// Tasks returns a snapshot list of all registered tasks.
func (s *Scheduler) Tasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, *t)
	}
	return list
}

// Start runs the periodic check ticker loop to execute tasks when they become due.
func (s *Scheduler) Start(ctx context.Context) {
	var timer *time.Timer
	var timerChan <-chan time.Time

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		// Calculate the next wakeup time
		dur, ok := s.nextWakeup()
		if ok {
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(dur)
			timerChan = timer.C
		} else {
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			timerChan = nil
		}

		select {
		case <-s.ctx.Done():
			return
		case <-ctx.Done():
			return
		case <-s.wakeup:
			s.Tick()
		case <-timerChan:
			s.Tick()
		}
	}
}

// nextWakeup returns the duration until the earliest task needs to run.
// It returns a boolean indicating if there is any task scheduled.
func (s *Scheduler) nextWakeup() (time.Duration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var earliest time.Time
	hasAny := false
	now := s.now()

	for _, task := range s.tasks {
		if task.Status != StatusRunning && !task.NextRun.IsZero() {
			if !hasAny || task.NextRun.Before(earliest) {
				earliest = task.NextRun
				hasAny = true
			}
		}
	}

	if !hasAny {
		return 0, false
	}

	dur := earliest.Sub(now)
	if dur < 0 {
		return 0, true
	}
	return dur, true
}

// Tick evaluates all tasks and fires any that are due.
func (s *Scheduler) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	changed := false

	for _, task := range s.tasks {
		if task.Status != StatusRunning && !task.NextRun.IsZero() && now.After(task.NextRun) {
			runner, ok := s.runners[task.ID]
			if !ok {
				continue
			}

			task.Status = StatusRunning
			task.LastRun = now

			if task.Recurring {
				interval, err := time.ParseDuration(task.Schedule)
				if err != nil {
					interval = 1 * time.Minute
				}
				task.NextRun = now.Add(interval)
			} else {
				task.NextRun = time.Time{}
			}

			taskCtx, cancel := context.WithCancel(s.ctx)
			task.cancel = cancel

			go s.executeTask(task.ID, taskCtx, runner)
			changed = true
		}
	}

	if changed {
		s.notify()
	}
}

func (s *Scheduler) executeTask(id string, ctx context.Context, runner Runner) {
	err := runner(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return
	}

	task.cancel = nil
	if err != nil {
		task.Status = StatusFailed
		task.LastError = err.Error()
	} else {
		task.Status = StatusCompleted
		task.LastError = ""
	}

	s.notify()
}

func (s *Scheduler) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func (s *Scheduler) notify() {
	if s.onChange != nil {
		s.onChange()
	}
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}
