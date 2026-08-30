package quota

import (
	"sync"
	"time"
)

const DefaultRefreshInterval = 5 * time.Minute

type Poller struct {
	Interval time.Duration

	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
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

func (p *Poller) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	select {
	case <-p.stop:
		p.mu.Unlock()
		return
	default:
		close(p.stop)
	}
	p.mu.Unlock()
	<-p.done
}
