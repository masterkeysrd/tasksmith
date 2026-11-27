package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

// watcherBufferSize is how many undelivered events a watcher may hold before
// the multiplexer starts applying backpressure to the shared SSE connection.
var watcherBufferSize = 256

// watcherStallTimeout bounds how long the multiplexer waits on a watcher with
// a full buffer before treating it as wedged. A consumer that never catches
// up is delivered ErrNeedSync and detached, so one stalled watcher can never
// freeze the shared connection for every other watcher; the affected watcher
// recovers through the standard snapshot-resync path.
var watcherStallTimeout = 30 * time.Second

type eventResult struct {
	ev  api.Event
	err error
}

type eventWatcher struct {
	topics []string
	ch     chan eventResult
	ctx    context.Context

	// reaped marks a watcher the mux already removed (wedged consumer or stream
	// generation torn down); its deferred detach must not unwind topic refs a
	// second time. closeErr is the terminal error the mux could not push into
	// ch (it was full or closed); Watch yields it when the consumer finally
	// drains ch and observes the close. reaped is written under eventMux.mu;
	// closeErr is written by the mux goroutine before it closes ch, so the
	// happens-before edge through close makes it safe to read after <-ch
	// returns !ok.
	reaped   bool
	closeErr error
}

// tryDeliver hands res to the watcher. It never drops silently: when the
// buffer is full it blocks until the consumer catches up, the watcher's
// context ends, or the stall timeout trips. It reports whether the watcher
// received res normally; false means the watcher is dead (context ended or
// wedged) and must be removed from the mux.
func (w *eventWatcher) tryDeliver(res eventResult) bool {
	if w.ctx.Err() != nil {
		return false
	}
	select {
	case w.ch <- res:
		return true
	default:
	}
	timer := time.NewTimer(watcherStallTimeout)
	defer timer.Stop()
	select {
	case w.ch <- res:
		return true
	case <-w.ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

type connectResult struct {
	subID string
	err   error
}

var errOverlappingReplay = errors.New("replay requires a separate subscriber")

const subscriptionTimeout = 5 * time.Second

type eventMux struct {
	mu            sync.Mutex
	running       bool
	gen           uint64
	subID         string
	streamCancel  context.CancelFunc
	topicRefs     map[string]int
	watchers      []*eventWatcher
	connectWait   chan struct{}
	connectResult *connectResult
}

func newEventMux() *eventMux {
	return &eventMux{
		topicRefs: make(map[string]int),
	}
}

// Watch returns an iterator of events multiplexed over a shared SSE connection.
// It subscribes to topics, replaying from since if non-empty, and yields events.
func (c *Client) Watch(ctx context.Context, since string, topics ...string) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		var cleanTopics []string
		for _, t := range topics {
			t = strings.TrimSpace(t)
			if t != "" && !slices.Contains(cleanTopics, t) {
				cleanTopics = append(cleanTopics, t)
			}
		}
		if len(cleanTopics) == 0 {
			return
		}

		watcher := &eventWatcher{
			topics: cleanTopics,
			ch:     make(chan eventResult, watcherBufferSize),
			ctx:    ctx,
		}

		mux := c.mux
		_, err := mux.attach(ctx, c, watcher, since, cleanTopics)
		if errors.Is(err, errOverlappingReplay) {
			mux = newEventMux()
			_, err = mux.attach(ctx, c, watcher, since, cleanTopics)
		}
		if err != nil {
			yield(api.Event{}, err)
			return
		}

		defer mux.detach(c, watcher)

		for {
			select {
			case <-ctx.Done():
				return
			case res, ok := <-watcher.ch:
				if !ok {
					if watcher.closeErr != nil {
						yield(api.Event{}, watcher.closeErr)
					}
					return
				}
				if res.err != nil {
					yield(api.Event{}, res.err)
					return
				}
				if !yield(res.ev, nil) {
					return
				}
			}
		}
	}
}

