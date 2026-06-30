package toast

import (
	"fmt"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
)

// Severity defines the levels of severity for a toast notification.
type Severity string

const (
	Success Severity = "success"
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

// Toast represents a single toast notification.
type Toast struct {
	ID        string
	Severity  Severity
	Title     string
	Message   string
	Duration  time.Duration
	CreatedAt time.Time
}

type toastState struct {
	Toasts  []Toast
	Version int64
}

var store = kites.Create(toastState{
	Toasts:  []Toast{},
	Version: 0,
})

// Add adds a new toast notification with custom details and returns its ID.
func Add(severity Severity, title, message string, duration time.Duration) string {
	id := fmt.Sprintf("toast-%d", time.Now().UnixNano())
	if duration <= 0 {
		duration = 5 * time.Second
	}
	t := Toast{
		ID:        id,
		Severity:  severity,
		Title:     title,
		Message:   message,
		Duration:  duration,
		CreatedAt: time.Now(),
	}
	store.Set(func(s toastState) toastState {
		// Prevent infinite memory growth: keep max 10 in the list/history buffer
		if len(s.Toasts) >= 10 {
			s.Toasts = s.Toasts[1:]
		}
		s.Toasts = append(s.Toasts, t)
		s.Version++
		return s
	})
	return id
}

// Dismiss removes a toast notification by ID.
func Dismiss(id string) {
	store.Set(func(s toastState) toastState {
		filtered := make([]Toast, 0, len(s.Toasts))
		for _, t := range s.Toasts {
			if t.ID != id {
				filtered = append(filtered, t)
			}
		}
		s.Toasts = filtered
		s.Version++
		return s
	})
}

// ClearAll removes all current notifications.
func ClearAll() {
	store.Set(func(s toastState) toastState {
		s.Toasts = []Toast{}
		s.Version++
		return s
	})
}

// UseToasts returns the slice of active toast notifications reactively.
func UseToasts() []Toast {
	// We use the Version int64 (comparable) to trigger reactivity,
	// then return the actual slice.
	_ = kites.Use(store, func(s toastState) int64 {
		return s.Version
	})
	return store.Get().Toasts
}

// GetToasts returns the current slice of active toast notifications (non-reactive).
// Safe to call outside the render phase of functional components.
func GetToasts() []Toast {
	return store.Get().Toasts
}

// AddSuccess adds a success toast.
func AddSuccess(title, message string) string {
	return Add(Success, title, message, 5*time.Second)
}

// AddInfo adds an informational toast.
func AddInfo(title, message string) string {
	return Add(Info, title, message, 5*time.Second)
}

// AddWarning adds a warning toast.
func AddWarning(title, message string) string {
	return Add(Warning, title, message, 5*time.Second)
}

// AddError adds a generic error toast from a Go error.
func AddError(err error) string {
	if err == nil {
		return ""
	}
	return Add(Error, "Error", err.Error(), 7*time.Second)
}

// AddErrorMessage adds an error toast with a custom title and message.
func AddErrorMessage(title, msg string) string {
	return Add(Error, title, msg, 7*time.Second)
}
