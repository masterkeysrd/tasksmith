package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestScheduler(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)

	// Setup a mock clock
	var mockTimeMutex sync.Mutex
	mockTime := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	s.nowFunc = func() time.Time {
		mockTimeMutex.Lock()
		defer mockTimeMutex.Unlock()
		return mockTime
	}

	setMockTime := func(newTime time.Time) {
		mockTimeMutex.Lock()
		defer mockTimeMutex.Unlock()
		mockTime = newTime
	}

	var changeCount int
	s.OnChange(func() {
		changeCount++
	})

	var taskCounter int
	var taskMutex sync.Mutex
	runner := func(ctx context.Context) error {
		taskMutex.Lock()
		taskCounter++
		taskMutex.Unlock()
		return nil
	}

	t.Run("RegisterAndFirstTick", func(t *testing.T) {
		err := s.Register("task-1", "Test Task", "10s", true, runner)
		if err != nil {
			t.Fatalf("failed to register task: %v", err)
		}

		s.Tick()

		// Counter should still be 0 because we haven't advanced time
		taskMutex.Lock()
		currentCount := taskCounter
		taskMutex.Unlock()
		if currentCount != 0 {
			t.Errorf("expected counter to be 0, got %d", currentCount)
		}
	})

	t.Run("AdvanceClockToTriggerTask", func(t *testing.T) {
		// Advance clock by 11 seconds
		setMockTime(mockTime.Add(11 * time.Second))
		s.Tick()

		// Wait briefly for asynchronous goroutine runner to finish
		time.Sleep(10 * time.Millisecond)

		taskMutex.Lock()
		currentCount := taskCounter
		taskMutex.Unlock()
		if currentCount != 1 {
			t.Errorf("expected task to have run once, counter is %d", currentCount)
		}

		tasks := s.Tasks()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task in list, got %d", len(tasks))
		}

		if tasks[0].Status != StatusCompleted {
			t.Errorf("expected task status to be completed, got %s", tasks[0].Status)
		}
	})

	t.Run("CancelRunningTask", func(t *testing.T) {
		blockChan := make(chan struct{})
		s.Register("task-long", "Long Running Task", "10s", false, func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-blockChan:
				return nil
			}
		})

		// Trigger task manually
		err := s.Trigger("task-long")
		if err != nil {
			t.Fatalf("failed to trigger task: %v", err)
		}

		tasks := s.Tasks()
		var longTask Task
		for _, t := range tasks {
			if t.ID == "task-long" {
				longTask = t
				break
			}
		}

		if longTask.Status != StatusRunning {
			t.Errorf("expected manual triggered task to be running, got %s", longTask.Status)
		}

		// Cancel task
		s.Cancel("task-long")

		// Let's verify status resets to Idle
		tasks = s.Tasks()
		for _, t := range tasks {
			if t.ID == "task-long" {
				longTask = t
				break
			}
		}

		if longTask.Status != StatusIdle {
			t.Errorf("expected status to reset to idle on cancel, got %s", longTask.Status)
		}
	})

	t.Run("TaskFailureStatus", func(t *testing.T) {
		err := s.Register("task-fail", "Failing Task", "5s", false, func(ctx context.Context) error {
			return errors.New("something went wrong")
		})
		if err != nil {
			t.Fatalf("failed to register task: %v", err)
		}

		// Advance mock clock to trigger the fail task
		setMockTime(mockTime.Add(6 * time.Second))
		s.Tick()

		time.Sleep(10 * time.Millisecond)

		tasks := s.Tasks()
		var failTask Task
		for _, t := range tasks {
			if t.ID == "task-fail" {
				failTask = t
				break
			}
		}

		if failTask.Status != StatusFailed {
			t.Errorf("expected status to be failed, got %s", failTask.Status)
		}
		if failTask.LastError != "something went wrong" {
			t.Errorf("expected last_error to be filled, got %q", failTask.LastError)
		}
	})
}
