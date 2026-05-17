package encoder

import (
	"io"
	"sync"
)

// Broadcaster distributes data from one source to multiple listeners
type Broadcaster struct {
	listeners map[chan []byte]struct{}
	history   [][]byte
	capacity  int // Dynamic pre-buffering capacity!
	mu        sync.RWMutex
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		listeners: make(map[chan []byte]struct{}),
		history:   make([][]byte, 0, 128),
		capacity:  128, // Default golden sweet spot is 128 chunks (512KB)
	}
}

// SetCapacity dynamically updates the history buffer capacity in real-time.
// If the current cache exceeds the new capacity, it automatically trims it.
func (b *Broadcaster) SetCapacity(capacity int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.capacity = capacity
	if len(b.history) > capacity {
		b.history = b.history[len(b.history)-capacity:]
	}
}

// AddListener adds a new channel to receive data.
// It instantly pre-loads the channel with the history buffer
// to satisfy browser pre-buffering thresholds instantly, eliminating the "Loading..." delay.
func (b *Broadcaster) AddListener() chan []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 100)
	
	// Pre-load with history
	for _, chunk := range b.history {
		select {
		case ch <- chunk:
		default:
		}
	}
	
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
	b.mu.Lock()
	defer b.mu.Unlock()

	// Copy data to avoid modification issues
	data := make([]byte, len(p))
	copy(data, p)

	// Keep last b.capacity chunks for instant pre-buffering
	b.history = append(b.history, data)
	if len(b.history) > b.capacity {
		b.history = b.history[len(b.history)-b.capacity:]
	}

	for ch := range b.listeners {
		select {
		case ch <- data:
		default:
			// Listener too slow, skip this chunk for them
		}
	}
	return len(p), nil
}

// BroadcastFrom handles the fan-out from an io.Reader to a Broadcaster
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
