// Package keymap provides a mode-aware keybinding system with escape-timeout
// sequence resolution, inspired by Neovim's keymap.
package keymap

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/key"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

// Options holds metadata for a keymap binding.
type Options struct {
	Description string
}

// Option is a functional option for configuring a keymap binding.
type Option func(*Options)

// Description returns an Option that sets the binding's description.
func Description(desc string) Option {
	return func(o *Options) {
		o.Description = desc
	}
}

// defaultTimeout is the default escape timeout for key sequence resolution.
const defaultTimeout = 500 * time.Millisecond

// defaultKeymap is the package-level keymap used by plugin registrations.
// It starts empty and is populated by plugin Setup calls.
var defaultKeymap = New()

// Set maps a key sequence (lhs) to a target (rhs) in the specified modes
// on the package-level keymap. This is the primary API for plugins to register bindings.
// rhs must be either a string (macro alias) or func() (handler).
// It panics if rhs is neither a string nor a func().
func Set[T string | func(context.Context)](modes []mode.Mode, lhs string, rhs T, opts ...Option) {
	defaultKeymap.Set(modes, lhs, rhs, opts...)
}

// Default returns the package-level keymap used by plugin registrations.
// Plugins should use Set() to register bindings; consumers should use
// this function to get the keymap instance for key resolution.
func Default() *Keymap {
	return defaultKeymap
}

// binding represents a single keybinding with its target and metadata.
type binding struct {
	target      any
	description string
}

// Description returns the binding's description.
func (b binding) Description() string { return b.description }

// Keymap is a mode-aware binding table with escape-timeout sequence resolution.
type Keymap struct {
	// Modes maps each mode to its binding table.
	Modes map[mode.Mode]map[string]binding

	timeout time.Duration

	logger log.Interface

	// unexported runtime state
	sequence     []string
	sequenceMode mode.Mode
	timer        *time.Timer
	timerGen     uint64
	onTimeout    func(any)
	mu           sync.Mutex
}

// New returns an empty Keymap with a 500ms escape timeout for sequence resolution.
func New() *Keymap {
	return &Keymap{
		Modes:   make(map[mode.Mode]map[string]binding),
		timeout: defaultTimeout,
		logger:  log.ForComponent("keymap"),
	}
}

// Set maps a key sequence (lhs) to a target (rhs) in the specified modes.
// rhs must be either a string (macro alias) or func() (handler).
// It panics if rhs is neither a string nor a func().
func (km *Keymap) Set(modes []mode.Mode, lhs string, rhs any, opts ...Option) {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Validate target type
	switch rhs.(type) {
	case string, func(context.Context):
		// valid
	default:
		panic("keymap: Set: target must be string or func()")
	}

	b := binding{target: rhs}
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	b.description = o.Description

	for _, m := range modes {
		km.ensureModesLocked()
		if km.Modes[m] == nil {
			km.Modes[m] = make(map[string]binding)
		}
		km.Modes[m][lhs] = b
	}
}

// Unbind removes a keybinding from the given mode's table.
func (km *Keymap) Unbind(m mode.Mode, key string) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if bindings, ok := km.Modes[m]; ok {
		delete(bindings, key)
	}
}

// Resolve returns the target for a single key in the given mode.
// Returns (nil, false) if no binding exists.
func (km *Keymap) Resolve(m mode.Mode, key string) (any, bool) {
	km.mu.Lock()
	defer km.mu.Unlock()

	return km.resolveLocked(m, key)
}

// ResolveSequence returns the target for a sequence of keys in the given mode.
// Returns (nil, false) if no binding exists.
func (km *Keymap) ResolveSequence(m mode.Mode, keys []string) (any, bool) {
	km.mu.Lock()
	defer km.mu.Unlock()

	return km.resolveSequenceLocked(m, keys)
}

// Input processes a single key event in the given mode.
//
// If the key resolves a complete target (alone or as the end of a sequence),
// it returns (target, true).
//
// If the key is a prefix for a multi-key sequence, it is buffered and the
// function returns (nil, false) until the full sequence is received
// or the timeout expires (at which point the buffered key is flushed as a
// standalone Resolve call).
//
// The mode parameter is passed each call because the active mode can change
// between keypresses.
func (km *Keymap) Input(m mode.Mode, key string) (any, bool) {
	km.mu.Lock()
	if len(km.sequence) > 0 && km.sequenceMode != m {
		km.clearBufferLocked()
	}

	callback := km.onTimeout
	var flushedTargets []any

	for {
		candidate := append(append([]string(nil), km.sequence...), key)
		target, exact := km.resolveSequenceLocked(m, candidate)
		prefix := km.isPrefixLocked(m, candidate)

		switch {
		case exact && !prefix:
			km.clearBufferLocked()
			km.mu.Unlock()
			km.invokeResolvedTargets(callback, flushedTargets)
			return target, true

		case prefix:
			km.sequence = candidate
			km.sequenceMode = m
			km.startTimerLocked(m)
			km.mu.Unlock()
			km.invokeResolvedTargets(callback, flushedTargets)
			return nil, false

		default:
			if len(km.sequence) == 0 {
				km.clearBufferLocked()
				km.mu.Unlock()
				km.invokeResolvedTargets(callback, flushedTargets)
				return nil, false
			}

			flushedTargets = append(flushedTargets, km.resolveBufferedTargetsLocked(km.sequenceMode, km.sequence)...)
			km.clearBufferLocked()
		}
	}
}

