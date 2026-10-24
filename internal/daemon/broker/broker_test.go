package broker

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type testEvent struct{ name string }

func (e testEvent) EventType() string { return e.name }

func registered(t *testing.T, b *Broker, id string) *Subscriber {
	t.Helper()
	sub := NewSubscriber(id)
	if err := b.Add(sub); err != nil {
		t.Fatalf("Add(%q): %v", id, err)
	}
	return sub
}

func receive(t *testing.T, sub *Subscriber) Message {
	t.Helper()
	select {
	case message := <-sub.Ch():
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a message")
		return Message{}
	}
}

func assertNothing(t *testing.T, sub *Subscriber) {
	t.Helper()
	select {
	case message := <-sub.Ch():
		t.Fatalf("unexpected message: %+v", message)
	default:
	}
}

func TestPublishDeliversOnlyToExactTopic(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")
	if _, err := sub.Subscribe("sessions.updates"); err != nil {
		t.Fatal(err)
	}

	b.Publish("sessions.updates", testEvent{"created"})
	got := receive(t, sub)
	if got.Topic != "sessions.updates" || got.Event.EventType() != "created" {
		t.Errorf("message = %+v", got)
	}

	// Neither a longer, a shorter nor a different topic matches.
	for _, topic := range []string{"sessions.updates.extra", "sessions", "sessions.other", "session.updates"} {
		b.Publish(topic, testEvent{"x"})
	}
	assertNothing(t, sub)
}

func TestSubscribeAndUnsubscribeIncrementally(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")
	other := registered(t, b, "c2")

	// updates, then session x, then y, then z, then leave x.
	for _, topic := range []string{"sessions.updates", "sessions.x", "sessions.y", "sessions.z"} {
		if added, err := sub.Subscribe(topic); err != nil || !slices.Equal(added, []string{topic}) {
			t.Fatalf("Subscribe(%q) = %v, %v", topic, added, err)
		}
	}
	if removed := sub.Unsubscribe("sessions.x"); !slices.Equal(removed, []string{"sessions.x"}) {
		t.Fatalf("Unsubscribe removed %v", removed)
	}
	if _, err := other.Subscribe("sessions.x"); err != nil {
		t.Fatal(err)
	}

	b.Publish("sessions.x", testEvent{"x"})
	b.Publish("sessions.y", testEvent{"y"})
	b.Publish("sessions.z", testEvent{"z"})
	b.Publish("sessions.updates", testEvent{"u"})

	var got []string
	for range 3 {
		got = append(got, receive(t, sub).Event.EventType())
	}
	if want := []string{"y", "z", "u"}; !slices.Equal(got, want) {
		t.Errorf("subscriber received %v, want %v (nothing from x)", got, want)
	}
	assertNothing(t, sub)
	if message := receive(t, other); message.Event.EventType() != "x" {
		t.Errorf("the other subscriber got %+v, want x", message)
	}
	if want := []string{"sessions.updates", "sessions.y", "sessions.z"}; !slices.Equal(sub.Topics(), want) {
		t.Errorf("Topics = %v, want %v", sub.Topics(), want)
	}
}

func TestSubscribeIsIdempotentAndReportsWhatWasAdded(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")

	added, err := sub.Subscribe("a.b", "a.c", "a.b")
	if err != nil || !slices.Equal(added, []string{"a.b", "a.c"}) {
		t.Fatalf("Subscribe = %v, %v", added, err)
	}
	if added, _ := sub.Subscribe("a.b"); len(added) != 0 {
		t.Errorf("resubscribing added %v", added)
	}
	if removed := sub.Unsubscribe("never.subscribed"); len(removed) != 0 {
		t.Errorf("Unsubscribe removed %v", removed)
	}

	// A subscribed subscriber gets each message once however often it subscribed.
	b.Publish("a.b", testEvent{"once"})
	receive(t, sub)
	assertNothing(t, sub)
}

func TestSubscribeRejectsInvalidTopicsWithoutAddingAny(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")

	for _, topic := range []string{"", "a..b", ".a", "a.", "a b", "a.*", "a.>", "*"} {
		if _, err := sub.Subscribe("good.topic", topic); !errors.Is(err, ErrInvalidTopic) {
			t.Errorf("Subscribe(%q) error = %v, want ErrInvalidTopic", topic, err)
		}
	}
	if topics := sub.Topics(); len(topics) != 0 {
		t.Errorf("a rejected subscribe added %v", topics)
	}
}

func TestSubscribeEnforcesTheTopicLimit(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")

	all := make([]string, MaxTopics)
	for i := range all {
		all[i] = fmt.Sprintf("t.%d", i)
	}
	if _, err := sub.Subscribe(all...); err != nil {
		t.Fatalf("subscribing up to the limit: %v", err)
	}
	if _, err := sub.Subscribe("t.one.more"); !errors.Is(err, ErrTooManyTopics) {
		t.Errorf("error = %v, want ErrTooManyTopics", err)
	}
	// Topics already held do not count again.
	if _, err := sub.Subscribe(all[0]); err != nil {
		t.Errorf("resubscribing a held topic at the limit: %v", err)
	}
}

