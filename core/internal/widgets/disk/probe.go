package disk

import (
	"sync"
	"time"
)

// How long a filesystem may take to say how full it is. A share gets less:
// a dead server is common; a stuck local mount (a FUSE filesystem over a
// dead share, a failing disk) is rare but must not freeze the widget either.
const (
	networkTimeout = 2 * time.Second
	localTimeout   = 5 * time.Second
)

// prober measures filesystems that may never answer. A network share whose
// server is gone blocks statfs in the kernel, and no context can interrupt
// it, so each such measurement runs in its own goroutine against a deadline.
// One still stuck from an earlier poll is not asked again until it returns:
// a dead share costs one goroutine, not one per poll.
type prober struct {
	mu       sync.Mutex
	inflight map[string]bool
}

// probe runs measure for key within timeout. answered is false when it did
// not return in time, or is still stuck from before.
func probe[T any](p *prober, key string, timeout time.Duration, measure func() (T, error)) (value T, err error, answered bool) {
	p.mu.Lock()
	if p.inflight == nil {
		p.inflight = map[string]bool{}
	}
	if p.inflight[key] {
		p.mu.Unlock()
		return value, nil, false
	}
	p.inflight[key] = true
	p.mu.Unlock()

	type outcome struct {
		v   T
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		v, err := measure()
		p.mu.Lock()
		delete(p.inflight, key)
		p.mu.Unlock()
		done <- outcome{v, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case o := <-done:
		return o.v, o.err, true
	case <-timer.C:
		return value, nil, false
	}
}
