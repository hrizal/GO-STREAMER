package encoder

import (
	"io"
	"sync"
)

// Broadcaster distributes data from one source to multiple listeners
type Broadcaster struct {
	listeners map[chan []byte]struct{}
	mu        sync.RWMutex
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		listeners: make(map[chan []byte]struct{}),
	}
}

// AddListener adds a new channel to receive data.
// bufferSize should be large enough to prevent blocking.
func (b *Broadcaster) AddListener() chan []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 100)
	b.listeners[ch] = struct{}{}
	return ch
}

// RemoveListener removes a listener channel
func (b *Broadcaster) RemoveListener(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.listeners, ch)
	close(ch)
}

// Write implements io.Writer to broadcast data to all listeners
func (b *Broadcaster) Write(p []byte) (n int, err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Copy data to avoid modification issues
	data := make([]byte, len(p))
	copy(data, p)

	for ch := range b.listeners {
		select {
		case ch <- data:
		default:
			// Listener too slow, skip this chunk for them
		}
	}
	return len(p), nil
}

// BroadcastStream handles the fan-out from an io.Reader to a Broadcaster
func (b *Broadcaster) BroadcastFrom(r io.Reader) error {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			return err
		}
	}
}
