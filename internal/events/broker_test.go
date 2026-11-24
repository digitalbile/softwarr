package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func recv(t *testing.T, ch <-chan Message) (Message, bool) {
	t.Helper()
	select {
	case m := <-ch:
		return m, true
	case <-time.After(50 * time.Millisecond):
		return Message{}, false
	}
}

func TestBrokerRouting(t *testing.T) {
	b := NewBroker()
	u1, c1 := b.Subscribe(1, false)
	defer c1()
	u2, c2 := b.Subscribe(2, false)
	defer c2()
	adm, c3 := b.Subscribe(3, true)
	defer c3()

	b.Publish(Message{Type: TypeRequestUpdated, UserIDs: []int64{1}})
	m, ok := recv(t, u1)
	require.True(t, ok)
	require.Equal(t, TypeRequestUpdated, m.Type)
	_, ok = recv(t, u2)
	require.False(t, ok)
	_, ok = recv(t, adm)
	require.False(t, ok)

	b.Publish(Message{Type: TypeHealth, Admins: true})
	_, ok = recv(t, adm)
	require.True(t, ok)
	_, ok = recv(t, u1)
	require.False(t, ok)
	_, ok = recv(t, u2)
	require.False(t, ok)

	b.Publish(Message{Type: TypeLibraryUpdated, All: true})
	for _, ch := range []<-chan Message{u1, u2, adm} {
		_, ok = recv(t, ch)
		require.True(t, ok)
	}
}

func TestBrokerCancelStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe(1, false)
	cancel()
	cancel() // idempotent
	b.Publish(Message{Type: TypeActivity, All: true})
	_, ok := recv(t, ch)
	require.False(t, ok)
}

func TestBrokerSlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewBroker()
	slow, cancel := b.Subscribe(1, false)
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(Message{Type: TypeActivity, All: true})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
	require.Len(t, slow, 32)
}

func TestBrokerDisconnectUserClosesOnlyTheirStreams(t *testing.T) {
	b := NewBroker()
	a1, c1 := b.Subscribe(1, true)
	defer c1()
	a2, c2 := b.Subscribe(1, true)
	defer c2()
	other, c3 := b.Subscribe(2, true)
	defer c3()

	b.DisconnectUser(1)
	for _, ch := range []<-chan Message{a1, a2} {
		select {
		case _, ok := <-ch:
			require.False(t, ok, "the channel is closed")
		case <-time.After(time.Second):
			t.Fatal("stream not closed")
		}
	}
	require.Equal(t, 1, b.SubscriberCount())
	b.Publish(Message{Type: TypeHealth, Admins: true})
	_, ok := recv(t, other)
	require.True(t, ok)
	c1() // cancelling after a disconnect is fine
	b.DisconnectUser(1)
}