// SetTimeout sets the escape timeout duration. Default is 500ms.
func (km *Keymap) SetTimeout(d time.Duration) {
	km.mu.Lock()
	defer km.mu.Unlock()
	km.timeout = d
}

// Timeout returns the current escape timeout duration.
func (km *Keymap) Timeout() time.Duration {
	km.mu.Lock()
	defer km.mu.Unlock()
	return km.effectiveTimeoutLocked()
}

// SetOnTimeout registers a callback invoked when a buffered key sequence
// expires without a match. The callback receives the target resolved from
// the buffered key as a standalone (or nil if none).
func (km *Keymap) SetOnTimeout(fn func(any)) {
	km.mu.Lock()
	defer km.mu.Unlock()
	km.onTimeout = fn
}

// isPrefix reports whether the given sequence is a prefix of any binding
// in the active mode's table.
func (km *Keymap) isPrefix(m mode.Mode, seq []string) bool {
	km.mu.Lock()
	defer km.mu.Unlock()

	return km.isPrefixLocked(m, seq)
}

func (km *Keymap) ensureModesLocked() {
	if km.Modes == nil {
		km.Modes = make(map[mode.Mode]map[string]binding)
	}
}

func (km *Keymap) effectiveTimeoutLocked() time.Duration {
	if km.timeout <= 0 {
		return defaultTimeout
	}
	return km.timeout
}

func (km *Keymap) resolveLocked(m mode.Mode, key string) (any, bool) {
	if bindings, ok := km.Modes[m]; ok {
		if b, ok := bindings[key]; ok {
			return b.target, true
		}
	}
	return nil, false
}

func (km *Keymap) resolveSequenceLocked(m mode.Mode, keys []string) (any, bool) {
	return km.resolveLocked(m, strings.Join(keys, ""))
}

func (km *Keymap) isPrefixLocked(m mode.Mode, seq []string) bool {
	prefix := strings.Join(seq, "")
	if bindings, ok := km.Modes[m]; ok {
		for key := range bindings {
			if strings.HasPrefix(key, prefix) && key != prefix {
				return true
			}
		}
	}
	return false
}

// clearBufferLocked clears the in-flight sequence buffer and cancels any pending timer.
func (km *Keymap) clearBufferLocked() {
	km.sequence = nil
	km.sequenceMode = mode.Mode(0)
	km.stopTimerLocked()
}

func (km *Keymap) stopTimerLocked() {
	if km.timer != nil {
		km.timer.Stop()
		km.timer = nil
	}
	km.timerGen++
}

func (km *Keymap) resolveBufferedTargetsLocked(m mode.Mode, seq []string) []any {
	targets := make([]any, 0, len(seq))
	for _, key := range seq {
		target, _ := km.resolveLocked(m, key)
		targets = append(targets, target)
	}
	return targets
}

func (km *Keymap) invokeResolvedTargets(callback func(any), targets []any) {
	if callback == nil {
		return
	}
	for _, target := range targets {
		if target != nil {
			callback(target)
		}
	}
}

// startTimerLocked starts (or resets) a timer that fires after the timeout duration.
// When it fires, it resolves the buffered sequence as a standalone and invokes the
// timeout callback.
func (km *Keymap) startTimerLocked(m mode.Mode) {
	if km.timer != nil {
		km.timer.Stop()
		km.timer = nil
	}

	km.timerGen++
	generation := km.timerGen
	timeout := km.effectiveTimeoutLocked()
	km.timer = time.AfterFunc(timeout, func() {
		km.handleTimer(generation, m)
	})
}

func (km *Keymap) handleTimer(generation uint64, m mode.Mode) {
	km.mu.Lock()
	if generation != km.timerGen || len(km.sequence) == 0 || km.sequenceMode != m {
		km.mu.Unlock()
		return
	}

	target, _ := km.resolveSequenceLocked(m, km.sequence)
	callback := km.onTimeout

	km.sequence = nil
	km.sequenceMode = mode.Mode(0)
	km.timer = nil
	km.timerGen++
	km.mu.Unlock()

	if callback != nil {
		callback(target)
	}
}