func TestSubscriberMustBeRegisteredToSubscribe(t *testing.T) {
	sub := NewSubscriber("loose")
	if _, err := sub.Subscribe("a.b"); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("error = %v, want ErrNotRegistered", err)
	}

	b := New()
	registered(t, b, "dup")
	if err := b.Add(NewSubscriber("dup")); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate ID error = %v, want ErrExists", err)
	}
	other := New()
	member := registered(t, b, "member")
	if err := other.Add(member); err == nil {
		t.Error("a subscriber was added to two brokers")
	}
}

func TestSlowSubscriberIsClosedNotSilentlyDropped(t *testing.T) {
	b := New()
	slow := registered(t, b, "slow")
	healthy := registered(t, b, "healthy")
	for _, c := range []*Subscriber{slow, healthy} {
		if _, err := c.Subscribe("stream"); err != nil {
			t.Fatal(err)
		}
	}

	for i := range bufferSize {
		b.Publish("stream", testEvent{fmt.Sprint(i)})
		// The healthy subscriber keeps up.
		receive(t, healthy)
	}
	if slow.Err() != nil {
		t.Fatalf("the subscriber closed with a full buffer but no overflow: %v", slow.Err())
	}

	b.Publish("stream", testEvent{"one too many"})
	select {
	case <-slow.Done():
	default:
		t.Fatal("the slow subscriber was not closed")
	}
	if !errors.Is(slow.Err(), ErrOverflow) {
		t.Errorf("slow subscriber error = %v, want ErrOverflow", slow.Err())
	}
	if _, ok := b.Get("slow"); ok {
		t.Error("the slow subscriber is still registered")
	}
	if receive(t, healthy).Event.EventType() != "one too many" {
		t.Error("the healthy subscriber missed the message")
	}
	if b.Len() != 1 {
		t.Errorf("Len = %d, want 1", b.Len())
	}
	// Later publishes no longer reach it.
	b.Publish("stream", testEvent{"after"})
	if receive(t, healthy).Event.EventType() != "after" {
		t.Error("healthy subscriber missed message after slow subscriber overflowed")
	}
}

func TestCloseReleasesSubscriptionsAndIsIdempotent(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")
	if _, err := sub.Subscribe("a.b"); err != nil {
		t.Fatal(err)
	}

	sub.Close()
	sub.Close()
	b.Remove(sub)

	select {
	case <-sub.Done():
	default:
		t.Fatal("Done is not closed after Close")
	}
	if !errors.Is(sub.Err(), ErrClosed) {
		t.Errorf("Err = %v, want ErrClosed", sub.Err())
	}
	b.Publish("a.b", testEvent{"x"})
	assertNothing(t, sub)
	if _, err := sub.Subscribe("c.d"); !errors.Is(err, ErrClosed) {
		t.Errorf("Subscribe after Close = %v, want ErrClosed", err)
	}
	if b.Len() != 0 || len(b.topics) != 0 {
		t.Errorf("broker kept state: %d subscribers, %d topics", b.Len(), len(b.topics))
	}
}

