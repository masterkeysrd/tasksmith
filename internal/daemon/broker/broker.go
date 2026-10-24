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
package broker

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Event is something that happened. It carries no routing information: which
// subscribers receive it is decided by the topics it is published to.
type Event interface {
	// EventType names the kind of event, such as "chunk". It is what a receiver
	// switches on to read the event; it is not a topic.
	EventType() string
}

// Message is an [Event] addressed to a topic. It is what a [Subscriber]
// receives.
type Message struct {
	Topic string
	Event Event
}

const (
	// bufferSize is how many undelivered messages a subscriber may hold.
	bufferSize = 256

	// MaxTopics bounds the number of topics a single subscriber may hold.
	MaxTopics = 256
)

var (
	// ErrClosed is the reason of a subscriber that was closed by its owner.
	ErrClosed = errors.New("broker: subscriber closed")

	// ErrOverflow is the reason of a subscriber that was closed because it
	// could not keep up and its buffer filled. It has missed messages, so it
	// must resubscribe and reload its state rather than carry on.
	ErrOverflow = errors.New("broker: subscriber fell behind")

	// ErrNotRegistered is returned when a subscriber that is not registered
	// with a [Broker] subscribes to a topic.
	ErrNotRegistered = errors.New("broker: subscriber is not registered")

	// ErrExists is returned when a subscriber ID is already registered.
	ErrExists = errors.New("broker: subscriber ID already registered")

	// ErrInvalidTopic is returned for a topic that is not a well-formed name.
	ErrInvalidTopic = errors.New("broker: invalid topic")

	// ErrTooManyTopics is returned when a subscription would exceed
	// [MaxTopics].
	ErrTooManyTopics = errors.New("broker: too many topics")
)

// ValidateTopic reports whether topic is a well-formed name: non-empty
// dot-separated segments with no whitespace and no wildcard characters. Topics
// are matched exactly, so there are no patterns.
func ValidateTopic(topic string) error {
	if topic == "" {
		return fmt.Errorf("%w: empty", ErrInvalidTopic)
	}
	if strings.ContainsAny(topic, " \t\r\n*>") {
		return fmt.Errorf("%w: %q has whitespace or a wildcard", ErrInvalidTopic, topic)
	}
	for segment := range strings.SplitSeq(topic, ".") {
		if segment == "" {
			return fmt.Errorf("%w: %q has an empty segment", ErrInvalidTopic, topic)
		}
	}
	return nil
}

// Broker delivers published events to the subscribers that subscribed to their
// exact topic. It keeps an index from topic to subscribers, so publishing
// touches only the subscribers that asked for it.
//
// Lock order is the broker's lock, then a subscriber's lock.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	topics      map[string]map[*Subscriber]struct{}
}

// New returns a broker with no subscribers.
func New() *Broker {
	return &Broker{
		subscribers: make(map[string]*Subscriber),
		topics:      make(map[string]map[*Subscriber]struct{}),
	}
}

// Add registers a subscriber. It returns [ErrExists] if the ID is taken or the
// subscriber already belongs to a broker, and the close reason if it was
// closed.
func (b *Broker) Add(sub *Subscriber) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[sub.id]; ok {
		return fmt.Errorf("%w: %q", ErrExists, sub.id)
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return sub.err
	}
	if sub.broker != nil {
		return fmt.Errorf("%w: %q already belongs to a broker", ErrExists, sub.id)
	}
	sub.broker = b
	b.subscribers[sub.id] = sub
	return nil
}

// Remove unregisters a subscriber, drops all of its subscriptions and closes
// it with [ErrClosed]. It is safe to call more than once.
func (b *Broker) Remove(sub *Subscriber) {
	b.remove(sub, ErrClosed)
}

func (b *Broker) remove(sub *Subscriber, reason error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[sub.id] == sub {
		delete(b.subscribers, sub.id)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	for topic := range sub.topics {
		b.dropLocked(topic, sub)
	}
	sub.topics = make(map[string]struct{})
	sub.closeLocked(reason)
}

// Get returns the registered subscriber with the given ID.
func (b *Broker) Get(id string) (*Subscriber, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sub, ok := b.subscribers[id]
	return sub, ok
}

// Len returns the number of registered subscribers.
func (b *Broker) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subscribers)
}

// Publish delivers event to every subscriber subscribed to topic.
// A subscriber whose buffer is full is closed with [ErrOverflow] and removed,
// so a slow subscriber can never silently miss part of a stream.
func (b *Broker) Publish(topic string, event Event) {
	b.mu.RLock()
	targets := make([]*Subscriber, 0, len(b.topics[topic]))
	for sub := range b.topics[topic] {
		targets = append(targets, sub)
	}
	b.mu.RUnlock()

	message := Message{Topic: topic, Event: event}
	for _, sub := range targets {
		switch sub.deliver(message) {
		case deliveredOK:
		case deliveredOverflow:
			b.remove(sub, ErrOverflow)
		}
	}
}

