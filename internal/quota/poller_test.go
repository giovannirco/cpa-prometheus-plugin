package quota

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPollerDefaultIntervalIsFiveMinutes(t *testing.T) {
	p := NewPoller(0)
	if p.Interval != 5*time.Minute {
		t.Fatalf("Interval = %s, want 5m", p.Interval)
	}
	if DefaultRefreshInterval != 5*time.Minute {
		t.Fatalf("DefaultRefreshInterval = %s, want 5m", DefaultRefreshInterval)
	}
}

func TestPollerStartRunsImmediatelyThenStops(t *testing.T) {
	p := NewPoller(time.Hour)
	var n atomic.Int32
	p.Start(func() { n.Add(1) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() < 1 {
		t.Fatal("poller did not run immediately")
	}
	p.Stop()
}