func TestConcurrentPublishSubscribeAndClose(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for range 4 {
		wg.Go(func() {
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				b.Publish(fmt.Sprintf("topic.%d", n%3), testEvent{"x"})
			}
		})
	}
	for i := range 20 {
		wg.Go(func() {
			sub := NewSubscriber(fmt.Sprintf("c%d", i))
			if err := b.Add(sub); err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			for n := range 50 {
				_, _ = sub.Subscribe(fmt.Sprintf("topic.%d", n%3))
				sub.Unsubscribe(fmt.Sprintf("topic.%d", (n+1)%3))
				select {
				case <-sub.Ch():
				default:
				}
			}
			sub.Close()
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	if b.Len() != 0 {
		t.Errorf("%d subscribers left registered", b.Len())
	}
}

func TestValidateTopic(t *testing.T) {
	for _, topic := range []string{"sessions.updates", "sessions.01a1676e-a6a2-7674-885b-d7c76662ad85", "a"} {
		if err := ValidateTopic(topic); err != nil {
			t.Errorf("ValidateTopic(%q) = %v", topic, err)
		}
	}
}

func TestMonotonicUUIDv7Publish(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")
	if _, err := sub.Subscribe("test.mono"); err != nil {
		t.Fatal(err)
	}

	const count = 500
	for i := range count {
		b.Publish("test.mono", testEvent{fmt.Sprintf("e-%d", i)})
	}

	var lastID string
	for i := range count {
		msg := receive(t, sub)
		if msg.ID == "" {
			t.Fatalf("msg[%d] has empty ID", i)
		}
		parsed, err := uuid.Parse(msg.ID)
		if err != nil {
			t.Fatalf("msg[%d] invalid UUID %q: %v", i, msg.ID, err)
		}
		if parsed.Version() != 7 {
			t.Errorf("msg[%d] version = %d, want 7", i, parsed.Version())
		}
		if msg.ID != strings.ToLower(msg.ID) {
			t.Errorf("msg[%d] not lowercase: %q", i, msg.ID)
		}
		if lastID != "" && msg.ID <= lastID {
			t.Fatalf("msg[%d] ID %q not strictly greater than previous %q", i, msg.ID, lastID)
		}
		lastID = msg.ID
	}
}

func TestSubscribeSinceReplay(t *testing.T) {
	b := New()
	sub1 := registered(t, b, "c1")
	if _, err := sub1.Subscribe("test.replay"); err != nil {
		t.Fatal(err)
	}

	const total = 5
	var ids []string
	for i := range total {
		b.Publish("test.replay", testEvent{fmt.Sprintf("e-%d", i)})
		msg := receive(t, sub1)
		ids = append(ids, msg.ID)
	}

	// sub2 subscribes since ids[1] (message 1), expecting messages 2, 3, 4
	sub2 := registered(t, b, "c2")
	added, err := sub2.SubscribeSince(ids[1], "test.replay")
	if err != nil {
		t.Fatalf("SubscribeSince: %v", err)
	}
	if !slices.Equal(added, []string{"test.replay"}) {
		t.Errorf("added = %v, want [test.replay]", added)
	}

	for i := 2; i < total; i++ {
		msg := receive(t, sub2)
		if msg.ID != ids[i] {
			t.Errorf("expected msg.ID = %q, got %q", ids[i], msg.ID)
		}
		if msg.Event.EventType() != fmt.Sprintf("e-%d", i) {
			t.Errorf("expected event e-%d, got %s", i, msg.Event.EventType())
		}
	}
	assertNothing(t, sub2)
}

func TestSubscribeSinceNeedSync(t *testing.T) {
	b := New(WithTopicBufferSize(func(topic string) int {
		return 3
	}))
	sub1 := registered(t, b, "c1")
	if _, err := sub1.Subscribe("test.needsync"); err != nil {
		t.Fatal(err)
	}

	var ids []string
	for i := range 5 {
		b.Publish("test.needsync", testEvent{fmt.Sprintf("e-%d", i)})
		msg := receive(t, sub1)
		ids = append(ids, msg.ID)
	}

	// Ring buffer of size 3 now holds ids[2], ids[3], ids[4].
	// ids[0] and ids[1] were evicted.
	// Subscribing since ids[0] (which is < ids[2]) must fail with ErrNeedSync.
	sub2 := registered(t, b, "c2")
	_, err := sub2.SubscribeSince(ids[0], "test.needsync")
	if !errors.Is(err, ErrNeedSync) {
		t.Fatalf("SubscribeSince(ids[0]) err = %v, want ErrNeedSync", err)
	}

	// Subscribing since ids[2] (oldest retained) should succeed and replay ids[3] and ids[4].
	sub3 := registered(t, b, "c3")
	_, err = sub3.SubscribeSince(ids[2], "test.needsync")
	if err != nil {
		t.Fatalf("SubscribeSince(ids[2]) err = %v, want nil", err)
	}
	msg3 := receive(t, sub3)
	if msg3.ID != ids[3] {
		t.Errorf("got %q, want ids[3] (%q)", msg3.ID, ids[3])
	}
	msg4 := receive(t, sub3)
	if msg4.ID != ids[4] {
		t.Errorf("got %q, want ids[4] (%q)", msg4.ID, ids[4])
	}
	assertNothing(t, sub3)
}

func TestSubscribeSinceInvalidCursor(t *testing.T) {
	b := New()
	sub := registered(t, b, "c1")

	for _, invalid := range []string{
		"not-a-uuid",
		"00000000-0000-4000-8000-000000000000",            // v4
		"0192e4c0-0000-7000-8000-00000000000G",            // non-hex
		strings.ToUpper(uuid.Must(uuid.NewV7()).String()), // uppercase
	} {
		_, err := sub.SubscribeSince(invalid, "test.invalid")
		if !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("SubscribeSince(%q) err = %v, want ErrInvalidCursor", invalid, err)
		}
	}
}

func TestWithTopicBufferSizeAndDeleteTopic(t *testing.T) {
	b := New(WithTopicBufferSize(func(topic string) int {
		if topic == "special" {
			return 2
		}
		return -1 // fallback to DefaultRingSize
	}))

	sub := registered(t, b, "c1")
	if _, err := sub.Subscribe("special"); err != nil {
		t.Fatal(err)
	}

	var ids []string
	for i := range 3 {
		b.Publish("special", testEvent{fmt.Sprintf("e-%d", i)})
		msg := receive(t, sub)
		ids = append(ids, msg.ID)
	}

	// ids[0] was evicted because cap=2
	sub2 := registered(t, b, "c2")
	_, err := sub2.SubscribeSince(ids[0], "special")
	if !errors.Is(err, ErrNeedSync) {
		t.Fatalf("expected ErrNeedSync for evicted cursor, got %v", err)
	}

	// DeleteTopic
	b.DeleteTopic("special")

	// After DeleteTopic, b.rings["special"] is gone, so SubscribeSince does not see evicted buffer
	sub3 := registered(t, b, "c3")
	if _, err := sub3.SubscribeSince(ids[0], "special"); err != nil {
		t.Fatalf("SubscribeSince after DeleteTopic error: %v", err)
	}
}