func (b *Broker) subscribe(sub *Subscriber, topics []string) ([]string, error) {
	for _, topic := range topics {
		if err := ValidateTopic(topic); err != nil {
			return nil, err
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	sub.mu.Lock()
	defer sub.mu.Unlock()

	if sub.closed {
		return nil, sub.err
	}
	if sub.broker != b || b.subscribers[sub.id] != sub {
		return nil, ErrNotRegistered
	}

	var added []string
	for _, topic := range topics {
		if _, ok := sub.topics[topic]; ok || slices.Contains(added, topic) {
			continue
		}
		added = append(added, topic)
	}
	if len(sub.topics)+len(added) > MaxTopics {
		return nil, fmt.Errorf("%w: limit is %d", ErrTooManyTopics, MaxTopics)
	}
	for _, topic := range added {
		sub.topics[topic] = struct{}{}
		index := b.topics[topic]
		if index == nil {
			index = make(map[*Subscriber]struct{})
			b.topics[topic] = index
		}
		index[sub] = struct{}{}
	}
	return added, nil
}

func (b *Broker) unsubscribe(sub *Subscriber, topics []string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub.mu.Lock()
	defer sub.mu.Unlock()

	var removed []string
	for _, topic := range topics {
		if _, ok := sub.topics[topic]; !ok {
			continue
		}
		delete(sub.topics, topic)
		b.dropLocked(topic, sub)
		removed = append(removed, topic)
	}
	return removed
}

// dropLocked removes sub from the topic index. b.mu must be held.
func (b *Broker) dropLocked(topic string, sub *Subscriber) {
	index := b.topics[topic]
	delete(index, sub)
	if len(index) == 0 {
		delete(b.topics, topic)
	}
}

type deliveryResult int

const (
	deliveredOK deliveryResult = iota
	deliveredClosed
	deliveredOverflow
)

// Subscriber is one connected stream of messages. It receives only the
// messages published to topics it explicitly subscribed to.
type Subscriber struct {
	id string
	ch chan Message

	// done is closed when the subscriber is closed, whatever the reason. The
	// message channel is never closed, so a publisher can never send on a
	// closed channel; receivers select on both.
	done chan struct{}

	mu     sync.Mutex
	broker *Broker
	topics map[string]struct{}
	closed bool
	err    error
}

// NewSubscriber returns a subscriber with the given ID and no subscriptions.
// Register it with [Broker.Add] before subscribing.
func NewSubscriber(id string) *Subscriber {
	return &Subscriber{
		id:     id,
		ch:     make(chan Message, bufferSize),
		done:   make(chan struct{}),
		topics: make(map[string]struct{}),
	}
}

// ID returns the subscriber's identifier.
func (s *Subscriber) ID() string { return s.id }

// Subscribe adds topics and returns the ones that were not already held. The
// subscriber must be registered with a broker. If any topic is invalid, or the
// topic limit would be exceeded, none are added.
func (s *Subscriber) Subscribe(topics ...string) ([]string, error) {
	s.mu.Lock()
	broker := s.broker
	s.mu.Unlock()
	if broker == nil {
		return nil, ErrNotRegistered
	}
	return broker.subscribe(s, topics)
}

// Unsubscribe removes topics and returns the ones that were held. Topics the
// subscriber was not subscribed to are ignored.
func (s *Subscriber) Unsubscribe(topics ...string) []string {
	s.mu.Lock()
	broker := s.broker
	s.mu.Unlock()
	if broker == nil {
		return nil
	}
	return broker.unsubscribe(s, topics)
}

// Topics returns the topics the subscriber is subscribed to, sorted.
func (s *Subscriber) Topics() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	topics := make([]string, 0, len(s.topics))
	for topic := range s.topics {
		topics = append(topics, topic)
	}
	slices.Sort(topics)
	return topics
}

// Ch returns the channel messages are delivered on. It is never closed: stop
// reading when [Subscriber.Done] is closed.
func (s *Subscriber) Ch() <-chan Message { return s.ch }

// Done is closed when the subscriber is closed.
func (s *Subscriber) Done() <-chan struct{} { return s.done }

// Err explains why the subscriber was closed: [ErrClosed] when its owner
// closed it, [ErrOverflow] when it fell behind. It is nil while open.
func (s *Subscriber) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// Close unregisters the subscriber and releases its subscriptions. It is safe
// to call more than once and from any goroutine.
func (s *Subscriber) Close() {
	s.mu.Lock()
	broker := s.broker
	s.mu.Unlock()

	if broker != nil {
		broker.Remove(s)
		return
	}
	s.mu.Lock()
	s.closeLocked(ErrClosed)
	s.mu.Unlock()
}

// deliver queues message without blocking. A full buffer means the subscriber
// has lost messages, so it is closed instead of dropping them silently.
func (s *Subscriber) deliver(message Message) deliveryResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return deliveredClosed
	}
	select {
	case s.ch <- message:
		return deliveredOK
	default:
		s.closeLocked(ErrOverflow)
		return deliveredOverflow
	}
}

// closeLocked marks the subscriber closed once. s.mu must be held.
func (s *Subscriber) closeLocked(reason error) {
	if s.closed {
		return
	}
	s.closed = true
	s.err = reason
	close(s.done)
}
