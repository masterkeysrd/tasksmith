// Package broker routes events to the subscribers that asked for them.
//
// A [Subscriber] is one connected stream, for example an open
// `GET /v1/events` connection. It receives only the messages published to
// topics it explicitly subscribed to, and it can add and remove topics at any
// time while it stays connected. Topics are matched exactly: there are no
// wildcards, so a subscriber never receives anything it did not name.
//
// # Events and messages
//
// An [Event] is something that happened and knows nothing about topics. A
// [Message] is an event addressed to a topic, which is what a subscriber
// receives. Deciding which topics an event goes to is the publisher's job, and
// this package does not know about sessions or any other part of the daemon.
//
// # Slow subscribers
//
// Each subscriber has a bounded buffer. A subscriber that does not keep up is
// closed with [ErrOverflow] instead of silently losing messages, because a
// stream with a gap cannot be trusted. It must reconnect, resubscribe and
// reload its state.
//
// # Topic ring buffers & replay
//
// Each topic maintains a ring buffer ([DefaultRingSize] capacity by default,
// configurable via [WithTopicBufferSize]). [Subscriber.SubscribeSince] replays
// messages with ID greater than a given cursor from the ring buffer, enabling
// gap-free reconnection. If the cursor is older than the oldest retained
// message, the subscription fails with [ErrNeedSync].
//
// # Integration
//
// [github.com/masterkeysrd/tasksmith/internal/daemon/session.Session] creates
// one [Subscriber] per connected event stream and registers it with the
// [Broker]. The session publishes [EventEntryAdded], [EventChunk],
// [EventPermissionRequested], and [EventCompleted] events to per-session
// topics, and [Broker.DeleteTopic] releases the ring buffer when a session
// ends.
package broker