// ExecuteTarget resolves a target from the keymap and executes it if it is a func(context.Context).
// If the target is a string, it is logged as a macro alias (out of scope for execution).
// Returns (true, nil) if a target was found, (false, nil) if no binding exists.
func (km *Keymap) ExecuteTarget(ctx context.Context, m mode.Mode, ke *event.KeyEvent) (bool, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.Modes == nil || km.Modes[m] == nil {
		km.logger.Debug("no bindings for mode", log.String("mode", m.String()))
		return false, nil
	}

	bindings := km.Modes[m]
	var target any
	var matchedKey string

	// 1. Try MatchString (robust, handles synonyms)
	for lhs, b := range bindings {
		if ke.MatchString(lhs) {
			target = b.target
			matchedKey = lhs
			break
		}
	}

	// 2. Fallback to exact string match using KeyToString
	if target == nil {
		keyStr := KeyToString(ke)
		if b, ok := bindings[keyStr]; ok {
			target = b.target
			matchedKey = keyStr
		}
	}

	if target == nil {
		return false, nil
	}

	km.logger.Debug("resolved target", log.String("lhs", matchedKey), log.String("mode", m.String()))
	switch t := target.(type) {
	case string:
		km.logger.Debug("macro alias (not executed)", log.String("lhs", matchedKey), log.String("macro", t))
		return true, nil
	case func(context.Context):
		t(ctx)
		return true, nil
	default:
		return true, nil // unexpected type, silently ignore
	}
}

// ExecuteSequence resolves a key sequence from the keymap and executes it if it is a func(context.Context).
// If the target is a string, it is logged as a macro alias (out of scope for execution).
// Returns an error if the target type is unrecognized.
func (km *Keymap) ExecuteSequence(ctx context.Context, m mode.Mode, keys []string) error {
	target, ok := km.ResolveSequence(m, keys)
	if !ok {
		return nil // no binding, ignore
	}

	switch t := target.(type) {
	case string:
		km.logger.Debug("macro alias (not executed)", log.Any("keys", keys), log.String("macro", t))
		return nil
	case func(context.Context):
		t(ctx)
		return nil
	default:
		return nil // unexpected type, silently ignore
	}
}

// ModeGetter returns the current application mode.
type ModeGetter func() mode.Mode

// document holds the kite document reference for registering key event listeners.
var document dom.Document

// modeGetter returns the current application mode.
var modeGetter ModeGetter

// SetDocument installs the kite document and mode getter for registering global key event listeners.
// Called by tui.Run() after core.Setup() with the kite document and a mode getter.
func SetDocument(doc dom.Document, getMode ModeGetter) {
	document = doc
	modeGetter = getMode
	if document == nil {
		return
	}

	document.AddEventListener(event.EventKeyDown, func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok {
			return
		}

		modeState := modeGetter()
		keyStr := KeyToString(ke)
		if keyStr == "" {
			return
		}
		log.Debug("translated key", log.String("key", keyStr))

		target, matched := defaultKeymap.Resolve(modeState, keyStr)
		if !matched {
			return
		}

		e.PreventDefault()
		e.StopPropagation()

		switch t := target.(type) {
		case string:
			log.Debug("macro alias (not executed)", log.String("key", keyStr), log.String("macro", t))
		case func(context.Context):
			t(context.Background())
		}
	})
}

// ResetForTest resets the global keymap and document reference for testing.
func ResetForTest() {
	defaultKeymap = New()
	document = nil
	modeGetter = nil
}

// KeyToString converts a KeyEvent to a key string matching keymap binding format.
func KeyToString(ke *event.KeyEvent) string {
	text := ke.Text
	mod := ke.Mod
	code := ke.Code

	// Control combinations: <C-x>
	if mod&key.ModCtrl != 0 {
		switch {
		case text == "\r" || code == key.KeyEnter:
			return "<C-Enter>"
		case text == "\u001b" || code == key.KeyEscape:
			return "<C-Esc>"
		case text != "" && len(text) == 1 && text[0] >= 1 && text[0] <= 26:
			return "<C-" + string(text[0]+'a'-1) + ">"
		case code >= 'a' && code <= 'z':
			return "<C-" + string(code) + ">"
		case code >= 'A' && code <= 'Z':
			return "<C-" + string(code+'a'-'A') + ">"
		case text != "":
			return "<C-" + strings.ToLower(text) + ">"
		case code >= 1 && code <= 26:
			return "<C-" + string(code+'a'-1) + ">"
		default:
			if code != 0 && code < 127 {
				return "<C-" + string(code) + ">"
			}
			return ""
		}
	}

	// Special keys
	switch {
	case text == "\r" || code == key.KeyEnter:
		return "<Enter>"
	case text == "\u001b" || code == key.KeyEscape:
		return "<Esc>"
	case text == "\t" || code == key.KeyTab:
		if mod&key.ModShift != 0 {
			return "<S-Tab>"
		}
		return "<Tab>"
	}

	// Printable characters
	if text != "" {
		return text
	}
	if code >= 32 && code < 127 {
		return string(code)
	}

	return ""
}
