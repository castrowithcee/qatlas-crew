package term

import (
	"sync"

	"github.com/charmbracelet/x/vt"
)

// outbox is the bytes on their way to the PTY. It has no limit because a pasted text is as large as the
// person made it, and a writer that waited for the PTY would hold the VT.
type outbox struct {
	mu     sync.Mutex
	buf    []byte
	wake   chan struct{} // capacity 1
	closed bool
}

func (o *outbox) add(p []byte) {
	o.mu.Lock()
	if !o.closed {
		o.buf = append(o.buf, p...)
	}
	o.mu.Unlock()
	signal(o.wake)
}

// take returns everything waiting, or nil.
func (o *outbox) take() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	b := o.buf
	o.buf = nil
	return b
}

// discard drops what waits and everything that is added later.
func (o *outbox) discard() {
	o.mu.Lock()
	o.buf, o.closed = nil, true
	o.mu.Unlock()
}

// inputQueue holds the input of the person until inputLoop has turned it into bytes.
type inputQueue struct {
	mu     sync.Mutex
	items  []func(*vt.SafeEmulator)
	wake   chan struct{} // capacity 1
	closed bool
}

func (q *inputQueue) add(f func(*vt.SafeEmulator)) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, f)
	}
	q.mu.Unlock()
	signal(q.wake)
}

// take returns the oldest item, or nil.
func (q *inputQueue) take() func(*vt.SafeEmulator) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	f := q.items[0]
	q.items[0] = nil
	q.items = q.items[1:]
	return f
}

func (q *inputQueue) discard() {
	q.mu.Lock()
	q.items, q.closed = nil, true
	q.mu.Unlock()
}

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}
