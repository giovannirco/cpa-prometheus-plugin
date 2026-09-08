package quota

import (
	"sync"
	"time"
)

const DefaultRefreshInterval = 5 * time.Minute

type Poller struct {
	Interval time.Duration

	mu      sync.Mutex
	started bool
	stop    chan struct{}
	done    chan struct{}
}

func NewPoller(interval time.Duration) *Poller {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	return &Poller{
		Interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (p *Poller) Start(run func()) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	select {
	case <-p.stop:
		// Already stopped; nothing to start.
		p.mu.Unlock()
		close(p.done)
		return
	default:
	}
	p.started = true
	p.mu.Unlock()
	go func() {
		defer close(p.done)
		run()
		ticker := time.NewTicker(p.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// Stop halts the poll loop and waits for an in-flight run to finish. It is
// safe to call on a poller that was never started, and safe to call twice;
// callers must not hold a lock that the poll callback needs, because Stop
// blocks for as long as the callback runs.
func (p *Poller) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	select {
	case <-p.stop:
		started := p.started
		p.mu.Unlock()
		if started {
			<-p.done
		}
		return
	default:
		close(p.stop)
	}
	started := p.started
	p.mu.Unlock()
	if !started {
		// Nothing ever ran, so no goroutine will close done.
		return
	}
	<-p.done
}
