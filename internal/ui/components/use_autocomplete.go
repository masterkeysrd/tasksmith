package components

import (
	"context"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

const autocompleteCacheStaleTime = 2 * time.Second

type completionKey struct {
	Engine uintptr
	Value  string
	Cursor int
}

type completionSpan struct {
	Engine  uintptr
	Source  string
	Trigger string
	Start   int
}

type activeCompletionSession struct {
	ID   uint64
	Span completionSpan
}

type lastCompletion struct {
	Valid      bool
	Span       completionSpan
	Candidates []autocomplete.Candidate
}

type completionScope struct {
	ID      uint64
	Context context.Context
	Cancel  context.CancelFunc
}

type completionQueryKey struct {
	completionKey
	Scope uint64
}

var completionSequence atomic.Uint64

// AutocompleteOptions identifies the current draft and source snapshot.
// Supply a new Engine when the underlying source data changes.
type AutocompleteOptions struct {
	Engine   *autocomplete.Engine
	Value    string
	Cursor   int
	Disabled bool
}

// AutocompleteEdit replaces a draft and restores its UTF-8 byte cursor.
type AutocompleteEdit struct {
	Value  string
	Cursor int
}

// AutocompleteState is shared by the input's keyboard handling and the view.
type AutocompleteState struct {
	Result    autocomplete.Result
	Selected  int
	IsOpen    bool
	IsLoading bool
	Error     error
	Up        func() bool
	Down      func() bool
	Dismiss   func() bool
	Accept    func() (AutocompleteEdit, error)
}

type completionSelection struct {
	Key   completionKey
	Index int
}

type completionDismissal struct {
	Key    completionKey
	Active bool
}

type completionResponse struct {
	Key    completionQueryKey
	Result autocomplete.Result
}

type completionError struct {
	Key completionKey
	Err error
}

// UseAutocomplete resolves completions using the enclosing wind.Provider.
// It never submits input or executes candidates.
func UseAutocomplete(opts AutocompleteOptions) AutocompleteState {
	// Kite deep-compares dependencies; scalar identities avoid inspecting
	// mutable engine locks and cancellation contexts.
	engineID := reflect.ValueOf(opts.Engine).Pointer()
	key := completionKey{Engine: engineID, Value: opts.Value, Cursor: opts.Cursor}
	selection, setSelection := kitex.UseState(completionSelection{})
	dismissal, setDismissal := kitex.UseState(completionDismissal{})
	applyError, setApplyError := kitex.UseState(completionError{})
	sessionRef := kitex.UseRef(activeCompletionSession{})
	lastResolved := kitex.UseRef(lastCompletion{})

	var request autocomplete.Request
	var matchErr error
	if opts.Engine != nil && !opts.Disabled {
		request, matchErr = opts.Engine.Match(opts.Value, opts.Cursor)
	}
	dismissed := dismissal().Active && dismissal().Key == key
	enabled := request.Source != "" && matchErr == nil && !dismissed

	var span completionSpan
	var activeKey completionKey
	var queryKey completionQueryKey
	if enabled {
		span = completionSpan{
			Engine:  engineID,
			Source:  request.Source,
			Trigger: request.Trigger,
			Start:   request.Range.Start,
		}
		if sessionRef.Current.ID == 0 || sessionRef.Current.Span != span {
			sessionRef.Current = activeCompletionSession{
				ID:   completionSequence.Add(1),
				Span: span,
			}
		}
		activeKey = key
		queryKey = completionQueryKey{completionKey: activeKey, Scope: sessionRef.Current.ID}
	} else {
		sessionRef.Current = activeCompletionSession{}
		lastResolved.Current = lastCompletion{}
	}

	scope := kitex.UseMemo(func() *completionScope {
		ctx, cancel := context.WithCancel(context.Background())
		return &completionScope{
			ID:      completionSequence.Add(1),
			Context: ctx,
			Cancel:  cancel,
		}
	}, []any{activeKey, enabled})

	kitex.UseEffectCleanup(func() func() {
		return scope.Cancel
	}, []any{scope.ID})

	kitex.UseEffect(func() {
		if selection().Index != 0 && selection().Key != key {
			setSelection(completionSelection{})
		}
		if dismissal().Active && dismissal().Key != key {
			setDismissal(completionDismissal{})
		}
	}, []any{key})

	query := wind.Use(queryKey, func(ctx context.Context, key completionQueryKey) *promise.Promise[completionResponse] {
		return promise.Wrap(func(ctx context.Context) (completionResponse, error) {
			ctx, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(scope.Context, cancel)
			defer stop()
			defer cancel()
			result, err := opts.Engine.Complete(ctx, key.Value, key.Cursor)
			return completionResponse{Key: key, Result: result}, err
		})(ctx)
	}, wind.Options{Enabled: enabled, StaleTime: autocompleteCacheStaleTime})

	loading := enabled && (query.IsLoading || query.IsFetching || query.Data.Key != queryKey && query.Error == nil)
	result := query.Data.Result
	hasPriorInSpan := false
	if enabled && query.Data.Key == queryKey && !loading && query.Error == nil {
		lastResolved.Current = lastCompletion{
			Valid:      true,
			Span:       span,
			Candidates: result.Candidates,
		}
	} else if query.Data.Key != queryKey {
		result = autocomplete.Result{Request: request}
		if prev := lastResolved.Current; enabled && matchErr == nil && query.Error == nil && prev.Valid && prev.Span == span {
			hasPriorInSpan = true
			if len(prev.Candidates) > 0 {
				result.Candidates = prev.Candidates
			}
		}
	}
	count := len(result.Candidates)
	selected := 0
	if current := selection(); current.Key == key {
		selected = min(current.Index, max(0, count-1))
	}
	err := matchErr
	if err == nil && enabled {
		err = query.Error
	}
	if local := applyError(); err == nil && local.Key == key {
		err = local.Err
	}

	showLoading := loading && !hasPriorInSpan
	open := !opts.Disabled && !dismissed && (err != nil || enabled && (showLoading || count > 0))

	move := func(delta int) bool {
		if !open || count == 0 || loading || err != nil {
			return false
		}
		current := selection()
		index := selected
		if current.Key == key {
			index = min(current.Index, count-1)
		}
		setSelection(completionSelection{Key: key, Index: max(0, min(index+delta, count-1))})
		return true
	}
	return AutocompleteState{
		Result: result, Selected: selected, IsOpen: open, IsLoading: loading, Error: err,
		Up:   func() bool { return move(-1) },
		Down: func() bool { return move(1) },
		Dismiss: func() bool {
			if !open || dismissal().Active && dismissal().Key == key {
				return false
			}
			setDismissal(completionDismissal{Key: key, Active: true})
			return true
		},
		Accept: func() (AutocompleteEdit, error) {
			index := selected
			if current := selection(); current.Key == key {
				index = min(current.Index, max(0, count-1))
			}
			if !open || loading || err != nil {
				return AutocompleteEdit{}, autocomplete.ErrInvalidSelection
			}
			value, cursor, err := result.Apply(index)
			if err != nil {
				setApplyError(completionError{Key: key, Err: err})
				return AutocompleteEdit{}, err
			}
			next, err := opts.Engine.Match(value, cursor)
			if err != nil {
				setApplyError(completionError{Key: key, Err: err})
				return AutocompleteEdit{}, err
			}
			if next.Source == request.Source && next.Trigger == request.Trigger {
				setDismissal(completionDismissal{
					Key: completionKey{Engine: engineID, Value: value, Cursor: cursor}, Active: true,
				})
			}
			return AutocompleteEdit{Value: value, Cursor: cursor}, nil
		},
	}
}
