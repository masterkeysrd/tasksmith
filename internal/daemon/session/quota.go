package session

import (
	"context"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

// quotaPublishTimeout bounds one post-turn quota read. Quota is display
// metadata: a slow provider must never delay the event stream.
const quotaPublishTimeout = 8 * time.Second

// publishQuota refreshes the active model's quota and publishes a
// QuotaUpdatedEvent on the session stream. It runs only for models whose
// provider bills against a subscription window, so per-token providers never
// pay a quota round trip. The read is bound to the session lifecycle context
// — never a per-turn context, which a rate-limited failure cancels while the
// quota read is the one thing that must still succeed — so it is also cut
// short when the session closes. A single read is in flight at a time: when
// finishTurn and the rate-limit path race, one of them wins and the other is
// dropped rather than publishing a duplicate event.
func (s *Session) publishQuota() {
	model, ok := s.quotaBilledModel()
	if !ok || s.publisher == nil || s.bgCtx == nil {
		return
	}
	gw := s.gateway()
	if gw == nil {
		return
	}
	if !s.quotaInFlight.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer s.quotaInFlight.Store(false)
		ctx, cancel := context.WithTimeout(s.bgCtx, quotaPublishTimeout)
		defer cancel()

		quota, err := gw.Quota(ctx, model)
		if err != nil || quota == nil {
			return
		}
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QuotaUpdatedEvent{
			SessionID: s.ID,
			Quota:     *quota,
		})
	}()
}

// quotaBilledModel resolves the session's active model and reports whether it
// is billed against a subscription window.
func (s *Session) quotaBilledModel() (models.Model, bool) {
	m, _, ok := s.ResolvedModel()
	if !ok || !m.QuotaBilled || s.gateway() == nil {
		return models.Model{}, false
	}
	return m, true
}