func (m *eventMux) attach(ctx context.Context, c *Client, watcher *eventWatcher, since string, topics []string) (string, error) {
	m.mu.Lock()

	for {
		if !m.running {
			m.running = true
			m.gen++
			gen := m.gen
			m.connectWait = make(chan struct{})
			result := &connectResult{}
			m.connectResult = result
			m.subID = ""
			m.topicRefs = make(map[string]int)
			for _, t := range topics {
				m.topicRefs[t] = 1
			}
			m.watchers = append(m.watchers, watcher)

			streamCtx, streamCancel := context.WithCancel(context.Background())
			m.streamCancel = streamCancel
			connectWait := m.connectWait

			m.mu.Unlock()

			go m.runStream(streamCtx, c, gen, since, topics, connectWait)

			select {
			case <-ctx.Done():
				m.detach(c, watcher)
				return "", ctx.Err()
			case <-connectWait:
				if result.err != nil {
					m.detach(c, watcher)
				}
				return result.subID, result.err
			}
		}

		if m.connectWait != nil {
			connectWait := m.connectWait
			m.mu.Unlock()

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-connectWait:
				m.mu.Lock()
				continue
			}
		}

		subID := m.subID
		if since != "" && slices.ContainsFunc(topics, func(t string) bool { return m.topicRefs[t] > 0 }) {
			m.mu.Unlock()
			return "", errOverlappingReplay
		}
		var needed []string
		for _, t := range topics {
			m.topicRefs[t]++
			if m.topicRefs[t] == 1 {
				needed = append(needed, t)
			}
		}
		m.watchers = append(m.watchers, watcher)

		if len(needed) > 0 {
			mutationCtx, cancel := context.WithTimeout(ctx, subscriptionTimeout)
			_, err := c.Subscribe(mutationCtx, subID, since, needed...)
			cancel()
			if err != nil {
				watcher.reaped = true
				m.removeWatcherLocked(watcher)
				for _, t := range topics {
					m.topicRefs[t]--
					if m.topicRefs[t] <= 0 {
						delete(m.topicRefs, t)
					}
				}
				if len(m.watchers) == 0 {
					m.closeStreamLocked()
				}
				m.mu.Unlock()
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && apiErr.Code == api.ErrCodeNeedSync {
					return "", fmt.Errorf("%w: %w", ErrNeedSync, err)
				}
				return "", fmt.Errorf("client: subscribe additional topics: %w", err)
			}
		}

		m.mu.Unlock()
		return subID, nil
	}
}

