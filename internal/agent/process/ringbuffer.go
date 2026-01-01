package process

import (
	"sync"
)

// RingBuffer is a thread-safe circular buffer for log lines
type RingBuffer struct {
	data  []string
	size  int
	head  int
	tail  int
	count int
	mu    sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the specified capacity
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]string, size),
		size: size,
	}
}

// Write adds a line to the buffer
func (rb *RingBuffer) Write(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data[rb.head] = line
	rb.head = (rb.head + 1) % rb.size

	if rb.count < rb.size {
		rb.count++
	} else {
		rb.tail = (rb.tail + 1) % rb.size
	}
}

// Tail returns the last n lines from the buffer
func (rb *RingBuffer) Tail(n int) []string {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.count {
		n = rb.count
	}

	result := make([]string, n)
	start := (rb.head - n + rb.size) % rb.size

	for i := 0; i < n; i++ {
		result[i] = rb.data[(start+i)%rb.size]
	}

	return result
}

// Lines returns the current count of lines in the buffer
func (rb *RingBuffer) Lines() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Clear empties the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.head = 0
	rb.tail = 0
	rb.count = 0
}
