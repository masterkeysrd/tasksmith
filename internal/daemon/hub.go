package daemon

import (
	"context"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/session"
)

type subscriber struct {
	workspace string
	sessionID string
	ch        chan api.Event
}

type hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func newHub() *hub {
	return &hub{
		subs: make(map[*subscriber]struct{}),
	}
}

func (h *hub) subscribe(workspace, sessionID string) (*subscriber, func()) {
	sub := &subscriber{
		workspace: strings.TrimSpace(workspace),
		sessionID: strings.TrimSpace(sessionID),
		ch:        make(chan api.Event, 128),
	}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, sub)
			close(sub.ch)
			h.mu.Unlock()
		})
	}
	return sub, unsubscribe
}

func (h *hub) publish(ev api.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subs {
		if sub.workspace != "" && sub.workspace != ev.Workspace {
			continue
		}
		if sub.sessionID != "" && sub.sessionID != ev.SessionID {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

type workspacePublisher struct {
	workspace string
	hub       *hub
}

var _ session.Publisher = (*workspacePublisher)(nil)

func (p *workspacePublisher) Publish(_ context.Context, ev session.Event) {
	p.hub.publish(toAPIEvent(p.workspace, ev))
}
