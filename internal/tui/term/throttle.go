package term

import "time"

// FrameInterval is the shortest time between two frame messages, 60 frames per second.
const FrameInterval = time.Second / 60

// Clock is the source of time for the frame throttle; tests replace it.
type Clock interface {
	Now() time.Time
	// After returns a channel that receives once d has passed.
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// throttle turns a stream of dirty signals into emit calls at most once per interval. Signals that arrive
// while it waits are merged into the next emit, so the last state is always emitted. It returns when stop
// is closed.
func throttle(clock Clock, interval time.Duration, dirty <-chan struct{}, stop <-chan struct{}, emit func()) {
	var last time.Time
	for {
		select {
		case <-stop:
			return
		case <-dirty:
		}
		if !last.IsZero() {
			if wait := interval - clock.Now().Sub(last); wait > 0 {
				select {
				case <-stop:
					return
				case <-clock.After(wait):
				}
			}
		}
		last = clock.Now()
		emit()
	}
}