func (m *eventMux) runStream(ctx context.Context, c *Client, gen uint64, since string, initialTopics []string, connectWait chan struct{}) {
	var notifiedConnect bool
	notifyConnect := func(err error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gen != gen {
			return
		}
		if !notifiedConnect {
			notifiedConnect = true
			m.connectResult.err = err
			m.connectResult.subID = m.subID
			m.connectWait = nil
			close(connectWait)
		}
	}

	readyCb := func(subID string) {
		m.mu.Lock()
		if m.gen == gen && subID != "" {
			m.subID = subID
		}
		m.mu.Unlock()
		notifyConnect(nil)
	}

	stream := c.EventsWithReady(ctx, "", since, readyCb, initialTopics...)

	var streamErr error
	for ev, err := range stream {
		if err != nil {
			streamErr = err
			break
		}

		if ev.Type == api.EventConnected {
			if cp, ok := ev.Payload.(api.ConnectedEvent); ok && cp.SubscriberID != "" {
				m.mu.Lock()
				if m.gen == gen {
					m.subID = cp.SubscriberID
				}
				m.mu.Unlock()
			}
			notifyConnect(nil)
			continue
		}

		m.mu.Lock()
		if m.gen != gen {
			m.mu.Unlock()
			break
		}
		watchers := slices.Clone(m.watchers)
		m.mu.Unlock()

		var dead []*eventWatcher
		for _, w := range watchers {
			if slices.Contains(w.topics, ev.Topic) && !w.tryDeliver(eventResult{ev: ev}) {
				dead = append(dead, w)
			}
		}

		if len(dead) > 0 {
			m.mu.Lock()
			var toUnsub []string
			var reaped []*eventWatcher
			for _, w := range dead {
				if w.reaped {
					continue
				}
				w.reaped = true
				reaped = append(reaped, w)
				m.removeWatcherLocked(w)
				for _, t := range w.topics {
					m.topicRefs[t]--
					if m.topicRefs[t] <= 0 {
						delete(m.topicRefs, t)
						toUnsub = append(toUnsub, t)
					}
				}
			}
			if len(m.watchers) == 0 {
				if m.gen == gen {
					m.closeStreamLocked()
				}
			} else if m.gen == gen {
				m.unsubscribeLocked(c, toUnsub)
			}
			m.mu.Unlock()
			for _, w := range reaped {
				select {
				case w.ch <- eventResult{err: fmt.Errorf("%w: consumer stalled", ErrNeedSync)}:
				default:
					// The buffer is still full: remember the terminal error so
					// Watch yields it once the consumer drains and sees close,
					// instead of the exit looking like a clean end of stream.
					w.closeErr = fmt.Errorf("%w: consumer stalled", ErrNeedSync)
				}
				close(w.ch)
			}
		}
	}

	if streamErr == nil {
		streamErr = io.EOF
	}

	notifyConnect(streamErr)

	m.mu.Lock()
	if m.gen != gen {
		m.mu.Unlock()
		return
	}
	watchers := slices.Clone(m.watchers)
	m.watchers = nil
	m.running = false
	m.subID = ""
	m.connectWait = nil
	m.topicRefs = make(map[string]int)
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	// Mark every watcher of this generation reaped: the mux no longer tracks
	// them, so their deferred detach must not unwind the next generation's
	// topic refs.
	for _, w := range watchers {
		w.reaped = true
	}
	m.mu.Unlock()

	for _, w := range watchers {
		if !w.tryDeliver(eventResult{err: streamErr}) {
			// The consumer is wedged or gone: deliver the terminal error via
			// closeErr so Watch still surfaces it after draining, rather than
			// looking like a clean end of stream.
			select {
			case w.ch <- eventResult{err: streamErr}:
			default:
				w.closeErr = streamErr
			}
		}
		close(w.ch)
	}
}

func (m *eventMux) detach(c *Client, watcher *eventWatcher) {
	m.mu.Lock()
	if watcher.reaped {
		m.mu.Unlock()
		return
	}
	watcher.reaped = true
	m.removeWatcherLocked(watcher)

	var toUnsub []string
	for _, t := range watcher.topics {
		m.topicRefs[t]--
		if m.topicRefs[t] <= 0 {
			delete(m.topicRefs, t)
			toUnsub = append(toUnsub, t)
		}
	}

	if len(m.watchers) == 0 {
		m.closeStreamLocked()
		m.mu.Unlock()
		return
	}
	m.unsubscribeLocked(c, toUnsub)
	m.mu.Unlock()
}

func (m *eventMux) unsubscribeLocked(c *Client, topics []string) {
	if len(topics) == 0 || m.subID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionTimeout)
	defer cancel()
	if _, err := c.Unsubscribe(ctx, m.subID, topics...); err != nil {
		m.streamCancel()
	}
}

func (m *eventMux) removeWatcherLocked(w *eventWatcher) {
	m.watchers = slices.DeleteFunc(m.watchers, func(item *eventWatcher) bool {
		return item == w
	})
}

func (m *eventMux) closeStreamLocked() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.gen++
	m.running = false
	m.subID = ""
	if m.connectWait != nil {
		close(m.connectWait)
		m.connectWait = nil
	}
	m.topicRefs = make(map[string]int)
}
