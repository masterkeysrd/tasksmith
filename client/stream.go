package client

import (
	"context"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
)

type eventResult struct {
	ev  api.Event
	err error
}

type eventWatcher struct {
	topics []string
	ch     chan eventResult
	ctx    context.Context
}

type eventMux struct {
	mu           sync.Mutex
	running      bool
	gen          uint64
	subID        string
	streamCancel context.CancelFunc
	topicRefs    map[string]int
	watchers     []*eventWatcher
	connectWait  chan struct{}
	connectErr   error
}

func newEventMux() *eventMux {
	return &eventMux{
		topicRefs: make(map[string]int),
	}
}

// Watch returns an iterator of events multiplexed over a shared SSE connection.
// It subscribes to topics, waits for connection, and invokes ready when connected.
func (c *Client) Watch(ctx context.Context, ready func(), topics ...string) iter.Seq2[api.Event, error] {
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
			ch:     make(chan eventResult, 256),
			ctx:    ctx,
		}

		subID, err := c.mux.attach(ctx, c, watcher, cleanTopics, ready)
		if err != nil {
			yield(api.Event{}, err)
			return
		}

		defer c.mux.detach(c, watcher, subID)

		for {
			select {
			case <-ctx.Done():
				return
			case res, ok := <-watcher.ch:
				if !ok {
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

func (m *eventMux) attach(ctx context.Context, c *Client, watcher *eventWatcher, topics []string, ready func()) (string, error) {
	m.mu.Lock()

	for {
		if !m.running {
			m.running = true
			m.gen++
			gen := m.gen
			m.connectWait = make(chan struct{})
			m.connectErr = nil
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

			go m.runStream(streamCtx, c, gen, topics, connectWait)

			select {
			case <-ctx.Done():
				m.mu.Lock()
				m.removeWatcherLocked(watcher)
				if len(m.watchers) == 0 {
					m.closeStreamLocked()
				}
				m.mu.Unlock()
				return "", ctx.Err()
			case <-connectWait:
				m.mu.Lock()
				if m.connectErr != nil {
					err := m.connectErr
					m.removeWatcherLocked(watcher)
					if len(m.watchers) == 0 {
						m.closeStreamLocked()
					}
					m.mu.Unlock()
					return "", err
				}
				subID := m.subID
				m.mu.Unlock()

				if ready != nil {
					ready()
				}
				return subID, nil
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
				if m.connectErr != nil {
					m.mu.Unlock()
					return "", m.connectErr
				}
				continue
			}
		}

		subID := m.subID
		var needed []string
		for _, t := range topics {
			m.topicRefs[t]++
			if m.topicRefs[t] == 1 {
				needed = append(needed, t)
			}
		}
		m.watchers = append(m.watchers, watcher)
		m.mu.Unlock()

		if len(needed) > 0 {
			if _, err := c.Subscribe(ctx, subID, needed...); err != nil {
				m.mu.Lock()
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
				return "", fmt.Errorf("client: subscribe additional topics: %w", err)
			}
		}

		if ready != nil {
			ready()
		}
		return subID, nil
	}
}

func (m *eventMux) runStream(ctx context.Context, c *Client, gen uint64, initialTopics []string, connectWait chan struct{}) {
	var notifiedConnect bool
	notifyConnect := func(err error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gen != gen {
			return
		}
		if !notifiedConnect {
			notifiedConnect = true
			m.connectErr = err
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

	stream := c.EventsWithReady(ctx, "", readyCb, initialTopics...)

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
		for _, w := range m.watchers {
			if slices.Contains(w.topics, ev.Topic) {
				select {
				case w.ch <- eventResult{ev: ev}:
				default:
				}
			}
		}
		m.mu.Unlock()
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
	m.mu.Unlock()

	for _, w := range watchers {
		select {
		case w.ch <- eventResult{err: streamErr}:
		default:
		}
		close(w.ch)
	}
}

func (m *eventMux) detach(c *Client, watcher *eventWatcher, subID string) {
	m.mu.Lock()
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
	m.mu.Unlock()

	if len(toUnsub) > 0 && subID != "" {
		go func() {
			_, _ = c.Unsubscribe(context.Background(), subID, toUnsub...)
		}()
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
	m.connectWait = nil
	m.topicRefs = make(map[string]int)
}
