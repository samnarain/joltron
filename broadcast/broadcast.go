package broadcast

import (
	"errors"
	"sync"
)

var (
	// ErrNotInBroadcast is returned when you try leaving a broadcast when you're not in it.
	ErrNotInBroadcast = errors.New("Not in broadcast")

	// ErrAlreadyClosed is returned when you try doing operations on an already closed broadcaster
	ErrAlreadyClosed = errors.New("Broadcast is already closed")
)

// Broadcaster is a writer -> multiple readers utility
type Broadcaster struct {
	mu        sync.Mutex
	listeners []chan interface{}
	closed    bool
}

// Subscriber is a consumer of a broadcaster messages.
// It has the read only channel the Broadcast.Join/Leave functions work with.
type Subscriber struct {
	ch          <-chan interface{}
	broadcaster *Broadcaster
}

// Next returns a read channel on which messages from the broadcaster can be received
func (s *Subscriber) Next() <-chan interface{} {
	return s.ch
}

// Close unsubscribes from the broadcaster
func (s *Subscriber) Close() error {
	return s.broadcaster.Leave(s)
}

// NewBroadcaster creates a new broadcaster
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		listeners: make([]chan interface{}, 0, 2),
	}
}

// Join returns a new channel that will receive broadcasts from this broadcaster.
// the channel must not be closed manually. Instead, call Leave() with it.
func (b *Broadcaster) Join() (*Subscriber, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrAlreadyClosed
	}

	ch := make(chan interface{}, 10)
	b.listeners = append(b.listeners, ch)
	return &Subscriber{ch: ch, broadcaster: b}, nil
}

// Leave removes a channel from a broadcaster, and closes it.
func (b *Broadcaster) Leave(s *Subscriber) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrAlreadyClosed
	}

	for i, val := range b.listeners {
		if val == s.ch {
			close(val)
			// Fast way of removing an element when we don't care about order - swap removed element with the last element and trim the slice.
			b.listeners[len(b.listeners)-1], b.listeners[i] = b.listeners[i], b.listeners[len(b.listeners)-1]
			b.listeners = b.listeners[:len(b.listeners)-1]
			return nil
		}
	}
	return ErrNotInBroadcast
}

// Broadcast sends a broadcast to all listening channels.
// it returns how many channels got the message.
func (b *Broadcaster) Broadcast(val interface{}) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, ErrAlreadyClosed
	}

	count := 0
	for _, ch := range b.listeners {
		select {
		case ch <- val:
			count++
		default:
		}
	}
	return count, nil
}

// Close closes the broadcast and all the listening channels it created.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for _, ch := range b.listeners {
		close(ch)
	}
	b.listeners = b.listeners[:0]
}
