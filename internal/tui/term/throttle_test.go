package term

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	waiting chan struct{}
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1000, 0), waiting: make(chan struct{}, 16)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, waiter{c.now.Add(d), ch})
	c.mu.Unlock()
	c.waiting <- struct{}{}
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	rest := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			rest = append(rest, w)
		}
	}
	c.waiters = rest
}

func TestThrottleLimitsFramesToTheInterval(t *testing.T) {
	clk := newFakeClock()
	dirty := make(chan struct{}, 1)
	stop := make(chan struct{})
	emitted := make(chan struct{}, 16)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		throttle(clk, FrameInterval, dirty, stop, func() { emitted <- struct{}{} })
	}()
	signal := func() {
		select {
		case dirty <- struct{}{}:
		default:
		}
	}
	expect := func(what string) {
		t.Helper()
		select {
		case <-emitted:
		case <-time.After(waitLimit):
			t.Fatalf("no frame: %s", what)
		}
	}

	signal()
	expect("first output is shown at once")

	// A burst right after the first frame waits for the interval and counts as one frame.
	signal()
	signal()
	signal()
	<-clk.waiting
	select {
	case <-emitted:
		t.Fatal("frame emitted before the interval passed")
	default:
	}
	clk.Advance(FrameInterval - time.Millisecond)
	select {
	case <-emitted:
		t.Fatal("frame emitted before the interval passed")
	case <-time.After(20 * time.Millisecond):
	}
	clk.Advance(time.Millisecond)
	expect("burst after the interval")
	select {
	case <-emitted:
		t.Fatal("burst produced more than one frame")
	case <-time.After(20 * time.Millisecond):
	}

	// After a pause of more than the interval the next output is shown at once again.
	clk.Advance(time.Second)
	signal()
	expect("output after a pause")

	close(stop)
	select {
	case <-ended:
	case <-time.After(waitLimit):
		t.Fatal("throttle did not stop")
	}
}
