// Package events fans server-side events out to connected SSE clients.
package events

import "sync"

const (
	TypeRequestUpdated = "request.updated"
	TypeNotification   = "notification"
	TypeLibraryUpdated = "library.updated"
	TypeActivity       = "activity"
	TypeHealth         = "health"
)

const subscriberBuffer = 32

// Message is one event. It reaches a subscriber when All is set, the
// subscriber's user id is in UserIDs, or Admins is set and the subscriber is
// an admin.
type Message struct {
	Type    string
	Data    any
	UserIDs []int64
	Admins  bool
	All     bool
}

type subscriber struct {
	userID  int64
	isAdmin bool
	ch      chan Message
}

// Broker routes messages to subscribers. It is safe for concurrent use.
type Broker struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[*subscriber]struct{}{}}
}

// Subscribe registers a subscriber. The returned cancel func is idempotent and
// does not close the channel.
func (b *Broker) Subscribe(userID int64, isAdmin bool) (<-chan Message, func()) {
	s := &subscriber{userID: userID, isAdmin: isAdmin, ch: make(chan Message, subscriberBuffer)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s.ch, func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
	}
}

// SubscriberCount reports the number of active subscribers.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Publish delivers m to matching subscribers without blocking. A subscriber
// whose buffer is full misses the message.
func (b *Broker) Publish(m Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if !matches(m, s) {
			continue
		}
		select {
		case s.ch <- m:
		default:
		}
	}
}

func matches(m Message, s *subscriber) bool {
	if m.All || (m.Admins && s.isAdmin) {
		return true
	}
	for _, id := range m.UserIDs {
		if id == s.userID {
			return true
		}
	}
	return false
}
